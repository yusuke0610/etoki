package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// ErrInterpretationJobNotFound は対象のジョブが無いことを表す。
//
// **他の利用者のジョブも、別のボードのジョブも、これになる。** 区別すると、
// ジョブ ID を総当たりして他人の解釈の存在を確かめられる（ErrBoardNotFound と
// 同じ理由、ADR 0016 / 0017）。
var ErrInterpretationJobNotFound = errors.New("etoki: interpretation job not found")

// 解釈のジョブに関わる既定値（ADR 0085）。
const (
	// DefaultInterpretationTimeout は 1 つの解釈が走り続けてよい上限。
	//
	// リクエストの ctx から切り離すと、止める手が利用者の取り消しとこの上限しか
	// 無くなる。LLM が応えないまま枠を握り続けると、次の解釈が 429 で弾かれ続ける。
	DefaultInterpretationTimeout = 10 * time.Minute

	// DefaultInterpretationRetention は終わったジョブの結果を持つ長さ。
	//
	// 結果はメモリだけに持つ。誰も取りに来ない結果が溜まり続けないよう、終わった
	// 時刻から数えて捨てる。走っている間は数えない。
	DefaultInterpretationRetention = time.Hour
)

// InterpretationJobStatus はジョブの状態。
type InterpretationJobStatus string

// ジョブの状態。**取り消したジョブは状態にならず、ジョブごと消える。**
const (
	InterpretationJobRunning   InterpretationJobStatus = "running"
	InterpretationJobSucceeded InterpretationJobStatus = "succeeded"
	InterpretationJobFailed    InterpretationJobStatus = "failed"
)

// InterpretationJob は 1 回の解釈の、いまの様子。
type InterpretationJob struct {
	ID           string
	AnnotationID string
	Status       InterpretationJobStatus
	StartedAt    time.Time
	// Result は Status が Succeeded のときだけ入る。
	Result *InterpretationResult
	// Err は Status が Failed のときだけ入る。写し替えは呼び出し側（HTTP）が
	// sentinel で行う。
	Err error
}

// InterpretationJobs は解釈をリクエストから切り離して走らせる。
//
// 解釈は 1〜数分かかる。リクエストに結びつけると、画面を離れた時点で結果の
// 受け取り手がいなくなるのに、サーバーは走り続けて枠を握る。画面は終わった
// つもりで再実行し、枠に弾かれる（#307）。**走らせるのと結果を受け取るのを
// 別のリクエストにする。**
//
// **状態はメモリだけに持つ。** 再起動すれば消える。依っているのは LLMLimiter /
// BoardLocks と同じ前提（SQLite のファイルを複数プロセスで共有しない）で、
// 結果は元々フロントのメモリにしか持たない設計だった（ADR 0085）。
//
// **ジョブは操作者とボードで絞る。** 他人のジョブは存在も見せない。
type InterpretationJobs struct {
	svc *InterpretationService

	// now と newID はテストが差し替える。
	now   func() time.Time
	newID func() string

	timeout   time.Duration
	retention time.Duration

	// mu は jobs と各 job の中身を守る。**Start は prepare から登録までこの鍵を
	// 握る。** 二度押しの 2 つ目が、1 つ目の登録より先に「実行中は無い」と読むと、
	// 同じ注釈のジョブが 2 つ走る。
	mu   sync.Mutex
	jobs map[string]*interpretationJob
}

// interpretationJob は InterpretationJobs の中の 1 件。外へは InterpretationJob の
// コピーで返す。
type interpretationJob struct {
	actor      string
	boardID    string
	job        InterpretationJob
	finishedAt time.Time
	cancel     context.CancelFunc
}

// InterpretationJobsOption は InterpretationJobs の設定を差し替える。
type InterpretationJobsOption func(*InterpretationJobs)

// WithJobClock は時計を差し替える。nil は無視する。
func WithJobClock(now func() time.Time) InterpretationJobsOption {
	return func(j *InterpretationJobs) {
		if now != nil {
			j.now = now
		}
	}
}

// WithJobIDGenerator は ID の採番を差し替える。nil は無視する。
func WithJobIDGenerator(f func() string) InterpretationJobsOption {
	return func(j *InterpretationJobs) {
		if f != nil {
			j.newID = f
		}
	}
}

// WithJobTimeout は 1 つの解釈が走り続けてよい上限を差し替える。0 以下は無視する。
func WithJobTimeout(d time.Duration) InterpretationJobsOption {
	return func(j *InterpretationJobs) {
		if d > 0 {
			j.timeout = d
		}
	}
}

// WithJobRetention は終わったジョブを持つ長さを差し替える。0 以下は無視する。
func WithJobRetention(d time.Duration) InterpretationJobsOption {
	return func(j *InterpretationJobs) {
		if d > 0 {
			j.retention = d
		}
	}
}

// NewInterpretationJobs は InterpretationJobs を作る。
func NewInterpretationJobs(
	svc *InterpretationService, opts ...InterpretationJobsOption,
) *InterpretationJobs {
	j := &InterpretationJobs{
		svc:       svc,
		now:       time.Now,
		newID:     randomJobID,
		timeout:   DefaultInterpretationTimeout,
		retention: DefaultInterpretationRetention,
		jobs:      make(map[string]*interpretationJob),
	}
	for _, opt := range opts {
		opt(j)
	}
	return j
}

func randomJobID() string {
	var b [16]byte
	// crypto/rand は Go 1.24 以降、失敗しない。
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Start は解釈を始めて、ジョブを返す。LLM の応答は待たない。
//
// 認可と入力の検証、枠の確保までは呼び出しの中で行う。404 / 400 / 429 は
// 始める時点で返り、ジョブにならない（ADR 0044）。
//
// **同じ操作者が同じ注釈をまだ解釈している最中なら、新しく始めず、そのジョブを
// 返す。** 二度押しや、戻って押し直したときに、同じ入力で LLM を二度叩いて
// 二度課金しない。そのとき images は読まない。別の注釈は枠に弾かれる（429）。
func (j *InterpretationJobs) Start(
	ctx context.Context, boardID, annotationID string, images []port.Image,
) (InterpretationJob, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.sweep()

	p, err := j.svc.prepare(ctx, boardID, annotationID, images)
	if err != nil {
		return InterpretationJob{}, err
	}

	actor := actorOf(ctx)
	for _, e := range j.jobs {
		if e.actor == actor && e.boardID == boardID &&
			e.job.AnnotationID == annotationID && e.job.Status == InterpretationJobRunning {
			return e.job, nil
		}
	}

	release, err := j.svc.limits.Acquire(ctx)
	if err != nil {
		return InterpretationJob{}, err
	}

	// **リクエストの ctx から切り離す。** 値（操作者）は引き継ぐ。止める手は
	// 取り消しと上限時間だけ。
	runCtx, cancelTimeout := context.WithTimeout(context.WithoutCancel(ctx), j.timeout)
	runCtx, cancel := context.WithCancel(runCtx)

	e := &interpretationJob{
		actor:   actor,
		boardID: boardID,
		job: InterpretationJob{
			ID:           j.newID(),
			AnnotationID: annotationID,
			Status:       InterpretationJobRunning,
			StartedAt:    j.now(),
		},
		cancel: func() { cancel(); cancelTimeout() },
	}
	j.jobs[e.job.ID] = e
	started := e.job

	release = sync.OnceFunc(release)
	go func() {
		defer release()
		defer e.cancel()

		result, err := j.svc.execute(runCtx, boardID, annotationID, p)
		// **枠を戻してから終わりを書く。** 逆だと、終わったと読めたのに次の Start が
		// 枠に弾かれる瞬間ができる。
		release()
		j.finish(e, result, err)
	}()

	return started, nil
}

// finish は結果を書く。取り消されて表から消えたジョブにも書くが、誰にも読まれない。
func (j *InterpretationJobs) finish(e *interpretationJob, result InterpretationResult, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	e.finishedAt = j.now()
	if err != nil {
		e.job.Status = InterpretationJobFailed
		e.job.Err = err
		// 取り消しは障害ではない。
		if !errors.Is(err, context.Canceled) {
			j.logFailure(e, err)
		}
		return
	}
	e.job.Status = InterpretationJobSucceeded
	e.job.Result = &result
}

// logFailure は失敗を残す。リクエストに結びつかないので、HTTP 層の記録
// （failLLM）は通らない。
func (j *InterpretationJobs) logFailure(e *interpretationJob, err error) {
	switch {
	case errors.Is(err, ErrLLMUnavailable):
		j.svc.logger.Error("llm call failed",
			slog.String("board", e.boardID), slog.String("annotation", e.job.AnnotationID),
			slog.Any("error", err))
	case errors.Is(err, ErrInterpretationFailed):
		j.svc.logger.Warn("llm output rejected",
			slog.String("board", e.boardID), slog.String("annotation", e.job.AnnotationID),
			slog.Any("error", err))
	}
}

// Get はジョブを 1 件返す。**読んでも消えない。**
//
// 返事が途中で失われたとき、読んだ時点で消えていると結果を二度と取れず、課金した
// 解釈を捨てることになる。要らなくなったことを伝えるのは Discard。
func (j *InterpretationJobs) Get(
	ctx context.Context, boardID, jobID string,
) (InterpretationJob, error) {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return InterpretationJob{}, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.sweep()

	e, ok := j.find(ctx, boardID, jobID)
	if !ok {
		return InterpretationJob{}, ErrInterpretationJobNotFound
	}
	return e.job, nil
}

// List は操作者の、そのボードのジョブを始めた順に返す。結果は含めない。
//
// 結果を取るのは Get。一覧で全部の結果を運ぶと、使わないものまで毎回送る。
func (j *InterpretationJobs) List(ctx context.Context, boardID string) ([]InterpretationJob, error) {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return nil, err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	j.sweep()

	actor := actorOf(ctx)
	out := []InterpretationJob{}
	for _, e := range j.jobs {
		if e.actor != actor || e.boardID != boardID {
			continue
		}
		summary := e.job
		summary.Result = nil
		out = append(out, summary)
	}
	sort.Slice(out, func(a, b int) bool {
		if !out[a].StartedAt.Equal(out[b].StartedAt) {
			return out[a].StartedAt.Before(out[b].StartedAt)
		}
		return out[a].ID < out[b].ID
	})
	return out, nil
}

// Discard はジョブを要らないものとして捨てる。
//
// 走っていれば止める（ctx が切れ、アダプタが応じれば枠が戻る）。終わっていれば
// 結果を捨てるだけ。**どちらも同じ操作にしてある。** 画面が結果を受け取り終えた
// ときも、利用者が取り消したときも、言いたいことは「このジョブはもう要らない」で
// 同じ。
func (j *InterpretationJobs) Discard(ctx context.Context, boardID, jobID string) error {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return err
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	e, ok := j.find(ctx, boardID, jobID)
	if !ok {
		return ErrInterpretationJobNotFound
	}
	e.cancel()
	delete(j.jobs, jobID)
	return nil
}

// find は操作者のそのボードのジョブを引く。鍵を握って呼ぶ。
func (j *InterpretationJobs) find(
	ctx context.Context, boardID, jobID string,
) (*interpretationJob, bool) {
	e, ok := j.jobs[jobID]
	if !ok || e.actor != actorOf(ctx) || e.boardID != boardID {
		return nil, false
	}
	return e, true
}

// sweep は保持の期間を過ぎた、終わったジョブを捨てる。鍵を握って呼ぶ。
func (j *InterpretationJobs) sweep() {
	cutoff := j.now().Add(-j.retention)
	for id, e := range j.jobs {
		if e.job.Status != InterpretationJobRunning && e.finishedAt.Before(cutoff) {
			delete(j.jobs, id)
		}
	}
}
