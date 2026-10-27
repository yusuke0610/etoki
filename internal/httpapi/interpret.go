package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// interpretAnnotation は注釈の解釈を始めて、ジョブを返す。LLM の応答は待たない。
//
// このエンドポイントは GitHub に何も作らず、sync_runs にも書かない。何を作るかは
// 結果を見た開発者が別途トリガーする（中核思想 3）。
//
// **解釈をリクエストに結びつけない**（ADR 0085）。結びつけると、画面を離れた時点で
// 受け取り手がいなくなるのにサーバーは走り続けて枠を握り、画面は終わったつもりで
// 再実行して 429 に弾かれる。
func (h *handlers) interpretAnnotation(c *gin.Context) {
	if h.interpretationJobs == nil {
		h.llmNotConfigured(c)
		return
	}

	images, ok := h.bindInterpretImages(c)
	if !ok {
		return
	}

	job, err := h.interpretationJobs.Start(
		c.Request.Context(), c.Param("id"), c.Param("annotationId"), images)
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusAccepted, toInterpretationJobResponse(job, false))
}

// listInterpretationJobs は自分の解釈のジョブを返す。結果は含めない。
func (h *handlers) listInterpretationJobs(c *gin.Context) {
	if h.interpretationJobs == nil {
		h.llmNotConfigured(c)
		return
	}
	listJobs(h, c, h.interpretationJobs, toInterpretationJobResponse)
}

// getInterpretationJob はジョブを 1 件返す。読んでも消えない。
func (h *handlers) getInterpretationJob(c *gin.Context) {
	if h.interpretationJobs == nil {
		h.llmNotConfigured(c)
		return
	}
	getJob(h, c, h.interpretationJobs, toInterpretationJobResponse)
}

// discardInterpretationJob はジョブを捨てる。走っていれば止める。
func (h *handlers) discardInterpretationJob(c *gin.Context) {
	if h.interpretationJobs == nil {
		h.llmNotConfigured(c)
		return
	}
	discardJob(h, c, h.interpretationJobs)
}

// toInterpretationJobResponse はジョブを境界の DTO に詰め替える。
//
// 失敗の理由は、リクエストの失敗と同じ表（errors.go）で code に写す。写し替えを
// 2 つ持つと、ジョブの失敗だけ画面の対応表から漏れる。withResult が false なら
// 結果は載せない（一覧）。
func toInterpretationJobResponse(job usecase.InterpretationJob, withResult bool) apitypes.InterpretationJob {
	out := apitypes.InterpretationJob{
		ID:           job.ID,
		AnnotationID: job.Input.AnnotationID,
		Status:       apitypes.InterpretationJobStatus(job.Status),
		StartedAt:    job.StartedAt,
	}

	if withResult && job.Result != nil {
		result := toInterpretationResponse(*job.Result)
		out.Result = &result
	}
	if job.Err != nil {
		out.Error = jobErrorResponse(job.Err)
	}
	return out
}

// jobErrorResponse はジョブの失敗を、失敗した応答の本文と同じ形にする。
func jobErrorResponse(err error) *apitypes.ErrorResponse {
	if m, ok := lookupError(err); ok {
		return &apitypes.ErrorResponse{Code: m.code, Error: hintFor(m, err)}
	}
	return &apitypes.ErrorResponse{Code: apitypes.ErrorCodeInternal, Error: err.Error()}
}

// maxInterpretBody は解釈のリクエストボディを読む上限。
//
// 画像そのものの上限はユースケース層が持つ（usecase.MaxImageBytes）。ここで
// 重ねて持つのは、上限を超えたボディを全部メモリに載せてからでないと判定できない
// のを避けるため。base64 は 4/3 に膨らむので、その分と JSON の包みを足して余裕を
// 取る。判定の正本はあくまでユースケース層側で、ここは読み込みの歯止めである。
const maxInterpretBody = usecase.MaxImageBytes*usecase.MaxImages*4/3 + 4<<10

// bindInterpretImages はリクエストボディから LLM に渡す画像を取り出す。
//
// ボディごと省略できる（ADR 0018）。省略されたときは画像なしで解釈する。
func (h *handlers) bindInterpretImages(c *gin.Context) ([]port.Image, bool) {
	widenBody(c, maxInterpretBody)

	var req apitypes.InterpretRequest
	// 空のボディは「画像なし」であってエラーではない。JSON デコーダは
	// その場合に io.EOF を返す。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		h.failBind(c, err, errRequestTooLarge)
		return nil, false
	}

	if req.Image == nil {
		return nil, true
	}

	// バイト数と形式はユースケース層が見る。ここは詰め替えるだけ。判定を
	// ハンドラにも置くと、API を通らない経路で抜ける。
	return []port.Image{{
		MediaType: string(req.Image.MediaType),
		Data:      req.Image.Data,
	}}, true
}

// toInterpretationResponse はドメインの解釈結果を境界の DTO に詰め替える。
func toInterpretationResponse(result usecase.InterpretationResult) apitypes.Interpretation {
	out := apitypes.Interpretation{
		Summary:     result.Interpretation.Summary,
		ContentHash: result.ContentHash,
		// nil を返すと JSON が null になる。常に配列にする。
		Items: make([]apitypes.InterpretedItem, 0, len(result.Interpretation.Items)),
	}

	for _, it := range result.Interpretation.Items {
		item := apitypes.InterpretedItem{
			LocalID: it.LocalID,
			Kind:    apitypes.ItemKind(it.Kind),
			Title:   it.Title,
			Body:    it.Body,
		}
		if it.ParentLocalID != nil {
			item.ParentLocalID = *it.ParentLocalID
		}
		// LLM が対応づけた候補。決めるのは開発者で、確認画面で外せる
		// （中核思想 3、ADR 0026）。
		if it.PreviousItemID != nil {
			item.PreviousItemID = *it.PreviousItemID
		}
		out.Items = append(out.Items, item)
	}

	return out
}
