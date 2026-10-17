package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// 設定の案内（何を設定すればよいか）は配線から受け取り、本文に載せる（ADR 0083）。
//
// **渡されていなければ環境変数の名前を出さない。** etoki.New に自前の実装を
// 差し込む構成は ETOKI_* を読まないので、名前を出すと存在しない設定を案内する。
// 逆に渡されたのに載らなければ、手元で使う人が何を設定すればよいかを辿れない。
// どちらの向きも、`*_not_configured` を返す口ごとに固定する。
func TestSetupHints(t *testing.T) {
	t.Parallel()

	// 案内文はテストのための値にする。ETOKI_ の名前にしておくと、名前を
	// 決め打ちで書いた実装でも「渡された案内が載った」に見えて通る。
	hints := httpapi.SetupHints{
		LLM:            "hint-for-llm",
		GitHub:         "hint-for-github",
		Auth:           "hint-for-auth",
		AllowedOrigins: "hint-for-origins",
	}

	tests := []struct {
		name   string
		method string
		path   string
		host   string
		status int
		code   apitypes.ErrorCode
		hint   string
	}{
		{
			name: "解釈", method: http.MethodPost,
			path:   "/api/boards/b/annotations/a/interpret",
			status: http.StatusServiceUnavailable, code: apitypes.ErrorCodeLlmNotConfigured,
			hint: hints.LLM,
		},
		{
			name: "図のドラフト", method: http.MethodPost, path: "/api/boards/b/diagram-draft",
			status: http.StatusServiceUnavailable, code: apitypes.ErrorCodeLlmNotConfigured,
			hint: hints.LLM,
		},
		{
			name: "作成", method: http.MethodPost, path: "/api/boards/b/annotations/a/items",
			status: http.StatusServiceUnavailable, code: apitypes.ErrorCodeGithubNotConfigured,
			hint: hints.GitHub,
		},
		{
			name: "作成先の候補", method: http.MethodGet, path: "/api/github/repositories",
			status: http.StatusServiceUnavailable, code: apitypes.ErrorCodeGithubNotConfigured,
			hint: hints.GitHub,
		},
		{
			name: "ログイン", method: http.MethodPost, path: "/api/auth/login",
			status: http.StatusServiceUnavailable, code: apitypes.ErrorCodeAuthNotConfigured,
			hint: hints.Auth,
		},
		{
			name: "共有", method: http.MethodGet, path: "/api/boards/b/members",
			status: http.StatusServiceUnavailable, code: apitypes.ErrorCodeSharingNotConfigured,
			hint: hints.Auth,
		},
		{
			name: "MCP の許可", method: http.MethodGet, path: "/api/oauth/grants",
			status: http.StatusServiceUnavailable,
			code:   apitypes.ErrorCodeMcpConnectionsNotConfigured,
			hint:   hints.Auth,
		},
		{
			// ループバック以外の Host。許すオリジンを足すのが案内になる。
			name: "cross-site の拒否", method: http.MethodGet, path: "/api/boards",
			host:   "etoki.example.com",
			status: http.StatusForbidden, code: apitypes.ErrorCodeCrossSiteRejected,
			hint: hints.AllowedOrigins,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("渡せば載る", func(t *testing.T) {
				t.Parallel()

				body := requestUnconfigured(t, hints, tc.method, tc.path, tc.host, tc.status, tc.code)
				if !strings.HasSuffix(body, ": "+tc.hint) {
					t.Errorf("error = %q, want suffix %q", body, ": "+tc.hint)
				}
				// 別の機能の案内を混ぜていないこと。共有に LLM の案内が載る、の
				// ような取り違えは接尾辞だけでは見えない。
				for _, other := range []string{
					hints.LLM, hints.GitHub, hints.Auth, hints.AllowedOrigins,
				} {
					if other != tc.hint && strings.Contains(body, other) {
						t.Errorf("error = %q, contains another hint %q", body, other)
					}
				}
			})

			t.Run("渡さなければ名前を出さない", func(t *testing.T) {
				t.Parallel()

				body := requestUnconfigured(t, httpapi.SetupHints{},
					tc.method, tc.path, tc.host, tc.status, tc.code)
				if strings.Contains(body, "ETOKI_") {
					t.Errorf("error = %q, names an environment variable without a hint", body)
				}
				// 案内が無いときに区切りだけ残すと、続きが欠けた文に見える。
				if strings.HasSuffix(strings.TrimSpace(body), ":") {
					t.Errorf("error = %q, ends with a dangling separator", body)
				}
			})
		})
	}
}

// requestUnconfigured は、任意の依存をすべて nil にしたルーターに 1 回投げ、
// status と code を確かめてから error 本文を返す。
func requestUnconfigured(
	t *testing.T, hints httpapi.SetupHints,
	method, path, host string, status int, code apitypes.ErrorCode,
) string {
	t.Helper()

	boards, mappings := newRepos(t)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks()),
		Annotations: usecase.NewAnnotationService(boards, mappings),
		SetupHints:  hints,
	})

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Host = loopbackHost
	if host != "" {
		req.Host = host
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != status {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, status, rec.Body)
	}
	got := decode[apitypes.ErrorResponse](t, rec)
	if got.Code != code {
		t.Fatalf("code = %q, want %q", got.Code, code)
	}
	return got.Error
}
