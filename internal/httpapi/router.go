// Package httpapi は etoki の HTTP ルーティングとハンドラを提供する。
package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
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
	// Catalog は作成先の候補一覧。nil でもよい。扱いは Creations と同じ。
	Catalog *usecase.GitHubCatalogService
	// Members はボードの共有。nil でもよい。
	//
	// nil のときはメンバーのエンドポイントが 503 を返す。招待は「誰であるか」が
	// 決まって初めて意味を持つので、認証を設定していない構成では組み立てない
	// （ADR 0017）。
	Members *usecase.BoardMemberService
	// Access はそのボードで何ができるか。
	//
	// GitHub が未設定でも組み立てる。確かめられないことは「分からない」として
	// 返るので、GitHub の有無で落とす理由が無い（ADR 0017）。nil のときは
	// 503 を返すが、組み立て口（etoki.New）は必ず渡す。
	Access *usecase.BoardAccessService
	// Auth はログインとセッション。nil なら認証しない。
	//
	// nil のときは既存の挙動のまま。全エンドポイントが素通しになり、
	// /api/auth/session は authRequired: false を返す（ADR 0015）。
	Auth *usecase.AuthService
	// PublicURL は認可から戻ってくる先の組み立てに使う。
	//
	// 空ならリクエストの Host から組む。make dev ではブラウザが :5173 にいて
	// Vite が Host を書き換えずに転送するので、それで正しい値になる。
	PublicURL string
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
		catalog:         deps.Catalog,
		members:         deps.Members,
		access:          deps.Access,
		auth:            deps.Auth,
		publicURL:       deps.PublicURL,
		logger:          logger,
	}

	// セッションの解決はここで一度だけ。弾くのは requireAuth の仕事で、
	// ここでは載せるだけ。/api/auth/session は未ログインでも 200 を返す。
	r.Use(resolveSession(deps.Auth, logger))

	auth := r.Group("/api/auth")
	{
		auth.GET("/session", h.getSession)
		// state の発行は書き込みなので POST。GET にすると外部ページから
		// 叩ける（ADR 0013 / 0015）。
		auth.POST("/login", h.startLogin)
		// etoki で唯一、副作用を持つ GET。守るのは Origin ではなく state。
		auth.GET("/callback", h.completeLogin)
		auth.POST("/logout", h.logout)
	}

	api := r.Group("/api", requireAuth(deps.Auth))
	{
		// いま使える機能。設定していない機能を押す前に見せるために引く
		// （ADR 0008 の帰結）。**認証の内側に置く。** プロセスの設定を
		// 未ログインの相手に並べて見せる理由が無い。
		api.GET("/capabilities", h.getCapabilities)

		api.POST("/boards", h.createBoard)
		api.GET("/boards", h.listBoards)
		api.GET("/boards/:id", h.getBoard)
		api.PUT("/boards/:id/scene", h.saveScene)
		// 作成先はボードごとに持つ。最初の draft issue を作ると固定される
		// （ADR 0014）。
		api.PUT("/boards/:id/target", h.setBoardTarget)
		api.GET("/boards/:id/annotations", h.listAnnotations)

		// そのボードで何ができるか。ボード取得とは別に置く。GitHub が
		// 未設定・不通でもボードは開ける必要がある（ADR 0017）。
		api.GET("/boards/:id/access", h.getBoardAccess)

		// 共有。招待される側にリポジトリのアクセス権は要らない（ADR 0017）。
		api.GET("/boards/:id/members", h.listBoardMembers)
		api.POST("/boards/:id/members", h.inviteBoardMember)
		api.PUT("/boards/:id/members/:userId", h.setBoardMemberRole)
		api.DELETE("/boards/:id/members/:userId", h.removeBoardMember)
		// 解釈と作成は別のエンドポイントに保つ。解釈結果を見た開発者が
		// 明示的に作成を叩く（中核思想 3）。
		api.POST("/boards/:id/annotations/:annotationId/interpret", h.interpretAnnotation)
		api.POST("/boards/:id/annotations/:annotationId/items", h.createItems)

		// 作成先を選ぶための一覧。ここで選んだ Project をボードに設定する。
		api.GET("/github/repositories", h.listRepositories)
		api.GET("/github/repositories/:owner/:repo/projects", h.listRepositoryProjects)
	}

	return r
}

// handleHealthz はプロセスが生きていることだけを返す。
// DB や外部サービスの疎通確認は含めない。etoki は自動で外部に触らないという
// 方針のため、ヘルスチェックが副作用を持たないようにしている。
func handleHealthz(c *gin.Context) {
	c.JSON(http.StatusOK, apitypes.HealthResponse{Status: "ok"})
}
