package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// createItems は解釈結果から draft issue を作る。
//
// リクエストボディは解釈のエンドポイントが返したものそのもの。開発者が確認した
// 結果を作る流れなのでサーバー側で解釈し直さない。ただし内容は信用せず、
// ユースケース層で検証し直す。
//
// 解釈とは別のエンドポイントに保つ。解釈結果を見た開発者が明示的に叩く
// （中核思想 3）。
func (h *handlers) createItems(c *gin.Context) {
	if h.creations == nil {
		errorJSON(c, http.StatusServiceUnavailable, githubNotConfigured)
		return
	}

	var req apitypes.Interpretation
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

	out := toCreatedRun(*run)
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

	case errors.Is(err, usecase.ErrProjectFieldMissing),
		errors.Is(err, usecase.ErrTargetNotSelected):
		// 設定不足であって、リクエストの誤りではない。何をすればよいかを返す。
		errorJSON(c, http.StatusUnprocessableEntity, err.Error())

	case errors.Is(err, usecase.ErrContentHashMismatch),
		errors.Is(err, usecase.ErrPreviousItemUnknown):
		// どちらも「解釈が古い」。解釈のやり直しは開発者が決めるので、ここで
		// 解釈し直して作成を続けない。
		errorJSON(c, http.StatusConflict, err.Error())

	case errors.Is(err, usecase.ErrCreationIncomplete):
		// 1 件も作れずに失敗した場合。GitHub 側の問題として返す。
		h.logger.ErrorContext(c.Request.Context(), "creation failed",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		errorJSON(c, http.StatusBadGateway, err.Error())

	default:
		h.fail(c, err)
	}
}

// toInterpretation は境界の DTO をドメインの解釈結果に詰め替える。
func toInterpretation(req apitypes.Interpretation) domain.Interpretation {
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
		if it.ParentLocalID != "" {
			parent := it.ParentLocalID
			item.ParentLocalID = &parent
		}
		in.Items = append(in.Items, item)
	}

	return in
}

// toCreatedRun は保存した run を境界の DTO に詰め替える。
func toCreatedRun(run port.SyncRun) apitypes.CreatedRun {
	return apitypes.CreatedRun{
		RunID:     run.ID,
		CreatedAt: run.CreatedAt,
		Items:     toSyncItems(run.Items),
	}
}
