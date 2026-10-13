package httpapi

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/usecase"
)

// statusClientClosedRequest は、待っていた相手がいなくなったときの記録用の
// ステータス（nginx の 499 に倣う）。
//
// **相手には届かない。** 契約（api/openapi.yaml）には載せない。載せても画面は
// 読めない応答を受け取る側にいない。リクエストログのステータスで、障害の 5xx と
// 区別するためだけに置く。
const statusClientClosedRequest = 499

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
	case errors.Is(err, usecase.ErrLLMUnavailable) &&
		errors.Is(err, context.Canceled) && c.Request.Context().Err() != nil:
		// 解釈は 1〜数分かかるので、待っている間のリロードや離脱で起きる。上流の
		// 障害ではなく、利用者が止めただけ。**リクエストの ctx が切れていることも
		// 条件にする。** ctx が生きているのに Canceled で返るものは、こちらの都合で
		// 切ったものではないので、障害として扱う。
		h.logger.InfoContext(c.Request.Context(), "llm call cancelled by client",
			slog.String("path", c.Request.URL.Path))
		c.AbortWithStatus(statusClientClosedRequest)
		return

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
