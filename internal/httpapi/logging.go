package httpapi

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// requestLogger はリクエストを 1 行ずつ記録する。
//
// gin 標準の Logger ではなく自前で持つのは、アプリ側のエラーログと同じ
// slog に流して出力先とフォーマットを揃えるため。
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		// ヘルスチェックは高頻度で叩かれうるうえ情報量がないので落とす。
		if c.Request.URL.Path == "/healthz" {
			return
		}

		logger.LogAttrs(c.Request.Context(), levelFor(c.Writer.Status()), "request",
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("took", time.Since(start)),
		)
	}
}

// levelFor はステータスコードからログレベルを決める。
// 5xx だけを error にし、4xx は呼び出し側の問題なので warn に留める。
func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
