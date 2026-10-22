package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	"github.com/yusuke0610/etoki/port"
)

// DiagramJob は 1 回の図のドラフト生成の、いまの様子。Input は送られた入力そのもの。
type DiagramJob = Job[DiagramRequest, DiagramDraft]

// DiagramJobs は図のドラフト生成をリクエストから切り離して走らせる（ADR 0091）。
//
// 生成は数十秒かかり、解釈と同じ枠（ADR 0044）を取る。リクエストに結びつけると、
// 生成中にボード一覧へ戻ったときに画面は忘れるのに、サーバーは走り続けて枠を握る。
// 戻って生成し直すと 429 に弾かれ、そのあいだは解釈も弾かれる（#329）。置き場の
// 約束は解釈と同じで、jobTable にある。
//
// **会話は保存しない**（ADR 0041）。ジョブが入力を持つのは、そのジョブが終わって
// 捨てられるまで。開き直した画面が、どの指示への答えかを戻すのに使う。
type DiagramJobs struct {
	svc   *DiagramService
	table *jobTable[DiagramRequest, DiagramDraft]
}

// NewDiagramJobs は DiagramJobs を作る。
func NewDiagramJobs(svc *DiagramService, opts ...JobsOption) *DiagramJobs {
	return &DiagramJobs{
		svc: svc,
		table: newJobTable[DiagramRequest, DiagramDraft](
			"diagram draft", svc.limits, svc.logger, ErrDiagramFailed, opts),
	}
}

// Start は生成を始めて、ジョブを返す。LLM の応答は待たない。
//
// 認可と入力の検証、枠の確保までは呼び出しの中で行う。404 / 400 / 413 / 429 は
// 始める時点で返り、ジョブにならない（ADR 0044）。
//
// **同じ操作者が同じ入力（種類・指示・往復・印のすべて）で生成している最中なら、
// 新しく始めず、そのジョブを返す。** どこか 1 つでも違えば別の入力で、枠に弾かれる
// （429）。
func (j *DiagramJobs) Start(
	ctx context.Context, boardID string, req DiagramRequest,
) (DiagramJob, error) {
	return j.table.start(ctx, boardID, func() (jobRequest[DiagramRequest, DiagramDraft], error) {
		if err := j.svc.prepare(ctx, boardID, req); err != nil {
			return jobRequest[DiagramRequest, DiagramDraft]{}, err
		}

		return jobRequest[DiagramRequest, DiagramDraft]{
			input: req,
			key:   diagramJobKey(req),
			attrs: []any{
				slog.String("board", boardID), slog.String("diagramKind", string(req.Kind)),
			},
			run: func(ctx context.Context) (DiagramDraft, error) {
				return j.svc.execute(ctx, boardID, req)
			},
		}, nil
	})
}

// diagramJobKey は同じ入力かを見る鍵を作る。**入力を丸ごと入れる。** 指示が同じでも
// 往復が違えば、返すべき図が違う。
func diagramJobKey(req DiagramRequest) string {
	// 文字列と真偽値だけの構造体なので、Marshal は失敗しない。
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Get はジョブを 1 件返す。**読んでも消えない**（jobTable.get）。
func (j *DiagramJobs) Get(ctx context.Context, boardID, jobID string) (DiagramJob, error) {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return DiagramJob{}, err
	}
	return j.table.get(ctx, boardID, jobID)
}

// List は操作者の、そのボードのジョブを始めた順に返す。結果は含めず、入力は含める。
func (j *DiagramJobs) List(ctx context.Context, boardID string) ([]DiagramJob, error) {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return nil, err
	}
	return j.table.list(ctx, boardID), nil
}

// Discard はジョブを要らないものとして捨てる。走っていれば止める。
func (j *DiagramJobs) Discard(ctx context.Context, boardID, jobID string) error {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return err
	}
	return j.table.discard(ctx, boardID, jobID)
}
