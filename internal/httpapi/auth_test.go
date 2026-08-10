package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/secret"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// stubProvider は決められた利用者を返す IdentityProvider。
//
// identity を差し替えると別人としてログインできる。共有のテストで 2 人目が要る。
type stubProvider struct {
	exchangeErr error
	redirectURI string
	identity    *port.Identity
}

func (s *stubProvider) Name() string { return "github" }

func (s *stubProvider) AuthorizeURL(state, redirectURI string) string {
	return "https://github.test/login/oauth/authorize?state=" + state +
		"&redirect_uri=" + url.QueryEscape(redirectURI)
}

func (s *stubProvider) Exchange(
	_ context.Context, _, redirectURI string,
) (port.Identity, port.Credentials, error) {
	s.redirectURI = redirectURI
	if s.exchangeErr != nil {
		return port.Identity{}, port.Credentials{}, s.exchangeErr
	}
	if s.identity != nil {
		return *s.identity, port.Credentials{AccessToken: "ghu_1"}, nil
	}
	return port.Identity{
			Provider: "github", Subject: "42", Login: "octocat", DisplayName: "Octo Cat",
		},
		port.Credentials{AccessToken: "ghu_1"}, nil
}

func (s *stubProvider) Refresh(context.Context, string) (port.Credentials, error) {
	return port.Credentials{}, port.ErrNotAuthenticated
}

// newAuthRouter は認証つきのルーターを返す。
// provider が nil なら認証を設定しない構成になる。
func newAuthRouter(
	t *testing.T, provider port.IdentityProvider,
) (*gin.Engine, port.SessionRepository) {
	t.Helper()

	db := openTempDB(t)
	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	boards := sqlite.NewBoardRepository(db)
	mappings := sqlite.NewMappingRepository(db)

	deps := httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks()),
		Annotations: usecase.NewAnnotationService(boards, mappings),
	}

	var sessions port.SessionRepository
	if provider != nil {
		key := make([]byte, secret.KeySize)
		for i := range key {
			key[i] = byte(i)
		}
		box, err := secret.New(key)
		if err != nil {
			t.Fatalf("secret.New: %v", err)
		}

		sessions = sqlite.NewSessionRepository(db, box)
		auth := usecase.NewAuthService(provider, sessions)
		deps.Auth = auth
		// 招待は認証が無いと意味を持たない。組み立ての条件を etoki.New と
		// 揃えておかないと、ここだけ 503 にならない構成になる（ADR 0017）。
		deps.Members = usecase.NewBoardMemberService(boards, auth)
	}

	return httpapi.NewRouter(deps), sessions
}

// login は実際にログインを通し、セッション cookie を返す。
func signIn(t *testing.T, r *gin.Engine) *http.Cookie {
	t.Helper()

	rec := do(t, r, http.MethodPost, "/api/auth/login", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body)
	}

	authorizeURL := decode[map[string]any](t, rec)["authorizeUrl"].(string)
	u, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	state := u.Query().Get("state")

	rec = do(t, r, http.MethodGet, "/api/auth/callback?code=c1&state="+state, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("callback: %d %s", rec.Code, rec.Body)
	}

	for _, c := range rec.Result().Cookies() {
		if c.Name == "etoki_session" {
			return c
		}
	}
	t.Fatal("セッション cookie が返っていない")
	return nil
}

// withCookie は cookie を付けてリクエストする。
func withCookie(
	t *testing.T, r *gin.Engine, method, path string, cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, nil)
	req.Host = loopbackHost
	if cookie != nil {
		req.AddCookie(cookie)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// 認証を設定していない構成は、これまでどおり素通し（ADR 0015）。
func TestSession_WithoutAuthConfigured(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, nil)

	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/auth/session", nil))
	if got["authRequired"] != false {
		t.Errorf("authRequired = %v, want false", got["authRequired"])
	}

	// ログインを求めないので、API はそのまま使える。
	if rec := do(t, r, http.MethodGet, "/api/boards", nil); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// 未ログインでも 200。401 で伝えると、認証を設定していない構成の起動時にも
// 401 が出て、本当の失効と見分けがつかない。
func TestSession_UnauthenticatedIsStillOK(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	rec := do(t, r, http.MethodGet, "/api/auth/session", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	got := decode[map[string]any](t, rec)
	if got["authRequired"] != true || got["authenticated"] != false {
		t.Errorf("session = %+v", got)
	}
}

func TestBoards_RequireLoginWhenAuthConfigured(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	rec := do(t, r, http.MethodGet, "/api/boards", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
}

func TestLogin_ThenSessionReportsUser(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})
	cookie := signIn(t, r)

	got := decode[map[string]any](t, withCookie(t, r, http.MethodGet, "/api/auth/session", cookie))
	if got["authenticated"] != true {
		t.Fatalf("session = %+v", got)
	}

	user, _ := got["user"].(map[string]any)
	if user["login"] != "octocat" || user["displayName"] != "Octo Cat" {
		t.Errorf("user = %+v", user)
	}

	// ログインすれば API が通る。
	if rec := withCookie(t, r, http.MethodGet, "/api/boards", cookie); rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// cookie は JS から読めず、cross-site の POST では送られない形にする。
func TestLogin_CookieIsHardened(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})
	cookie := signIn(t, r)

	if !cookie.HttpOnly {
		t.Error("HttpOnly が付いていない")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	// 既定は http://127.0.0.1。常に Secure を付けると一部のブラウザで
	// 保存されず、原因の分からないログイン失敗になる。
	if cookie.Secure {
		t.Error("平文 HTTP なのに Secure が付いている")
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
}

// コールバックは副作用を持つ GET。守るのは Origin ではなく state（ADR 0015）。
func TestCallback_RejectsUnknownState(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	// ブラウザのトップレベル遷移に JSON を返すと生のエラー本文が見えるので、
	// 画面に戻す。戻ればログイン画面が出て、やり直しの導線に繋がる。
	rec := do(t, r, http.MethodGet, "/api/auth/callback?code=c1&state=forged", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "etoki_session" && c.Value != "" {
			t.Error("state が通っていないのにセッションを張っている")
		}
	}
}

func TestCallback_RejectsReusedState(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	rec := do(t, r, http.MethodPost, "/api/auth/login", nil)
	authorizeURL := decode[map[string]any](t, rec)["authorizeUrl"].(string)
	u, _ := url.Parse(authorizeURL)
	state := u.Query().Get("state")

	if rec = do(t, r, http.MethodGet, "/api/auth/callback?code=c1&state="+state, nil); rec.Code != http.StatusFound {
		t.Fatalf("1 回目: %d %s", rec.Code, rec.Body)
	}
	// 2 回目はセッションを張らずに画面へ戻す。
	rec = do(t, r, http.MethodGet, "/api/auth/callback?code=c1&state="+state, nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("2 回目: %d %s, want 302", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "etoki_session" && c.Value != "" {
			t.Error("使い回した state でセッションを張っている")
		}
	}
}

// 認可からの正しい遷移なら code と state は必ず付く。無いのは直接叩かれた場合。
func TestCallback_RejectsMissingParameters(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	for _, query := range []string{"", "?code=c1", "?state=s1"} {
		rec := do(t, r, http.MethodGet, "/api/auth/callback"+query, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("callback%q = %d, want 400", query, rec.Code)
		}
	}
}

// 引き換えそのものが失敗した場合も画面に戻す。
func TestCallback_RedirectsWhenExchangeFails(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{exchangeErr: port.ErrNotAuthenticated}
	r, _ := newAuthRouter(t, provider)

	rec := do(t, r, http.MethodPost, "/api/auth/login", nil)
	authorizeURL := decode[map[string]any](t, rec)["authorizeUrl"].(string)
	u, _ := url.Parse(authorizeURL)

	rec = do(t, r, http.MethodGet, "/api/auth/callback?code=c1&state="+u.Query().Get("state"), nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == "etoki_session" && c.Value != "" {
			t.Error("引き換えに失敗したのにセッションを張っている")
		}
	}
}

// state の発行は書き込み。GET にすると外部ページの <img> から叩ける。
func TestLogin_IsNotReachableByGet(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	rec := do(t, r, http.MethodGet, "/api/auth/login", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404（GET は生やさない）", rec.Code)
	}
}

// redirect_uri はリクエストの Host から組む。make dev では Vite が Host を
// 書き換えずに転送するので、これで :5173 になる。
func TestLogin_BuildsRedirectURIFromHost(t *testing.T) {
	t.Parallel()

	provider := &stubProvider{}
	r, _ := newAuthRouter(t, provider)

	rec := do(t, r, http.MethodPost, "/api/auth/login", nil)
	authorizeURL := decode[map[string]any](t, rec)["authorizeUrl"].(string)

	want := "http://" + loopbackHost + "/api/auth/callback"
	if !strings.Contains(authorizeURL, url.QueryEscape(want)) {
		t.Errorf("authorizeUrl に %q が含まれない: %s", want, authorizeURL)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})
	cookie := signIn(t, r)

	rec := withCookie(t, r, http.MethodPost, "/api/auth/logout", cookie)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body)
	}

	// 破棄されたので、同じ cookie ではもう通らない。
	if rec = withCookie(t, r, http.MethodGet, "/api/boards", cookie); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestLogout_WithoutSessionIsNoContent(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	if rec := do(t, r, http.MethodPost, "/api/auth/logout", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

// 失効したセッションは未ログインとして扱う。
func TestExpiredSessionIsRejected(t *testing.T) {
	t.Parallel()

	r, sessions := newAuthRouter(t, &stubProvider{})
	cookie := signIn(t, r)

	// 有効期間を跨いだ時刻から見ると解決できない。
	future := time.Now().Add(usecase.SessionTTL + time.Hour)
	expired := usecase.NewAuthService(&stubProvider{}, sessions,
		usecase.WithAuthClock(func() time.Time { return future }))

	user, err := expired.Resolve(t.Context(), cookie.Value)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if user != nil {
		t.Errorf("失効したセッションが解決できている: %+v", user)
	}
}

// ---------------------------------------------------------------------------
// 所有者による分離（ADR 0016）
// ---------------------------------------------------------------------------

// もう 1 人分のセッションを作る。stubProvider は毎回同じ利用者を返すので、
// 別の subject を返すものを差し替えて使う。
type otherUserProvider struct{ stubProvider }

func (p *otherUserProvider) Exchange(
	context.Context, string, string,
) (port.Identity, port.Credentials, error) {
	return port.Identity{
			Provider: "github", Subject: "99", Login: "hubot", DisplayName: "Hubot",
		},
		port.Credentials{AccessToken: "ghu_2"}, nil
}

// ログインした A が、B のボード ID を知っていても触れないこと。
// 認証だけ入れて認可を入れないのは、鍵をかけずに受付を置いたのと同じ。
func TestBoards_AreIsolatedBetweenUsers(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	key := make([]byte, secret.KeySize)
	box, err := secret.New(key)
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}

	boards := sqlite.NewBoardRepository(db)
	mappings := sqlite.NewMappingRepository(db)
	sessions := sqlite.NewSessionRepository(db, box)

	// 同じ DB に向いた 2 つのルーター。利用者だけが違う。
	newFor := func(provider port.IdentityProvider) *gin.Engine {
		return httpapi.NewRouter(httpapi.Deps{
			Boards:      usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks()),
			Annotations: usecase.NewAnnotationService(boards, mappings),
			Auth:        usecase.NewAuthService(provider, sessions),
		})
	}

	alice, bob := newFor(&stubProvider{}), newFor(&otherUserProvider{})
	aliceCookie, bobCookie := signIn(t, alice), signIn(t, bob)

	rec := doWithCookie(t, alice, http.MethodPost, "/api/boards",
		map[string]string{"name": "アリスのボード"}, aliceCookie)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	boardID, _ := decode[map[string]any](t, rec)["id"].(string)

	// 一覧に出ない。
	list := decode[[]map[string]any](t,
		withCookie(t, bob, http.MethodGet, "/api/boards", bobCookie))
	if len(list) != 0 {
		t.Errorf("他人のボードが一覧に出ている: %+v", list)
	}

	// ID を知っていても 404。403 にしないのは、ID の総当たりで存在を
	// 確かめられるようにしないため。
	if rec = withCookie(t, bob, http.MethodGet, "/api/boards/"+boardID, bobCookie); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/boards/{id} = %d, want 404", rec.Code)
	}

	// 書き換えもできない。ここは Find を通らない経路。
	rec = doWithCookie(t, bob, http.MethodPut, "/api/boards/"+boardID+"/scene",
		map[string]string{"scene": `{"type":"excalidraw","elements":[],"appState":{}}`}, bobCookie)
	if rec.Code != http.StatusNotFound {
		t.Errorf("PUT scene = %d, want 404 (%s)", rec.Code, rec.Body)
	}

	// 作成先も設定できない。
	rec = doWithCookie(t, bob, http.MethodPut, "/api/boards/"+boardID+"/target",
		map[string]string{"repositoryOwner": "evil", "repositoryName": "r", "projectId": "PVT_x"},
		bobCookie)
	if rec.Code != http.StatusNotFound {
		t.Errorf("PUT target = %d, want 404 (%s)", rec.Code, rec.Body)
	}

	// 注釈の状態も見えない。
	if rec = withCookie(t, bob, http.MethodGet, "/api/boards/"+boardID+"/annotations", bobCookie); rec.Code != http.StatusNotFound {
		t.Errorf("GET annotations = %d, want 404", rec.Code)
	}

	// 本人には見える。分離できていることと、壊れていることを区別する。
	if rec = withCookie(t, alice, http.MethodGet, "/api/boards/"+boardID, aliceCookie); rec.Code != http.StatusOK {
		t.Errorf("所有者本人が見られない: %d (%s)", rec.Code, rec.Body)
	}
}

// doWithCookie は本文つきで cookie を送る。
func doWithCookie(
	t *testing.T, r *gin.Engine, method, path string, body any, cookie *http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Host = loopbackHost
	req.AddCookie(cookie)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
