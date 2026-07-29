// Package httpapi は etoki の HTTP ルーティングとハンドラを提供する。
package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// NewRouter は etoki の HTTP ルーティングを構築する。
//
// ロギングミドルウェアは意図的に入れていない。構造化ログを導入する段で
// まとめて足す方が、途中で gin 既定のテキストログと二重になるのを避けられる。
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", handleHealthz)

	return r
}

// healthResponse は /healthz のレスポンスボディ。
type healthResponse struct {
	Status string `json:"status"`
}

// handleHealthz はプロセスが生きていることだけを返す。
// DB や外部サービスの疎通確認は含めない。etoki は自動で外部に触らないという
// 方針のため、ヘルスチェックが副作用を持たないようにしている。
func handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, healthResponse{Status: "ok"})
}
