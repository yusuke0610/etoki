package usecase

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"
)

// ErrJobNotFound は対象のジョブが無いことを表す。
//
// **他の利用者のジョブも、別のボードのジョブも、これになる。** 区別すると、
// ジョブ ID を総当たりして他人の実行の存在を確かめられる（ErrBoardNotFound と
// 同じ理由、ADR 0016 / 0017）。
var ErrJobNotFound = errors.New("etoki: job not found")

// LLM を叩くジョブに関わる既定値（ADR 0085）。解釈と図のドラフト生成で共有する。
const (
	// DefaultJobTimeout は 1 つのジョブが走り続けてよい上限。
	//
	// リクエストの ctx から切り離すと、止める手が利用者の取り消しとこの上限しか
	// 無くなる。LLM が応えないまま枠を握り続けると、次の実行が 429 で弾かれ続ける。
	DefaultJobTimeout = 10 * time.Minute

	// DefaultJobRetention は終わったジョブの結果を持つ長さ。
	//
	// 結果はメモリだけに持つ。誰も取りに来ない結果が溜まり続けないよう、終わった
	// 時刻から数えて捨てる。走っている間は数えない。
	DefaultJobRetention = time.Hour
)

// JobStatus はジョブの状態。
type JobStatus string

// ジョブの状態。**取り消したジョブは状態にならず、ジョブごと消える。**
const (
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
)

// Job は 1 つのジョブの、いまの様子。In は頼まれたもの、R は結果。
type Job[In, R any] struct {
	ID        string
	Status    JobStatus
	StartedAt time.Time
	// Input は始めたときに頼まれたもの。開き直した画面が、どの操作のジョブかを
	// 戻すのに読む。
	Input In
	// Result は Status が JobSucceeded のときだけ入る。
	Result *R
	// Err は Status が JobFailed のときだけ入る。写し替えは呼び出し側（HTTP）が
	// sentinel で行う。
	Err error
}

// JobsOption はジョブの置き場の設定を差し替える。
type JobsOption func(*jobConfig)

type jobConfig struct {
	// now と newID はテストが差し替える。
	now   func() time.Time
	newID func() string

	timeout   time.Duration
	retention time.Duration
}

// WithJobClock は時計を差し替える。nil は無視する。
func WithJobClock(now func() time.Time) JobsOption {
	return func(c *jobConfig) {
		if now != nil {
			c.now = now
		}
	}
}

// WithJobIDGenerator は ID の採番を差し替える。nil は無視する。
func WithJobIDGenerator(f func() string) JobsOption {
	return func(c *jobConfig) {
		if f != nil {
			c.newID = f
		}
	}
}

// WithJobTimeout は 1 つのジョブが走り続けてよい上限を差し替える。0 以下は無視する。
func WithJobTimeout(d time.Duration) JobsOption {
	return func(c *jobConfig) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithJobRetention は終わったジョブを持つ長さを差し替える。0 以下は無視する。
func WithJobRetention(d time.Duration) JobsOption {
	return func(c *jobConfig) {
		if d > 0 {
			c.retention = d
		}
	}
}

func newJobConfig(opts []JobsOption) jobConfig {
	c := jobConfig{
		now:       time.Now,
		newID:     randomJobID,
		timeout:   DefaultJobTimeout,
		retention: DefaultJobRetention,
	}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func randomJobID() string {
	var b [16]byte
	// crypto/rand は Go 1.24 以降、失敗しない。
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// jobTable は LLM を叩く実行を、リクエストから切り離して走らせる置き場（ADR 0085）。
//
// 解釈と図のドラフト生成が**同じものを使う**（ADR 0091）。止める手（取り消しと
// 上限時間）、枠を戻す順、panic の扱い、見せる範囲は、どちらでも同じでなければ
// ならない。2 つに書くと、片方だけ直したときに枠を握ったまま残るジョブができる。
//
// **状態はメモリだけに持つ。** 再起動すれば消える。依っているのは LLMLimiter /
// BoardLocks と同じ前提（SQLite のファイルを複数プロセスで共有しない）。
//
// **ジョブは操作者とボードで絞る。** 他人のジョブは存在も見せない。
type jobTable[In, R any] struct {
	jobConfig

	limits *LLMLimiter
	logger *slog.Logger
	// name はログでジョブの種類を名乗る語（"interpretation" など）。
	name string
	// rejected は「接続はできたが、上限まで投げ直しても出力を受け付けられなかった」
	// ことを表す sentinel。記録の出し分けで種類ごとに違うのはそこだけ。
	rejected error

	// mu は jobs と各 job の中身を守る。**start は prepare から登録までこの鍵を
	// 握る。** 二度押しの 2 つ目が、1 つ目の登録より先に「実行中は無い」と読むと、
	// 同じ入力のジョブが 2 つ走る。
	mu   sync.Mutex
	jobs map[string]*jobEntry[In, R]
}

// jobEntry は jobTable の中の 1 件。外へは Job のコピーで返す。
type jobEntry[In, R any] struct {
	actor   string
	boardID string
	// key は同じ入力かを見る鍵。**中身まで入れる。** 同じ注釈 ID でも、保存で
	// 内容が変われば別の入力。
	key        string
	attrs      []any
	job        Job[In, R]
	finishedAt time.Time
	cancel     context.CancelFunc
}

// jobRequest は始めるジョブ 1 件ぶん。prepare が揃える。
type jobRequest[In, R any] struct {
	input In
	// key は同じ入力かを見る鍵（jobEntry.key）。
	key string
	// attrs はログでこのジョブを名指す属性。
	attrs []any
	// run は枠を取ったあとに、リクエストから切り離した ctx で呼ばれる。
	run func(ctx context.Context) (R, error)
}

func newJobTable[In, R any](
	name string, limits *LLMLimiter, logger *slog.Logger, rejected error, opts []JobsOption,
) *jobTable[In, R] {
	return &jobTable[In, R]{
		jobConfig: newJobConfig(opts),
		limits:    limits,
		logger:    logger,
		name:      name,
		rejected:  rejected,
		jobs:      make(map[string]*jobEntry[In, R]),
	}
}

// start はジョブを始めて返す。LLM の応答は待たない。
//
// prepare は鍵を握ったまま呼ぶ。認可と入力の検証はそこで済ませ、枠の確保は
// ここで行う。404 / 400 / 413 / 429 は始める時点で返り、ジョブにならない
// （ADR 0044 の「枠を取るのは LLM を叩く直前」のまま）。
//
// **同じ操作者・同じボード・同じ鍵のジョブが走っていれば、新しく始めずにそれを
// 返す。** 二度押しや、戻って押し直したときに、同じ入力で LLM を二度叩いて
// 二度課金しない。違う入力は枠に弾かれる（429）。
func (t *jobTable[In, R]) start(
	ctx context.Context, boardID string, prepare func() (jobRequest[In, R], error),
) (Job[In, R], error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sweep()

	req, err := prepare()
	if err != nil {
		return Job[In, R]{}, err
	}

	actor := actorOf(ctx)
	for _, e := range t.jobs {
		if e.actor == actor && e.boardID == boardID && e.key == req.key &&
			e.job.Status == JobRunning {
			return e.job, nil
		}
	}

	release, err := t.limits.Acquire(ctx)
	if err != nil {
		return Job[In, R]{}, err
	}

	// **リクエストの ctx から切り離す。** 値（操作者）は引き継ぐ。止める手は
	// 取り消しと上限時間だけ。
	runCtx, cancelTimeout := context.WithTimeout(context.WithoutCancel(ctx), t.timeout)
	runCtx, cancel := context.WithCancel(runCtx)

	e := &jobEntry[In, R]{
		actor:   actor,
		boardID: boardID,
		key:     req.key,
		attrs:   req.attrs,
		job: Job[In, R]{
			ID:        t.newID(),
			Status:    JobRunning,
			StartedAt: t.now(),
			Input:     req.input,
		},
		cancel: func() { cancel(); cancelTimeout() },
	}
	t.jobs[e.job.ID] = e
	started := e.job

	release = sync.OnceFunc(release)
	go func() {
		defer release()
		defer e.cancel()

		result, err := t.run(runCtx, e, req.run)
		// **枠を戻してから終わりを書く。** 逆だと、終わったと読めたのに次の start が
		// 枠に弾かれる瞬間ができる。
		release()
		t.finish(e, result, err)
	}()

	return started, nil
}

// run は f を呼び、panic を失敗として返す。
//
// **リクエストの goroutine ではないので、Gin の Recovery が受けない。** アダプタの
// panic が、そのままプロセスごと落とす。落とさず、このジョブの失敗にする。
func (t *jobTable[In, R]) run(
	ctx context.Context, e *jobEntry[In, R], f func(context.Context) (R, error),
) (result R, err error) {
	defer func() {
		if r := recover(); r != nil {
			t.logger.Error(t.name+" panicked", e.with(slog.Any("panic", r))...)
			err = errors.New("etoki: " + t.name + " panicked")
		}
	}()
	return f(ctx)
}

// finish は結果を書く。取り消されて表から消えたジョブにも書くが、誰にも読まれない。
func (t *jobTable[In, R]) finish(e *jobEntry[In, R], result R, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	e.finishedAt = t.now()
	if err != nil {
		e.job.Status = JobFailed
		e.job.Err = err
		// 取り消しは障害ではない。
		if !errors.Is(err, context.Canceled) {
			t.logFailure(e, err)
		}
		return
	}
	e.job.Status = JobSucceeded
	e.job.Result = &result
}

// logFailure は失敗を残す。リクエストに結びつかないので、HTTP 層の記録は通らない。
func (t *jobTable[In, R]) logFailure(e *jobEntry[In, R], err error) {
	switch {
	case IsLLMCallFailure(err):
		// 呼び出しそのものの失敗。API キーはアダプタ側でエラーに載せていない。
		t.logger.Error("llm call failed", e.with(slog.Any("error", err))...)
	case errors.Is(err, t.rejected):
		t.logger.Warn("llm output rejected", e.with(slog.Any("error", err))...)
	}
}

// with はこのジョブを名指す属性に、attrs を足して返す。
//
// **e.attrs に書き込まない。** 容量に余りがあると、append は元の配列に書く。
func (e *jobEntry[In, R]) with(attrs ...any) []any {
	return append(slices.Clip(e.attrs), attrs...)
}

// get はジョブを 1 件返す。**読んでも消えない。**
//
// 返事が途中で失われたとき、読んだ時点で消えていると結果を二度と取れず、課金した
// 実行を捨てることになる。要らなくなったことを伝えるのは discard。
func (t *jobTable[In, R]) get(ctx context.Context, boardID, jobID string) (Job[In, R], error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sweep()

	e, ok := t.find(ctx, boardID, jobID)
	if !ok {
		return Job[In, R]{}, ErrJobNotFound
	}
	return e.job, nil
}

// list は操作者の、そのボードのジョブを始めた順に返す。結果は含めない。
//
// 結果を取るのは get。一覧で全部の結果を運ぶと、使わないものまで毎回送る。
func (t *jobTable[In, R]) list(ctx context.Context, boardID string) []Job[In, R] {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sweep()

	actor := actorOf(ctx)
	out := []Job[In, R]{}
	for _, e := range t.jobs {
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
	return out
}

// discard はジョブを要らないものとして捨てる。
//
// 走っていれば止める（ctx が切れ、アダプタが応じれば枠が戻る）。終わっていれば
// 結果を捨てるだけ。**どちらも同じ操作にしてある。** 画面が結果を受け取り終えた
// ときも、利用者が取り消したときも、言いたいことは「このジョブはもう要らない」で
// 同じ。
func (t *jobTable[In, R]) discard(ctx context.Context, boardID, jobID string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	e, ok := t.find(ctx, boardID, jobID)
	if !ok {
		return ErrJobNotFound
	}
	e.cancel()
	delete(t.jobs, jobID)
	return nil
}

// find は操作者のそのボードのジョブを引く。鍵を握って呼ぶ。
func (t *jobTable[In, R]) find(
	ctx context.Context, boardID, jobID string,
) (*jobEntry[In, R], bool) {
	e, ok := t.jobs[jobID]
	if !ok || e.actor != actorOf(ctx) || e.boardID != boardID {
		return nil, false
	}
	return e, true
}

// sweep は保持の期間を過ぎた、終わったジョブを捨てる。鍵を握って呼ぶ。
func (t *jobTable[In, R]) sweep() {
	cutoff := t.now().Add(-t.retention)
	for id, e := range t.jobs {
		if e.job.Status != JobRunning && e.finishedAt.Before(cutoff) {
			delete(t.jobs, id)
		}
	}
}
