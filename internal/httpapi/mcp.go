package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
)

// mcpPath は MCP の入口。`/api` とは分ける（ADR 0071）。MCP のクライアントは
// 画面を意識する必要が無く、画面の配信や `/api` の DTO の都合と切り離しておく。
const mcpPath = "/mcp"

// MCP の道具の入出力。
//
// **出力は `/api` と同じ生成型を使う**（ADR 0011 / 0071）。配列を返す口だけは
// オブジェクトで包む。MCP の 2025-06-18 版と 2025-11-25 版は、構造化された結果と
// 出力のスキーマをオブジェクトと定めている。配列を許したのはその後の版
// （SEP-2106）で、追いついていないクライアントにも読めるようにしておく。
// 包むのは 1 段だけで、中身は `/api` の応答そのもの。

type mcpNoInput struct{}

type mcpBoardInput struct {
	BoardID string `json:"boardId" jsonschema:"ボードの ID。list_boards の id"`
}

type mcpAnnotationInput struct {
	BoardID      string `json:"boardId" jsonschema:"ボードの ID。list_boards の id"`
	AnnotationID string `json:"annotationId" jsonschema:"注釈の ID。list_annotations の annotations[].id か detached[].id"`
}

type mcpBoardsOutput struct {
	Boards []apitypes.BoardListEntry `json:"boards" jsonschema:"GET /api/boards と同じ一覧"`
}

type mcpRunsOutput struct {
	Runs []apitypes.SyncRun `json:"runs" jsonschema:"GET /api/boards/{id}/annotations/{annotationId}/runs と同じ履歴"`
}

// mcpEntrance は `/mcp` の入口を返す。
//
//   - 認証なしの構成：道具をそのまま開く（ADR 0071）。
//   - 認証ありで認可サーバーがある：etoki が発行したトークンで通す（ADR 0076）。
//   - 認証ありで認可サーバーが無い：開かずに 503。利用者を決める手段が無いまま
//     開くと、ログインせずに全ボードが読める。
func mcpEntrance(h *handlers, authConfigured bool) gin.HandlerFunc {
	switch {
	case !authConfigured:
		return gin.WrapH(newMCPHandler(h))
	case h.oauth != nil:
		return h.requireBearer(newMCPHandler(h))
	default:
		return gin.WrapH(mcpUnavailable())
	}
}

// newMCPHandler は `/mcp` の道具を返す。
//
// **公開するのは読み取りの 3 つだけ**（ADR 0071）。解釈と作成は出さない。
// 解釈の画像はブラウザでしか作れず（ADR 0018）、作成は作るものを選んで
// 手直しする人の確認（ADR 0024）をどこに挟むかが決まっていない。
func newMCPHandler(h *handlers) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "etoki", Version: buildVersion()}, nil)

	// 読むのは etoki の DB だけで、GitHub にも LLM にも出ていかない。
	closedWorld := false
	readOnly := &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  &closedWorld,
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_boards",
		Description: "etoki のボードの一覧を返す。作成先（リポジトリと Projects v2）と、" +
			"注釈の 3 状態（uncreated / created / changed）の件数を含む。",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ mcpNoInput) (
		*mcp.CallToolResult, mcpBoardsOutput, error,
	) {
		boards, err := h.boardList(ctx)
		if err != nil {
			return nil, mcpBoardsOutput{}, h.toolError(ctx, "list_boards", err)
		}
		return nil, mcpBoardsOutput{Boards: boards}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_annotations",
		Description: "ボードの注釈ごとの 3 状態と、GitHub に作った draft issue を返す。" +
			"シーンから消えたが GitHub にものが残っている注釈は detached に分けて返す。" +
			"判定は保存済みのシーンが基準で、画面で編集中の内容は含まない。",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpBoardInput) (
		*mcp.CallToolResult, apitypes.BoardAnnotations, error,
	) {
		out, err := h.boardAnnotations(ctx, in.BoardID)
		if err != nil {
			return nil, apitypes.BoardAnnotations{}, h.toolError(ctx, "list_annotations", err)
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_annotation_runs",
		Description: "注釈 1 つの実行の履歴を、新しい順に 1 回ずつ返す。いま GitHub に在るものは " +
			"list_annotations の items が持つ。こちらはいつ何回に分けて作ったかの記録。",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mcpAnnotationInput) (
		*mcp.CallToolResult, mcpRunsOutput, error,
	) {
		runs, err := h.annotationRuns(ctx, in.BoardID, in.AnnotationID)
		if err != nil {
			return nil, mcpRunsOutput{}, h.toolError(ctx, "list_annotation_runs", err)
		}
		return nil, mcpRunsOutput{Runs: runs}, nil
	})

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{
			// セッションを持たない。道具は読み取りだけで、サーバーからクライアントへ
			// 送るものが無い。持つと、再起動で消えるセッションの扱いを抱える。
			Stateless:    true,
			JSONResponse: true,
			// **Host の検証は originGuard 1 つに任せる**（ADR 0013）。SDK の守りは
			// ループバックに届いた非ループバックの Host を弾くが、ETOKI_ALLOWED_ORIGINS
			// で許したホストまで弾くので、`/api` は通るのに `/mcp` だけ 403 になる
			// 構成ができる。同じ問いに 2 つの答えを持たせない。外したぶんは
			// TestMCP_RejectsCrossSite が見ている。
			DisableLocalhostProtection: true,
			// 本文の上限は `/api` の既定と同じ。道具の引数は ID だけなので足りる。
			MaxRequestBodyBytes: defaultMaxBody,
			Logger:              h.logger,
		})
}

// toolError は道具の失敗を、`/api` と同じ code を頭に付けた文にする。
//
// **写し替えの表は errors.go の 1 つを引く。** 本文も `/api` と同じ規則
// （hintFor）で、見つからないときは理由を載せない（ADR 0016 / 0017）。
// SDK は返した error を道具の失敗（isError）として包む。
func (h *handlers) toolError(ctx context.Context, tool string, err error) error {
	if m, ok := lookupError(err); ok {
		return fmt.Errorf("%s: %s", m.code, hintFor(m, err))
	}

	h.logger.ErrorContext(ctx, "unhandled error",
		slog.String("path", mcpPath),
		slog.String("tool", tool),
		slog.Any("error", err),
	)
	return fmt.Errorf("%s: internal error", apitypes.ErrorCodeInternal)
}

// mcpUnavailable は認証ありの構成で、認可サーバー（ADR 0076）を組み立てて
// いないときに `/mcp` に来たリクエストへの応答。
//
// **404 ではなく 503。** URL の誤りではなく、この構成では開いていないことを
// 伝える（ADR 0030 の 503 と同じ区別）。
//
// **本文は `ErrorResponse` ではなく text/plain にする。** `/mcp` で SDK 自身が
// 返すエラー（405 / 413 など）が text/plain なので、同じ口で形を揃える。
// `ErrorResponse` の code は画面が日本語を引くための列挙で（ADR 0034）、画面は
// `/mcp` を叩かない。読む人のいない code を契約に足さない（ADR 0030）。
func mcpUnavailable() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w,
			"mcp is not available: authentication is configured "+
				"but no OAuth grant repository is set up for MCP clients",
			http.StatusServiceUnavailable)
	})
}

// buildVersion は MCP の初期化で名乗る版。etoki はリリースを切っていないので、
// ビルド情報にある版（Go が VCS の情報から付けたもの）をそのまま渡す。取れ
// なければ "(devel)"。
func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
