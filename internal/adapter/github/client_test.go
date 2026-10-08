package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/github"
	"github.com/yusuke0610/etoki/port"
)

const testToken = "ghp_secret_must_not_leak"

func ptr[T any](v T) *T { return &v }

// capturedRequest は 1 回分の GraphQL リクエスト。
type capturedRequest struct {
	Header    http.Header
	Path      string
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// newClient は決められた応答を順に返すサーバーに向いたクライアントを返す。
// 応答を使い切ったら最後のものを返し続ける。
func newClient(t *testing.T, responses ...string) (*github.Client, *[]capturedRequest) {
	t.Helper()

	var got []capturedRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		req := capturedRequest{Header: r.Header.Clone(), Path: r.URL.Path}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("リクエストが JSON ではない: %v", err)
		}
		got = append(got, req)

		i := min(len(got)-1, len(responses)-1)
		_, _ = io.WriteString(w, responses[i])
	}))
	t.Cleanup(srv.Close)

	c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	return c, &got
}

func TestNew_RequiresToken(t *testing.T) {
	t.Parallel()

	_, err := github.New(github.Config{})
	if !errors.Is(err, github.ErrTokenRequired) {
		t.Fatalf("New() = %v, want ErrTokenRequired", err)
	}
}

// ctx を切ったら呼び出しを打ち切る。長い処理を止められないと困る。
func TestHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = c.ListProjectFields(ctx, "PVT_1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListProjectFields() = %v, want context.Canceled", err)
	}
}

// GraphQL は HTTP 200 でもボディに errors を返す。ステータスコードだけを見て
// 成功と判断すると、何も作られていないのに成功として進んでしまう。
func TestGraphQLErrorsOnHTTP200(t *testing.T) {
	t.Parallel()

	body := `{"data":null,"errors":[
		{"type":"NOT_FOUND","message":"Could not resolve to a node with the global id of 'PVT_x'"},
		{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`

	c, _ := newClient(t, body)

	_, err := c.CreateDraftIssue(t.Context(), "PVT_x", port.DraftIssue{Title: "t"})
	if err == nil {
		t.Fatal("CreateDraftIssue() = nil, want error")
	}
	for _, want := range []string{"NOT_FOUND", "Could not resolve", "FORBIDDEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラーに %q が含まれない: %v", want, err)
		}
	}
}

func TestHTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		want    []string
	}{
		{
			name:   "401 認証エラー",
			status: http.StatusUnauthorized,
			body:   `{"message":"Bad credentials"}`,
			want:   []string{"401", "Bad credentials"},
		},
		{
			name:    "403 レート制限",
			status:  http.StatusForbidden,
			headers: map[string]string{"x-ratelimit-remaining": "0", "x-ratelimit-reset": "1780000000"},
			body:    `{"message":"API rate limit exceeded"}`,
			want:    []string{"403", "rate limit resets at", "1780000000"},
		},
		{
			// 二次レート制限。残数は残ったまま retry-after だけが付く。
			name:    "403 二次レート制限",
			status:  http.StatusForbidden,
			headers: map[string]string{"x-ratelimit-remaining": "4999", "retry-after": "60"},
			body:    `{"message":"You have exceeded a secondary rate limit"}`,
			want:    []string{"403", "retry-after: 60"},
		},
		{
			// レート制限ではない 403。招待されただけでリポジトリに権限が無い
			// 利用者はここに来る（ADR 0017）。
			name:   "403 権限不足",
			status: http.StatusForbidden,
			body:   `{"message":"Resource not accessible by integration"}`,
			want:   []string{"403", "Resource not accessible"},
		},
		{
			name:   "502 JSON ではない応答",
			status: http.StatusBadGateway,
			body:   `<html>502 Bad Gateway</html>`,
			want:   []string{"502", "Bad Gateway"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)

			c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}

			_, err = c.ListProjectFields(t.Context(), "PVT_1")
			if err == nil {
				t.Fatal("ListProjectFields() = nil, want error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("エラーに %q が含まれない: %v", want, err)
				}
			}

			// 401 だけは sentinel に寄せる。文字列で判定させると、UI が
			// 「再ログインが要る」を見分けられない（ADR 0015）。
			if got := errors.Is(err, port.ErrNotAuthenticated); got != (tt.status == http.StatusUnauthorized) {
				t.Errorf("errors.Is(err, ErrNotAuthenticated) = %v (status %d): %v",
					got, tt.status, err)
			}

			// 403 も sentinel に寄せるが、レート制限は含めない。待てば通るものを
			// 「権限がありません」と見せない（ADR 0017）。
			wantForbidden := tt.status == http.StatusForbidden &&
				tt.headers["x-ratelimit-remaining"] != "0" &&
				tt.headers["retry-after"] == ""
			if got := errors.Is(err, port.ErrForbidden); got != wantForbidden {
				t.Errorf("errors.Is(err, ErrForbidden) = %v, want %v: %v",
					got, wantForbidden, err)
			}
		})
	}
}

// トークンがエラーに混ざると、ログや画面に出た時点で漏れる。
func TestDoesNotLeakToken(t *testing.T) {
	t.Parallel()

	tests := map[string]http.HandlerFunc{
		"認証エラー": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
		},
		"GraphQL エラー": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"data":null,"errors":[{"message":"nope"}]}`)
		},
		"壊れた応答": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `not json`)
		},
	}

	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(handler)
			t.Cleanup(srv.Close)

			c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}

			_, err = c.CreateDraftIssue(t.Context(), "PVT_1", port.DraftIssue{Title: "t"})
			if err == nil {
				t.Fatal("CreateDraftIssue() = nil, want error")
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("エラーにトークンが含まれている: %v", err)
			}
		})
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("ETOKI_GITHUB_TOKEN", "ghp_from_env")
	t.Setenv("ETOKI_GITHUB_BASE_URL", "http://127.0.0.1:8090")

	got := github.ConfigFromEnv()
	if got.Token != "ghp_from_env" {
		t.Errorf("Token = %q", got.Token)
	}
	if got.BaseURL != "http://127.0.0.1:8090" {
		t.Errorf("BaseURL = %q", got.BaseURL)
	}
}

// 綴り間違いを実行時まで持ち越すと、作成先の一覧や作成の失敗として現れて
// 切り分けが遠回りになる。
func TestNew_RejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"スキームが無い":   "api.github.com",
		"対応しないスキーム": "ftp://example.test",
		"壊れた URL":   "http://[::1",
		// スキームは通るがホストが無い。呼び出し時まで失敗が遅れる。
		"ホストが無い":     "http://",
		"スラッシュが足りない": "https:api.github.com",
		// 本物のトークンを平文で外に送る。http で届く GitHub は無い。
		"ループバックの外への http": "http://api.github.com",
		// 送り先は base に "/graphql" を足した文字列なので、クエリや
		// fragment が付いていると足したぶんが path に入らない。
		"クエリつき":       "https://api.github.com?x=1",
		"空のクエリ":       "https://api.github.com?",
		"fragment つき": "https://api.github.com#x",
		"空の fragment": "https://api.github.com#",
		"パスのあとのクエリ":   "https://api.github.com/api/v3?x=1",
	}

	for name, base := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := github.New(github.Config{BaseURL: base, Token: testToken}); err == nil {
				t.Fatalf("New(%q) = nil, want error", base)
			}
		})
	}
}

// https は向け先を問わず、http はループバックだけを通す（ADR 0050）。この口を
// 開けたのは手元の偽物に向けるためなので、偽物に届く形は残す。
func TestNew_AcceptsBaseURL(t *testing.T) {
	t.Parallel()

	for _, base := range []string{
		"https://api.github.com",
		"https://github.example.test",
		"http://127.0.0.1:8090",
		"http://localhost:8090",
		"http://[::1]:8090",
	} {
		t.Run(base, func(t *testing.T) {
			t.Parallel()

			if _, err := github.New(github.Config{BaseURL: base, Token: testToken}); err != nil {
				t.Fatalf("New(%q) = %v, want nil", base, err)
			}
		})
	}
}

// 末尾の / を落とさないと //graphql に送り、向け先によっては 404 になる。
func TestNew_TrimsTrailingSlashOfBaseURL(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"data":{"node":{"viewerCanUpdate":true}}}`)
	}))
	t.Cleanup(srv.Close)

	c, err := github.New(github.Config{BaseURL: srv.URL + "/", Token: testToken})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := c.CanWriteProject(context.Background(), "PVT_1"); err != nil {
		t.Fatalf("CanWriteProject() = %v", err)
	}
	if gotPath != "/graphql" {
		t.Errorf("path = %q, want /graphql", gotPath)
	}
}

// Client が port.GitHubClient を満たすことを固定する。
var _ port.GitHubClient = (*github.Client)(nil)

// ---------------------------------------------------------------------------
// 作成先の候補一覧（ADR 0014）
// ---------------------------------------------------------------------------

// GraphQL は権限で拒むとき HTTP 200 のボディに errors を載せる。ステータスだけを
// 見ていると、権限の問題が 500 に落ちる（ADR 0017）。
func TestGraphQLForbiddenIsSentinel(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t,
		`{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible"}]}`)

	_, err := c.CanWriteProject(t.Context(), "PVT_1")
	if !errors.Is(err, port.ErrForbidden) {
		t.Fatalf("CanWriteProject() = %v, want port.ErrForbidden", err)
	}
}
