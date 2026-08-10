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
//
// 記録は c.Next() の後で行う。利用者を解決するのは後段のミドルウェアなので、
// 先に書くと「誰が」が入らない。
func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		// ヘルスチェックは高頻度で叩かれうるうえ情報量がないので落とす。
		if c.Request.URL.Path == "/healthz" {
			return
		}

		attrs := []slog.Attr{
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Duration("took", time.Since(start)),
		}
		// 誰が何をしたかを残す。ADR 0004 が「共有すると壊れる」として挙げた
		// 監査ログの不在は、これで埋まる（ADR 0016）。専用の表は作らない。
		// 全リクエストを 1 行ずつ記録している以上、足りないのは「誰が」だけ。
		if user, ok := currentUser(c); ok {
			attrs = append(attrs, slog.String("user", user.Login))
		}

		logger.LogAttrs(c.Request.Context(), levelFor(c.Writer.Status()), "request", attrs...)
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
