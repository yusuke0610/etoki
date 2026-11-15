package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// handlers はユースケース層への入口をまとめる。
type handlers struct {
	boards      *usecase.BoardService
	annotations *usecase.AnnotationService
	// interpretations は nil でもよい。その場合は 503 を返す。
	interpretations *usecase.InterpretationService
	// interpretationJobs は interpretations を、リクエストから切り離して走らせる
	// （ADR 0085）。nil の条件は interpretations と同じ。
	interpretationJobs *usecase.InterpretationJobs
	// diagrams はプロンプトからの図のドラフト生成。nil でもよい。
	//
	// interpretations と同じ LLM の設定で決まるが、**別々に持つ。** 使えるかを
	// 見せる口（capabilities）が答えているのは「解釈できるか」と「ドラフトを
	// 作れるか」という別の問いで、1 つに畳むと画面がどちらのボタンを止めれば
	// よいのかを決められなくなる。
	diagrams *usecase.DiagramService
	// diagramJobs は diagrams を、リクエストから切り離して走らせる
	// （ADR 0091）。nil の条件は diagrams と同じ。
	diagramJobs *usecase.DiagramJobs
	// creations は nil でもよい。その場合は 503 を返す。
	creations *usecase.CreationService
	// catalog は作成先の候補一覧。nil でもよい。その場合は 503 を返す。
	catalog *usecase.GitHubCatalogService
	// members はボードの共有。nil でもよい。その場合は 503 を返す。
	members *usecase.BoardMemberService
	// access はそのボードで何ができるか。GitHub が未設定でも組み立てる。
	//
	// GitHub 側を確かめられないことは「分からない」として返るので、ここを
	// nil にする理由が無い（ADR 0017）。
	access *usecase.BoardAccessService
	// auth はログインとセッション。nil なら認証しない。
	//
	// nil のときは /api/auth/session が authRequired: false を返し、画面は
	// ログインを求めない（ADR 0015）。
	auth *usecase.AuthService
	// oauth は MCP のクライアントのための認可サーバー（ADR 0076）。nil でもよい。
	//
	// nil のときは `/oauth/*` と `/.well-known/*` が 503 を返し、認証ありの構成の
	// `/mcp` は開かない（ADR 0071）。
	oauth *usecase.OAuthServer
	// oauthMetadataDocuments は Client ID Metadata Document で名乗るクライアントを
	// 受けるか。認可サーバーのメタデータに載せる。
	oauthMetadataDocuments bool
	// publicURL は認可から戻ってくる先の組み立てに使う。空ならリクエストの
	// Host から組む。
	publicURL string
	logger    *slog.Logger
	// hints は「設定されていない」ときに本文へ添える案内（Deps.SetupHints）。
	hints SetupHints
}

// fail はユースケース層のエラーを契約の code と HTTP ステータスに写す。
//
// 写し替えの表は errors.go に 1 つだけ置いてある。ここが決めるのは、表に無かった
// ときにどこへ落とすかだけ。
func (h *handlers) fail(c *gin.Context, err error) {
	if respondMapped(c, err) {
		return
	}

	// レスポンスには内部情報を載せないが、原因が分からないままだと
	// 手元で調べようがない。サーバー側には必ず残す。
	h.logger.ErrorContext(c.Request.Context(), "unhandled error",
		slog.String("path", c.Request.URL.Path),
		slog.Any("error", err),
	)
	errorJSON(c, http.StatusInternalServerError, apitypes.ErrorCodeInternal, "internal error")
}

// badRequest はリクエストの読み取りに失敗したことを返す。
//
// ボディのバインドはユースケース層に届く前に落ちるので、sentinel を持たない。
// 表を引かずにここで code を決めるのはこの経路だけ。
func (h *handlers) badRequest(c *gin.Context, err error) {
	errorJSON(c, http.StatusBadRequest, apitypes.ErrorCodeInvalidInput, err.Error())
}

// bindJSON は掛かっている上限（既定は router.go の defaultMaxBody、広げた口では
// widenBody で置いたもの）のもとで本文を読む。
//
// **歯止めに当たった失敗も契約の code に写す**（`.claude/rules/api-contract.md`）。
// 写さないと、同じ「大きすぎる」がボディの大きさしだいで 400 と 413 に割れ、
// 画面が同じ原因を 2 通りに案内することになる。
//
// シーン・解釈・図のドラフトは自分の上限と自分の sentinel を持つので、この
// ヘルパーは通らない（bindSceneBody / bindInterpretImages / bindDiagramRequest）。
// 読めなかったときの写し方は同じで、どれも failBind を通す。
func (h *handlers) bindJSON(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		h.failBind(c, err, errRequestTooLarge)
		return false
	}
	return true
}

// failBind は本文を読めなかったことを返す。
//
// **歯止め（`http.MaxBytesReader`）に当たった失敗は tooLarge に包んで 413 に写し**、
// それ以外は 400 にする。写さないと、同じ「大きすぎる」がボディの大きさしだいで
// 400 と 413 に割れる（`.claude/rules/api-contract.md`）。**本文を読む口はすべて
// ここを通す。** 口ごとに写し方を書くと、足した口だけが写し忘れる。
//
// tooLarge は口ごとに違う。どれに写すかで画面の案内が変わる（シーンは減らす、
// 会話はやり直す、ADR 0038）ので、ここでは選ばない。写し替えの表は errors.go。
func (h *handlers) failBind(c *gin.Context, err, tooLarge error) {
	var overLimit *http.MaxBytesError
	if errors.As(err, &overLimit) {
		h.fail(c, fmt.Errorf("%w: request body exceeds %d bytes", tooLarge, overLimit.Limit))
		return
	}

	h.badRequest(c, err)
}
