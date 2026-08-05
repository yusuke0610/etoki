package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// createItemsRequest は作成する内容。解釈結果をそのまま送る。
//
// 開発者が確認した結果を作る、という流れなので、サーバー側で解釈し直さない。
// ただし内容は信用せず、ユースケース層で検証し直す。
type createItemsRequest struct {
	Summary     string                    `json:"summary"`
	ContentHash string                    `json:"contentHash"`
	Items       []interpretedItemResponse `json:"items"`
}

// createItemsResponse は作成した run。
type createItemsResponse struct {
	RunID     int64              `json:"runId"`
	CreatedAt time.Time          `json:"createdAt"`
	Items     []syncItemResponse `json:"items"`

	// Incomplete は途中で失敗したことを表す。作られたぶんは Items に入る。
	Incomplete bool   `json:"incomplete,omitempty"`
	Error      string `json:"error,omitempty"`
}

// createItems は解釈結果から draft issue を作る。
//
// 解釈とは別のエンドポイントに保つ。解釈結果を見た開発者が明示的に叩く
// （中核思想 3）。
func (h *handlers) createItems(c *gin.Context) {
	if h.creations == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "github is not configured: set ETOKI_GITHUB_TOKEN and ETOKI_GITHUB_PROJECT_ID",
		})
		return
	}

	var req createItemsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		h.badRequest(c, err)
		return
	}

	run, err := h.creations.Create(
		c.Request.Context(), c.Param("id"), c.Param("annotationId"), req.ContentHash, toInterpretation(req))

	// 途中まで作れた場合は run が返る。エラーだけ返すと、開発者は何も作られて
	// いないと誤解して再実行し、重複を増やす（ADR 0009）。
	//
	// err ではなく run で分岐する。この下で run を参照するので、戻り値の
	// 組み合わせが変わっても panic にならないようにしておく。
	if run == nil {
		if err == nil {
			err = errors.New("creation returned no run")
		}
		h.failCreate(c, err)
		return
	}

	out := toCreateItemsResponse(*run)
	if err != nil {
		h.logger.ErrorContext(c.Request.Context(), "creation did not complete",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		out.Incomplete = true
		out.Error = err.Error()
	}

	c.JSON(http.StatusCreated, out)
}

// failCreate は作成のエラーを HTTP ステータスに写す。
func (h *handlers) failCreate(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrBoardNotFound), errors.Is(err, usecase.ErrAnnotationNotFound):
		h.notFound(c)

	case errors.Is(err, usecase.ErrInvalidInput):
		h.badRequest(c, err)

	case errors.Is(err, usecase.ErrProjectFieldMissing):
		// 設定不足であって、リクエストの誤りではない。何を作ればよいかを返す。
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})

	case errors.Is(err, usecase.ErrContentHashMismatch):
		// 解釈のやり直しは開発者が決める。ここで解釈し直して作成を続けない。
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})

	case errors.Is(err, usecase.ErrCreationIncomplete):
		// 1 件も作れずに失敗した場合。GitHub 側の問題として返す。
		h.logger.ErrorContext(c.Request.Context(), "creation failed",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})

	default:
		h.fail(c, err)
	}
}

// toInterpretation は境界の DTO をドメインの解釈結果に詰め替える。
func toInterpretation(req createItemsRequest) domain.Interpretation {
	in := domain.Interpretation{
		Summary: req.Summary,
		Items:   make([]domain.InterpretedItem, 0, len(req.Items)),
	}

	for _, it := range req.Items {
		item := domain.InterpretedItem{
			LocalID: it.LocalID,
			Kind:    domain.ItemKind(it.Kind),
			Title:   it.Title,
			Body:    it.Body,
		}
		if it.Parent != "" {
			parent := it.Parent
			item.ParentLocalID = &parent
		}
		in.Items = append(in.Items, item)
	}

	return in
}

// toCreateItemsResponse は保存した run を境界の DTO に詰め替える。
func toCreateItemsResponse(run port.SyncRun) createItemsResponse {
	out := createItemsResponse{
		RunID:     run.ID,
		CreatedAt: run.CreatedAt,
		Items:     make([]syncItemResponse, 0, len(run.Items)),
	}

	for _, it := range run.Items {
		item := syncItemResponse{
			ItemID:  it.ItemID,
			Kind:    string(it.Kind),
			Title:   it.Title,
			LocalID: it.LocalID,
		}
		if it.ParentLocalID != nil {
			item.Parent = *it.ParentLocalID
		}
		out.Items = append(out.Items, item)
	}

	return out
}
