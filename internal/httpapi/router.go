// Package httpapi は etoki の HTTP ルーティングとハンドラを提供する。
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/usecase"
)

// Deps はルーティングが必要とするユースケース層。
type Deps struct {
	Boards      *usecase.BoardService
	Annotations *usecase.AnnotationService
	// Logger はリクエストとエラーの記録先。nil なら slog の既定を使う。
	Logger *slog.Logger
}

// NewRouter は etoki の HTTP ルーティングを構築する。
func NewRouter(deps Deps) *gin.Engine {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	r := gin.New()
	r.Use(gin.Recovery(), requestLogger(logger))

	r.GET("/healthz", handleHealthz)

	h := &handlers{boards: deps.Boards, annotations: deps.Annotations, logger: logger}

	api := r.Group("/api")
	{
		api.POST("/boards", h.createBoard)
		api.GET("/boards", h.listBoards)
		api.GET("/boards/:id", h.getBoard)
		api.PUT("/boards/:id/scene", h.saveScene)
		api.GET("/boards/:id/annotations", h.listAnnotations)
	}

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
