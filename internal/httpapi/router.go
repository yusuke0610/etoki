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
	// Interpretations は注釈の解釈。nil でもよい。
	//
	// nil のときは解釈のエンドポイントが 503 を返す。ルート自体は生やす。
	// 404 だと「機能が無い」のか「URL が違う」のか区別できない。
	Interpretations *usecase.InterpretationService
	// Creations は draft issue の作成。nil でもよい。
	//
	// nil のときは作成のエンドポイントが 503 を返す。理由は Interpretations と
	// 同じで、ルート自体は生やす。
	Creations *usecase.CreationService
	// Logger はリクエストとエラーの記録先。nil なら slog の既定を使う。
	Logger *slog.Logger
	// AllowedOrigins はループバック以外に追加で許すオリジン。
	//
	// ループバックは常に許すので、既定の構成では空でよい。ETOKI_ADDR で
	// 公開インターフェースにバインドした利用者が、自分で自分を締め出さない
	// ための逃げ道。
	AllowedOrigins []string
}

// NewRouter は etoki の HTTP ルーティングを構築する。
func NewRouter(deps Deps) *gin.Engine {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	r := gin.New()
	// 検証はログの後に置く。弾いたリクエストも記録に残す。何を叩かれたかが
	// 分からないと、弾けていること自体を確かめられない。
	r.Use(gin.Recovery(), requestLogger(logger),
		newOriginGuard(deps.AllowedOrigins).handler(logger))

	r.GET("/healthz", handleHealthz)

	h := &handlers{
		boards:          deps.Boards,
		annotations:     deps.Annotations,
		interpretations: deps.Interpretations,
		creations:       deps.Creations,
		logger:          logger,
	}

	api := r.Group("/api")
	{
		api.POST("/boards", h.createBoard)
		api.GET("/boards", h.listBoards)
		api.GET("/boards/:id", h.getBoard)
		api.PUT("/boards/:id/scene", h.saveScene)
		api.GET("/boards/:id/annotations", h.listAnnotations)
		// 解釈と作成は別のエンドポイントに保つ。解釈結果を見た開発者が
		// 明示的に作成を叩く（中核思想 3）。
		api.POST("/boards/:id/annotations/:annotationId/interpret", h.interpretAnnotation)
		api.POST("/boards/:id/annotations/:annotationId/items", h.createItems)
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
