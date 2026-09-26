package httpapi

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/usecase"
)

// failLLM は LLM を叩く実行（解釈と図のドラフト生成）のエラーを応答にする。
//
// 写し替えは errors.go の表が持つ。LLM 側の失敗を 500 に丸めないのはそちらの
// 責任で、ここに残すのは記録だけ。
//
// rejected は「接続はできたが、上限まで投げ直しても出力を受け付けられなかった」
// ことを表す sentinel（解釈は usecase.ErrInterpretationFailed、図の生成は
// usecase.ErrDiagramFailed）。記録の出し分けで違うのはそこだけなので、関数は
// 1 つにして sentinel を呼び出し側が渡す（#156）。
func (h *handlers) failLLM(c *gin.Context, err, rejected error) {
	switch {
	case errors.Is(err, usecase.ErrLLMUnavailable):
		// 認証や接続の失敗。API キーはアダプタ側でエラーに載せていない。
		h.logger.ErrorContext(c.Request.Context(), "llm call failed",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))

	case errors.Is(err, rejected):
		h.logger.WarnContext(c.Request.Context(), "llm output rejected",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
	}

	h.fail(c, err)
}
