package httpapi

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/usecase"
)

// jobStore は 1 種類のジョブの置き場（InterpretationJobs / DiagramJobs）が持つ口。
//
// 始める口（Start）は入れない。受け取る入力が種類ごとに違う。
type jobStore[In, R any] interface {
	List(ctx context.Context, boardID string) ([]usecase.Job[In, R], error)
	Get(ctx context.Context, boardID, jobID string) (usecase.Job[In, R], error)
	Discard(ctx context.Context, boardID, jobID string) error
}

// 以下の 3 つは、ジョブの一覧・取得・破棄の口の共通の形（ADR 0085 / 0091）。
// 種類ごとに違うのは、ジョブを境界の DTO に詰め替える関数だけ。
//
// **未設定（nil）の判定は呼ぶ側に残す。** nil のポインタをインターフェースに
// 入れると nil ではなくなるので、ここで見ると「設定されていない」が素通りする。

// listJobs は自分のジョブを返す。結果は含めない（toResponse に false を渡す）。
func listJobs[In, R, Out any](
	h *handlers, c *gin.Context, jobs jobStore[In, R],
	toResponse func(usecase.Job[In, R], bool) Out,
) {
	list, err := jobs.List(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}

	// nil を返すと JSON が null になる。常に配列にする。
	out := make([]Out, 0, len(list))
	for _, job := range list {
		out = append(out, toResponse(job, false))
	}
	c.JSON(http.StatusOK, out)
}

// getJob はジョブを 1 件、結果ごと返す。読んでも消えない（jobTable.get）。
func getJob[In, R, Out any](
	h *handlers, c *gin.Context, jobs jobStore[In, R],
	toResponse func(usecase.Job[In, R], bool) Out,
) {
	job, err := jobs.Get(c.Request.Context(), c.Param("id"), c.Param("jobId"))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, toResponse(job, true))
}

// discardJob はジョブを捨てる。走っていれば止める。
func discardJob[In, R any](h *handlers, c *gin.Context, jobs jobStore[In, R]) {
	if err := jobs.Discard(c.Request.Context(), c.Param("id"), c.Param("jobId")); err != nil {
		h.fail(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
