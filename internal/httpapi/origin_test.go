package httpapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// newGuardedRouter は許可オリジンを指定してルーターを作る。
func newGuardedRouter(t *testing.T, allowed ...string) *gin.Engine {
	t.Helper()

	boards, mappings := newRepos(t)
	seq := 0

	return httpapi.NewRouter(httpapi.Deps{
		Boards: usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks(),
			usecase.WithClock(func() time.Time { return fixedTime }),
			usecase.WithIDGenerator(func() string {
				seq++
				return "board-" + string(rune('0'+seq))
			}),
		),
		Annotations:    usecase.NewAnnotationService(boards, mappings),
		AllowedOrigins: allowed,
	})
}

// request は Host と任意のヘッダを指定してリクエストを投げる。
func request(t *testing.T, r *gin.Engine, method, path, host string, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, reader)
	if host != "" {
		req.Host = host
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

const loopbackHost = "127.0.0.1:8080"

// countBoards は副作用が起きていないことを確かめるために使う。
func countBoards(t *testing.T, r *gin.Engine) int {
	t.Helper()

	rec := request(t, r, http.MethodGet, "/api/boards", loopbackHost, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("一覧の取得に失敗した: status = %d", rec.Code)
	}
	return len(decode[[]map[string]any](t, rec))
}

// Origin を送らないクライアント（curl / スクリプト）は従来どおり通す。
// ブラウザは cross-origin の POST に必ず Origin を付けるので、ここを塞ぐと
// 守れるものが増えないまま CLI からの利用だけが壊れる。
func TestOriginGuard_AllowsRequestWithoutOrigin(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t)
	rec := request(t, r, http.MethodPost, "/api/boards", loopbackHost,
		map[string]string{"Content-Type": "application/json"}, `{"name":"cli"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body)
	}
}

// ループバック由来ならポートは問わない。make dev では Vite の dev サーバー
// (:5173) が Host と Origin をそのまま転送するため、リッスンポートだけに
// 絞ると開発時に落ちる。
func TestOriginGuard_AllowsLoopbackOrigins(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ host, origin string }{
		"同一オリジン":          {loopbackHost, "http://127.0.0.1:8080"},
		"Vite の dev サーバー": {"localhost:5173", "http://localhost:5173"},
		"IPv6 ループバック":     {"[::1]:8080", "http://[::1]:8080"},
		"127.0.0.0/8":     {"127.0.0.2:8080", "http://127.0.0.2:8080"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newGuardedRouter(t)
			rec := request(t, r, http.MethodPost, "/api/boards", tc.host,
				map[string]string{"Content-Type": "application/json", "Origin": tc.origin},
				`{"name":"local"}`)

			if rec.Code != http.StatusCreated {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body)
			}
		})
	}
}

// 外部サイトのページから叩かれた場合。副作用が起きていないことまで見る。
func TestOriginGuard_RejectsCrossSiteOrigin(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t)
	before := countBoards(t, r)

	rec := request(t, r, http.MethodPost, "/api/boards", loopbackHost,
		map[string]string{"Content-Type": "application/json", "Origin": "https://evil.example"},
		`{"name":"attacker"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusForbidden, rec.Body)
	}
	if after := countBoards(t, r); after != before {
		t.Errorf("ボードが %d 件から %d 件に増えている。弾く前に処理が走っている", before, after)
	}
}

// text/plain はプリフライトの起きない simple request なので、CORS ヘッダを
// 返さないだけでは止まらない。Gin の ShouldBindJSON は Content-Type を見ずに
// 本文を JSON として読むため、この形が実際に通ってしまっていた。
func TestOriginGuard_RejectsCrossSiteSimpleRequest(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t)
	before := countBoards(t, r)

	rec := request(t, r, http.MethodPost, "/api/boards", loopbackHost,
		map[string]string{"Content-Type": "text/plain;charset=UTF-8", "Origin": "https://evil.example"},
		`{"name":"attacker"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusForbidden, rec.Body)
	}
	if after := countBoards(t, r); after != before {
		t.Errorf("ボードが %d 件から %d 件に増えている", before, after)
	}
}

// sandbox された iframe や file:// からは Origin が "null" になる。
// ループバックではないので弾く。
func TestOriginGuard_RejectsNullOrigin(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t)
	rec := request(t, r, http.MethodPost, "/api/boards", loopbackHost,
		map[string]string{"Content-Type": "application/json", "Origin": "null"},
		`{"name":"sandboxed"}`)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// DNS リバインディング。攻撃者のドメインが 127.0.0.1 を指すと、ページと
// etoki が同一オリジンになり CORS の制約がかからない。Origin では止まらない
// ので Host を見る。読み取りが目的なので GET でも塞ぐ必要がある。
func TestOriginGuard_RejectsNonLoopbackHost(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"GET":  http.MethodGet,
		"POST": http.MethodPost,
	}

	for name, method := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newGuardedRouter(t)
			rec := request(t, r, method, "/api/boards", "evil.example:8080", nil, "")

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want %d (body: %s)", rec.Code, http.StatusForbidden, rec.Body)
			}
		})
	}
}

// 弾いたときの本文も契約どおりの形にする。gin.H を直に書くと、契約に無いキーが
// 混ざっても気づけない（ADR 0011）。
func TestOriginGuard_RejectBodyMatchesContract(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t)
	rec := request(t, r, http.MethodGet, "/api/boards", "evil.example:8080", nil, "")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}

	body := decode[map[string]any](t, rec)
	if len(body) != 1 {
		t.Errorf("ErrorResponse に無いキーが混ざっている: %v", body)
	}
	if msg, _ := body["error"].(string); msg == "" {
		t.Errorf("error = %v, want 非空の文字列", body["error"])
	}
}

// ループバック以外にバインドする場合は、利用者が明示的に許可を足す。
// ETOKI_ADDR を上書きできる以上、これが無いと自分で締め出される。
func TestOriginGuard_AllowsConfiguredOrigin(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t, "http://192.168.1.5:8080")
	rec := request(t, r, http.MethodPost, "/api/boards", "192.168.1.5:8080",
		map[string]string{"Content-Type": "application/json", "Origin": "http://192.168.1.5:8080"},
		`{"name":"lan"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, http.StatusCreated, rec.Body)
	}
}

// 許可を足しても、そこに書いていないオリジンは通さない。
func TestOriginGuard_ConfiguredOriginDoesNotOpenOthers(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t, "http://192.168.1.5:8080")
	rec := request(t, r, http.MethodPost, "/api/boards", loopbackHost,
		map[string]string{"Content-Type": "application/json", "Origin": "http://192.168.1.6:8080"},
		`{"name":"other"}`)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// /healthz も同じ扱いにする。守る情報は無いが、経路ごとに規則が変わると
// 「ここは素通し」が増えていく。
func TestOriginGuard_AppliesToHealthz(t *testing.T) {
	t.Parallel()

	r := newGuardedRouter(t)

	if rec := request(t, r, http.MethodGet, "/healthz", loopbackHost, nil, ""); rec.Code != http.StatusOK {
		t.Errorf("ループバック: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec := request(t, r, http.MethodGet, "/healthz", "evil.example:8080", nil, ""); rec.Code != http.StatusForbidden {
		t.Errorf("外部ホスト: status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
