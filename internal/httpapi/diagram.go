package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// generateDiagramDraft はプロンプトから図のドラフトを作って返す。
//
// **サーバーの状態を一切変えない**（ADR 0041）。DB にも GitHub にも触れず、
// 会話も保存しない。キャンバスに置くかどうかは結果を見た開発者が決める
// （中核思想 3）。
func (h *handlers) generateDiagramDraft(c *gin.Context) {
	if h.diagrams == nil {
		llmNotConfigured(c)
		return
	}

	req, ok := h.bindDiagramRequest(c)
	if !ok {
		return
	}

	draft, err := h.diagrams.Generate(c.Request.Context(), c.Param("id"), req)
	if err != nil {
		h.failDiagram(c, err)
		return
	}

	c.JSON(http.StatusOK, apitypes.DiagramDraft{
		Kind:           apitypes.DiagramKind(draft.Kind),
		Mermaid:        draft.Mermaid,
		TurnsRemaining: draft.TurnsRemaining,
	})
}

// maxDiagramBody は生成のリクエストボディを読む上限。
//
// **判定の正本はユースケース層**（usecase.MaxDiagramChatBytes）で、ここは
// 読み込みの歯止め。JSON の文字列は 1 バイトが `\u00XX` で 6 バイトになりうる
// ので、その最悪値で割り増して置く。2 倍で取ると、上限ちょうどの会話が歯止め
// 側で落ち、減らす必要のないものを減らせと言うことになる（ADR 0038 の
// maxSceneBody と同じ形）。
const maxDiagramBody = usecase.MaxDiagramChatBytes*6 + 4<<10

// bindDiagramRequest はリクエストボディを生成の入力に詰め替える。
//
// **値の検証はしない。** 種類も長さもユースケース層が見る。ハンドラにも置くと、
// API を通らない経路で抜ける（判定は 1 箇所）。
//
// 歯止めに引っかかったボディだけは別で、400 ではなく 413 に写す。**同じ
// 「積み上がりすぎた会話」が、ボディの大きさしだいで 400 と 413 に割れない
// ようにする**（bindSceneBody と同じ形）。写し替えの表は errors.go にあるので、
// ここは sentinel を選ぶだけ。
func (h *handlers) bindDiagramRequest(c *gin.Context) (usecase.DiagramRequest, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDiagramBody)

	var body apitypes.GenerateDiagramRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(c, fmt.Errorf("%w: request body exceeds %d bytes",
				usecase.ErrDiagramChatTooLong, maxDiagramBody))
			return usecase.DiagramRequest{}, false
		}

		h.badRequest(c, err)
		return usecase.DiagramRequest{}, false
	}

	req := usecase.DiagramRequest{
		Kind:   domain.DiagramKind(body.Kind),
		Prompt: body.Prompt,
	}
	if body.History != nil {
		req.History = make([]usecase.DiagramTurn, 0, len(*body.History))
		for _, t := range *body.History {
			req.History = append(req.History,
				usecase.DiagramTurn{Prompt: t.Prompt, Mermaid: t.Mermaid})
		}
	}

	return req, true
}

// failDiagram は生成のエラーを応答にする。
//
// 写し替えは errors.go の表が持つ。ここに残すのは記録だけ（failInterpret と
// 同じ形）。
func (h *handlers) failDiagram(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrLLMUnavailable):
		h.logger.ErrorContext(c.Request.Context(), "llm call failed",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))

	case errors.Is(err, usecase.ErrDiagramFailed):
		// 接続はできたが、上限まで投げ直しても図が返らなかった。
		h.logger.WarnContext(c.Request.Context(), "llm output rejected",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
	}

	h.fail(c, err)
}
