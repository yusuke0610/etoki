package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// interpretedItemResponse は解釈結果に含まれる draft issue 1 件。
type interpretedItemResponse struct {
	LocalID string `json:"localId"`
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Parent  string `json:"parentLocalId,omitempty"`
}

// interpretationResponse は解釈の結果。
//
// Summary は GitHub には作らない。LLM がこの囲みをどう解釈したかを開発者が
// 確かめるための材料として返す（ADR 0006）。
type interpretationResponse struct {
	Summary string                    `json:"summary"`
	Items   []interpretedItemResponse `json:"items"`
}

// interpretAnnotation は注釈を LLM に解釈させて結果を返す。
//
// このエンドポイントは GitHub に何も作らず、sync_runs にも書かない。何を作るかは
// 結果を見た開発者が別途トリガーする（中核思想 3）。
func (h *handlers) interpretAnnotation(c *gin.Context) {
	if h.interpretations == nil {
		// 404 ではなく 503。URL の誤りではなく設定の不足だと伝える必要がある。
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL",
		})
		return
	}

	in, err := h.interpretations.Interpret(
		c.Request.Context(), c.Param("id"), c.Param("annotationId"))
	if err != nil {
		h.failInterpret(c, err)
		return
	}

	c.JSON(http.StatusOK, toInterpretationResponse(in))
}

// failInterpret は解釈のエラーを HTTP ステータスに写す。
//
// LLM 側の失敗を 500 に丸めると、開発者は自分の設定を疑えない。上流の失敗と
// 分かるステータスとメッセージを返す。
func (h *handlers) failInterpret(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrBoardNotFound), errors.Is(err, usecase.ErrAnnotationNotFound):
		h.notFound(c)

	case errors.Is(err, usecase.ErrInvalidInput):
		h.badRequest(c, err)

	case errors.Is(err, usecase.ErrLLMUnavailable):
		// 認証や接続の失敗。API キーはアダプタ側でエラーに載せていない。
		h.logger.ErrorContext(c.Request.Context(), "llm call failed",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "llm call failed: " + err.Error()})

	case errors.Is(err, usecase.ErrInterpretationFailed):
		// 接続はできたが、上限まで再送しても出力がスキーマを満たさなかった。
		h.logger.WarnContext(c.Request.Context(), "llm output rejected",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})

	default:
		h.fail(c, err)
	}
}

// toInterpretationResponse はドメインの解釈結果を境界の DTO に詰め替える。
func toInterpretationResponse(in domain.Interpretation) interpretationResponse {
	out := interpretationResponse{
		Summary: in.Summary,
		// nil を返すと JSON が null になる。常に配列にする。
		Items: make([]interpretedItemResponse, 0, len(in.Items)),
	}

	for _, it := range in.Items {
		item := interpretedItemResponse{
			LocalID: it.LocalID,
			Kind:    string(it.Kind),
			Title:   it.Title,
			Body:    it.Body,
		}
		if it.ParentLocalID != nil {
			item.Parent = *it.ParentLocalID
		}
		out.Items = append(out.Items, item)
	}

	return out
}
