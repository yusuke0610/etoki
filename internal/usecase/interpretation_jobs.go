package usecase

import (
	"context"
	"log/slog"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// InterpretationJobInput は解釈のジョブが頼まれたもの。
type InterpretationJobInput struct {
	AnnotationID string
}

// InterpretationJob は 1 回の解釈の、いまの様子。
type InterpretationJob = Job[InterpretationJobInput, InterpretationResult]

// InterpretationJobs は解釈をリクエストから切り離して走らせる。
//
// 解釈は 1〜数分かかる。リクエストに結びつけると、画面を離れた時点で結果の
// 受け取り手がいなくなるのに、サーバーは走り続けて枠を握る。画面は終わった
// つもりで再実行し、枠に弾かれる（#307）。**走らせるのと結果を受け取るのを
// 別のリクエストにする**（ADR 0085）。置き場の約束は jobTable。
type InterpretationJobs struct {
	svc   *InterpretationService
	table *jobTable[InterpretationJobInput, InterpretationResult]
}

// NewInterpretationJobs は InterpretationJobs を作る。
func NewInterpretationJobs(svc *InterpretationService, opts ...JobsOption) *InterpretationJobs {
	return &InterpretationJobs{
		svc: svc,
		table: newJobTable[InterpretationJobInput, InterpretationResult](
			"interpretation", svc.limits, svc.logger, ErrInterpretationFailed, opts),
	}
}

// Start は解釈を始めて、ジョブを返す。LLM の応答は待たない。
//
// 認可と入力の検証、枠の確保までは呼び出しの中で行う。404 / 400 / 429 は
// 始める時点で返り、ジョブにならない（ADR 0044）。
//
// **同じ操作者が同じ注釈（保存済みの内容が同じもの）をまだ解釈している最中なら、
// 新しく始めず、そのジョブを返す。** そのとき images は読まない。別の注釈は枠に
// 弾かれる（429）。
func (j *InterpretationJobs) Start(
	ctx context.Context, boardID, annotationID string, images []port.Image,
) (InterpretationJob, error) {
	return j.table.start(ctx, boardID, func() (jobRequest[InterpretationJobInput, InterpretationResult], error) {
		p, err := j.svc.prepare(ctx, boardID, annotationID, images)
		if err != nil {
			return jobRequest[InterpretationJobInput, InterpretationResult]{}, err
		}

		return jobRequest[InterpretationJobInput, InterpretationResult]{
			input: InterpretationJobInput{AnnotationID: annotationID},
			// 同じ注釈 ID でも、保存で内容が変われば別の入力。保存前のシーンを
			// 読んでいるジョブを返さない。
			//
			// **鍵は解釈が読むテキスト（AnnotationTexts）から作る。AnnotationHash
			// ではない。** あちらは frame の名前を入れない（ADR 0079）ので、名前だけ
			// 変えて保存し直すと、古い名前で走っているジョブが返る（ADR 0085）。
			//
			// **ボードの背景も鍵に入れる**（ADR 0093）。背景はハッシュには入らない
			// が、保存すれば LLM に渡すものが変わる。入れないと、背景を直して押し
			// 直したときに古い背景で走っているジョブが返る。別の絵から作ったものは
			// 入れない。変わるのは保存ではなく作成で、前回ぶんを入れないのと同じ。
			// 背景は何でも書けるので末尾に置く（区切りの後ろに続くものが無い）。
			key: annotationID + "\x00" + string(domain.ComputeContentHash(
				p.scene.AnnotationTexts(annotationID), p.annotation.Granularity, p.annotation.Kind)) +
				"\x00" + p.context.Premise,
			attrs: []any{
				slog.String("board", boardID), slog.String("annotation", annotationID),
			},
			run: func(ctx context.Context) (InterpretationResult, error) {
				return j.svc.execute(ctx, boardID, annotationID, p)
			},
		}, nil
	})
}

// Get はジョブを 1 件返す。**読んでも消えない**（jobTable.get）。
func (j *InterpretationJobs) Get(
	ctx context.Context, boardID, jobID string,
) (InterpretationJob, error) {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return InterpretationJob{}, err
	}
	return j.table.get(ctx, boardID, jobID)
}

// List は操作者の、そのボードのジョブを始めた順に返す。結果は含めない。
func (j *InterpretationJobs) List(ctx context.Context, boardID string) ([]InterpretationJob, error) {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return nil, err
	}
	return j.table.list(ctx, boardID), nil
}

// Discard はジョブを要らないものとして捨てる。走っていれば止める。
func (j *InterpretationJobs) Discard(ctx context.Context, boardID, jobID string) error {
	if _, err := j.svc.access(ctx, boardID, port.RoleEditor); err != nil {
		return err
	}
	return j.table.discard(ctx, boardID, jobID)
}
