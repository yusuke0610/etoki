package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// MCP のクライアントのための認可サーバー（ADR 0076）の HTTP 面。発行の規則は
// usecase のテストが持ち、ここでは口の形と、`/mcp` に効いていることを見る。

const (
	// testVerifier と testChallenge は RFC 7636 付録 B の組。
	testVerifier    = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	testChallenge   = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	testRedirectURI = "http://127.0.0.1:33418/callback"
	testResource    = "http://" + loopbackHost + "/mcp"
)

type oauthRouterOptions struct {
	publicURL    string
	metadataDocs bool
}

// newOAuthRouter は認証と認可サーバーを組み立てたルーターを返す。
func newOAuthRouter(t *testing.T, opts oauthRouterOptions) (*gin.Engine, *stubProvider, *sql.DB) {
	t.Helper()

	provider := &stubProvider{}
	deps, _, db := newAuthDeps(t, provider)
	deps.OAuth = usecaseOAuthServer(sqlite.NewOAuthGrantRepository(db))
	deps.OAuthMetadataDocuments = opts.metadataDocs
	deps.PublicURL = opts.publicURL
	return httpapi.NewRouter(deps), provider, db
}

// formPost は form の本文で POST する。MCP のクライアントと同じく Origin を
// 付けない。
func formPost(t *testing.T, r *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, r, http.MethodPost, path, loopbackHost,
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, form.Encode())
}

// registerClient は動的登録を通して client_id を返す。
func registerClient(t *testing.T, r *gin.Engine) string {
	t.Helper()

	rec := request(t, r, http.MethodPost, "/oauth/register", loopbackHost,
		map[string]string{"Content-Type": "application/json"},
		`{"client_name":"Claude Code","redirect_uris":["http://127.0.0.1/callback"],`+
			`"token_endpoint_auth_method":"none"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("register の Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode register: %v", err)
	}
	id, _ := out["client_id"].(string)
	if id == "" {
		t.Fatalf("client_id が無い: %s", rec.Body)
	}
	return id
}

func authorizeQuery(clientID string) string {
	return url.Values{
		"response_type":         {"code"},
		"client_id":             {clientID},
		"redirect_uri":          {testRedirectURI},
		"code_challenge":        {testChallenge},
		"code_challenge_method": {"S256"},
		"scope":                 {"read"},
		"state":                 {"st-1"},
		"resource":              {testResource},
	}.Encode()
}

// consentRequest は `/oauth/authorize` を叩き、画面に転送された要求を返す。
func consentRequest(t *testing.T, r *gin.Engine, clientID string) string {
	t.Helper()

	rec := request(t, r, http.MethodGet, "/oauth/authorize?"+authorizeQuery(clientID), loopbackHost, nil, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if loc.Path != "/" || loc.Host != "" {
		t.Fatalf("転送先 = %s, want 自オリジンの /", loc)
	}
	return loc.Query().Get("authorize")
}

// approveConsent は画面と同じ順に同意し、コードを返す。
func approveConsent(t *testing.T, r *gin.Engine, cookie *http.Cookie, request string) string {
	t.Helper()

	rec := withCookie(t, r, http.MethodGet,
		"/api/oauth/authorization?request="+url.QueryEscape(request), cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("getOAuthAuthorization: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(t, r, http.MethodPost, "/api/oauth/authorization", cookie,
		apitypes.OAuthDecisionRequest{Request: request, Approve: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("decide: %d %s", rec.Code, rec.Body)
	}
	target, err := url.Parse(decode[apitypes.OAuthDecision](t, rec).RedirectTo)
	if err != nil {
		t.Fatalf("parse redirectTo: %v", err)
	}
	if got := target.Scheme + "://" + target.Host + target.Path; got != testRedirectURI {
		t.Fatalf("戻り先 = %s, want %s", got, testRedirectURI)
	}
	if target.Query().Get("state") != "st-1" || target.Query().Get("iss") != "http://"+loopbackHost {
		t.Errorf("戻り先のクエリ = %v", target.Query())
	}
	return target.Query().Get("code")
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// connectClient は登録から引き換えまでを通し、トークンを返す。
func connectClient(t *testing.T, r *gin.Engine, cookie *http.Cookie) (string, tokenPair) {
	t.Helper()

	clientID := registerClient(t, r)
	code := approveConsent(t, r, cookie, consentRequest(t, r, clientID))

	rec := formPost(t, r, "/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {testRedirectURI},
		"client_id":     {clientID},
		"code_verifier": {testVerifier},
		"resource":      {testResource},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("token の Cache-Control = %q", rec.Header().Get("Cache-Control"))
	}
	var tokens tokenPair
	if err := json.Unmarshal(rec.Body.Bytes(), &tokens); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if tokens.TokenType != "Bearer" || tokens.ExpiresIn != 3600 || tokens.Scope != "read" ||
		tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("token = %+v", tokens)
	}
	return clientID, tokens
}

// mcpListTools は `/mcp` に tools/list を送る。
func mcpListTools(t *testing.T, r *gin.Engine, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	h := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json, text/event-stream",
	}
	for k, v := range headers {
		h[k] = v
	}
	return request(t, r, http.MethodPost, "/mcp", loopbackHost, h,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
}

// bearerTransport は MCP のクライアントの要求にトークンを付ける。
type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

// connectMCPWithToken は実際の HTTP サーバーに載せ、トークンを付けて繋ぐ。
func connectMCPWithToken(t *testing.T, r *gin.Engine, token string) *mcp.ClientSession {
	t.Helper()

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "etoki-test", Version: "0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{token: token}},
	}, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// 13. 認証ありの構成で認可サーバーが無ければ、`/mcp` も OAuth の口も開かない。
func TestOAuth_UnavailableWithoutGrantRepository(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})
	cookie := signIn(t, r)

	for _, path := range []string{
		"/.well-known/oauth-protected-resource/mcp",
		"/.well-known/oauth-authorization-server",
		"/oauth/authorize?client_id=x",
	} {
		rec := request(t, r, http.MethodGet, path, loopbackHost, nil, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s: status = %d, want 503", path, rec.Code)
		}
	}
	for _, path := range []string{"/oauth/token", "/oauth/register"} {
		rec := formPost(t, r, path, url.Values{})
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s: status = %d, want 503", path, rec.Code)
		}
	}

	rec := withCookie(t, r, http.MethodGet, "/api/oauth/grants", cookie)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("grants: status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeMcpConnectionsNotConfigured {
		t.Errorf("code = %q", code)
	}

	caps := decodeOK[apitypes.Capabilities](t, withCookie(t, r, http.MethodGet, "/api/capabilities", cookie))
	if caps.McpConnections {
		t.Error("capabilities.mcpConnections = true, want false")
	}
}

// 認証なしの構成では、認可サーバーを渡されても使わない。`/mcp` は許可なしで
// 開いている（ADR 0071）。
func TestOAuth_IgnoredWithoutAuth(t *testing.T) {
	t.Parallel()

	deps, _, db := newAuthDeps(t, nil)
	deps.OAuth = usecaseOAuthServer(sqlite.NewOAuthGrantRepository(db))
	r := httpapi.NewRouter(deps)

	if rec := mcpListTools(t, r, nil); rec.Code != http.StatusOK {
		t.Errorf("/mcp: status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if rec := request(t, r, http.MethodGet, "/.well-known/oauth-authorization-server",
		loopbackHost, nil, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("metadata: status = %d, want 503", rec.Code)
	}
	caps := decodeOK[apitypes.Capabilities](t, do(t, r, http.MethodGet, "/api/capabilities", nil))
	if caps.McpConnections {
		t.Error("capabilities.mcpConnections = true, want false")
	}
}

// 14. トークンが無い・無効なら 401 と、メタデータの在りかを返す。画面の
// セッション cookie では通さない。
func TestMCP_RequiresBearer(t *testing.T) {
	t.Parallel()

	r, _, _ := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	wantMetadata := `resource_metadata="http://` + loopbackHost + `/.well-known/oauth-protected-resource/mcp"`

	cases := map[string]map[string]string{
		"無し":        nil,
		"無効":        {"Authorization": "Bearer nope"},
		"Basic":     {"Authorization": "Basic Zm9vOmJhcg=="},
		"cookie だけ": {"Cookie": cookie.Name + "=" + cookie.Value},
	}
	for name, headers := range cases {
		rec := mcpListTools(t, r, headers)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401 (%s)", name, rec.Code, rec.Body)
			continue
		}
		challenge := rec.Header().Get("WWW-Authenticate")
		if !strings.HasPrefix(challenge, "Bearer ") || !strings.Contains(challenge, wantMetadata) {
			t.Errorf("%s: WWW-Authenticate = %q", name, challenge)
		}
		if strings.Contains(rec.Body.String(), "list_boards") {
			t.Errorf("%s: 弾いたのに道具の一覧が出ている", name)
		}
	}
	if got := mcpListTools(t, r, map[string]string{"Authorization": "Bearer nope"}).
		Header().Get("WWW-Authenticate"); !strings.Contains(got, `error="invalid_token"`) {
		t.Errorf("無効なトークンの WWW-Authenticate = %q", got)
	}
}

// 15. トークンが通れば、見えるのは自分がメンバーのボードだけ。他人のボードは
// not_found。
func TestMCP_BearerSeesOnlyOwnBoards(t *testing.T) {
	t.Parallel()

	// 実際の HTTP サーバーは別のポートで待つ。トークンは発行したときの
	// `/mcp` の URL に結びつく（RFC 8707）ので、起点を固定して揃える。
	r, provider, _ := newOAuthRouter(t, oauthRouterOptions{publicURL: "http://" + loopbackHost})
	alice := signInAs(t, r, provider, "1", "alice")
	bob := signInAs(t, r, provider, "2", "bob")
	mine := createSharedBoard(t, r, alice, "alice のボード")
	theirs := createSharedBoard(t, r, bob, "bob のボード")

	_, tokens := connectClient(t, r, alice)
	session := connectMCPWithToken(t, r, tokens.AccessToken)

	out := callTool(t, session, "list_boards", map[string]any{})
	boards, _ := out["boards"].([]any)
	if len(boards) != 1 {
		t.Fatalf("boards = %v, want alice の 1 枚だけ", boards)
	}
	if id, _ := boards[0].(map[string]any)["id"].(string); id != mine {
		t.Errorf("board id = %q, want %q", id, mine)
	}

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "list_annotations", Arguments: map[string]any{"boardId": theirs},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError || !strings.HasPrefix(toolText(res), "not_found") {
		t.Errorf("他人のボード = isError %v, %q; want not_found", res.IsError, toolText(res))
	}
}

// 16. `/api` は Bearer を受けない。MCP のトークンだけでは 401。
func TestAPI_DoesNotAcceptBearer(t *testing.T) {
	t.Parallel()

	r, _, _ := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	_, tokens := connectClient(t, r, cookie)

	rec := request(t, r, http.MethodGet, "/api/boards", loopbackHost,
		map[string]string{"Authorization": "Bearer " + tokens.AccessToken}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
}

// 17. メタデータの URL は ETOKI_PUBLIC_URL があればそれ、無ければ Host から組む。
func TestOAuth_Metadata(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		opts oauthRouterOptions
		base string
	}{
		"Host から":      {oauthRouterOptions{}, "http://" + loopbackHost},
		"PublicURL から": {oauthRouterOptions{publicURL: "https://etoki.example/"}, "https://etoki.example"},
		"CIMD を受ける構成":  {oauthRouterOptions{metadataDocs: true}, "http://" + loopbackHost},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, _, _ := newOAuthRouter(t, tc.opts)

			for _, path := range []string{
				"/.well-known/oauth-protected-resource/mcp",
				"/.well-known/oauth-protected-resource",
			} {
				rec := request(t, r, http.MethodGet, path, loopbackHost, nil, "")
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body)
				}
				var prm struct {
					Resource             string   `json:"resource"`
					AuthorizationServers []string `json:"authorization_servers"`
					ScopesSupported      []string `json:"scopes_supported"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &prm); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if prm.Resource != tc.base+"/mcp" || len(prm.AuthorizationServers) != 1 ||
					prm.AuthorizationServers[0] != tc.base || len(prm.ScopesSupported) != 1 ||
					prm.ScopesSupported[0] != "read" {
					t.Errorf("%s = %+v", path, prm)
				}
			}

			rec := request(t, r, http.MethodGet, "/.well-known/oauth-authorization-server", loopbackHost, nil, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("AS metadata: %d %s", rec.Code, rec.Body)
			}
			var as map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &as); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for key, want := range map[string]any{
				"issuer":                                         tc.base,
				"authorization_endpoint":                         tc.base + "/oauth/authorize",
				"token_endpoint":                                 tc.base + "/oauth/token",
				"registration_endpoint":                          tc.base + "/oauth/register",
				"client_id_metadata_document_supported":          tc.opts.metadataDocs,
				"authorization_response_iss_parameter_supported": true,
			} {
				if as[key] != want {
					t.Errorf("%s = %v, want %v", key, as[key], want)
				}
			}
			if methods, _ := as["code_challenge_methods_supported"].([]any); len(methods) != 1 || methods[0] != "S256" {
				t.Errorf("code_challenge_methods_supported = %v", as["code_challenge_methods_supported"])
			}
			if methods, _ := as["token_endpoint_auth_methods_supported"].([]any); len(methods) != 1 || methods[0] != "none" {
				t.Errorf("token_endpoint_auth_methods_supported = %v", as["token_endpoint_auth_methods_supported"])
			}
		})
	}
}

// `/oauth/authorize` は副作用を持たない。検証もせず、何も書かずに画面へ転送する
// （副作用を持つ GET を増やさない。ADR 0013）。
func TestAuthorize_OnlyForwardsToConsent(t *testing.T) {
	t.Parallel()

	r, _, db := newOAuthRouter(t, oauthRouterOptions{})
	raw := "client_id=unknown&state=a%26b&redirect_uri=https%3A%2F%2Fevil.example%2F"

	rec := request(t, r, http.MethodGet, "/oauth/authorize?"+raw, loopbackHost, nil, "")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (%s)", rec.Code, rec.Body)
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if loc.Path != "/" || loc.Host != "" || loc.Query().Get("authorize") != raw {
		t.Errorf("Location = %s, want /?authorize=<そのまま>", loc)
	}
	for _, table := range []string{"oauth_clients", "oauth_codes", "oauth_grants", "oauth_tokens"} {
		var n int
		if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s に %d 行書かれた", table, n)
		}
	}
}

// 同意の画面の口は要求の誤りを 400 で返す。クライアントには戻さない。
func TestOAuthAuthorization_RejectsInvalidRequest(t *testing.T) {
	t.Parallel()

	r, _, _ := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	clientID := registerClient(t, r)

	bad := strings.Replace(authorizeQuery(clientID), "S256", "plain", 1)
	rec := withCookie(t, r, http.MethodGet, "/api/oauth/authorization?request="+url.QueryEscape(bad), cookie)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeInvalidInput {
		t.Errorf("code = %q", code)
	}

	// 同じ名前を 2 つ付けた要求も断る。画面に見せた値と使う値が食い違いうる。
	dup := authorizeQuery(clientID) + "&redirect_uri=" + url.QueryEscape("http://127.0.0.1:1/callback")
	rec = withCookie(t, r, http.MethodGet, "/api/oauth/authorization?request="+url.QueryEscape(dup), cookie)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("重複: status = %d, want 400 (%s)", rec.Code, rec.Body)
	}

	// 同意の画面の口はログインが要る。
	rec = request(t, r, http.MethodGet,
		"/api/oauth/authorization?request="+url.QueryEscape(authorizeQuery(clientID)), loopbackHost, nil, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("未ログイン: status = %d, want 401", rec.Code)
	}
}

// 断ったら access_denied を付けて戻す。コードは出さない。
func TestOAuthAuthorization_Deny(t *testing.T) {
	t.Parallel()

	r, _, db := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	req := consentRequest(t, r, registerClient(t, r))

	rec := doJSON(t, r, http.MethodPost, "/api/oauth/authorization", cookie,
		apitypes.OAuthDecisionRequest{Request: req, Approve: false})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	target, _ := url.Parse(decode[apitypes.OAuthDecision](t, rec).RedirectTo)
	if target.Query().Get("error") != "access_denied" || target.Query().Get("code") != "" {
		t.Errorf("redirectTo = %s", target)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM oauth_codes").Scan(&n); err != nil || n != 0 {
		t.Errorf("断ったのにコードが %d 件書かれた（%v）", n, err)
	}
}

// 18. 同意の返事は Origin の検証の内側にある。トークンと登録は Origin を
// 付けない MCP のクライアントから届く。
func TestOAuthAuthorization_RejectsCrossSite(t *testing.T) {
	t.Parallel()

	r, _, db := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	req := consentRequest(t, r, registerClient(t, r))

	raw, _ := json.Marshal(apitypes.OAuthDecisionRequest{Request: req, Approve: true})
	rec := request(t, r, http.MethodPost, "/api/oauth/authorization", loopbackHost, map[string]string{
		"Content-Type": "application/json",
		"Origin":       "https://evil.example",
		"Cookie":       cookie.Name + "=" + cookie.Value,
	}, string(raw))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM oauth_codes").Scan(&n); err != nil || n != 0 {
		t.Errorf("弾いたのにコードが %d 件書かれた（%v）", n, err)
	}
}

// トークンの口の誤りは RFC 6749 5.2 の形。invalid_client だけ 401。
func TestToken_ErrorResponses(t *testing.T) {
	t.Parallel()

	r, _, _ := newOAuthRouter(t, oauthRouterOptions{})

	for name, tc := range map[string]struct {
		form   url.Values
		status int
		code   string
	}{
		"知らないコード": {url.Values{
			"grant_type": {"authorization_code"}, "code": {"nope"}, "client_id": {"c"},
			"redirect_uri": {testRedirectURI}, "code_verifier": {testVerifier},
		}, http.StatusBadRequest, "invalid_grant"},
		"client_id が無い": {url.Values{"grant_type": {"authorization_code"}},
			http.StatusUnauthorized, "invalid_client"},
		"grant_type が違う": {url.Values{"grant_type": {"password"}, "client_id": {"c"}},
			http.StatusBadRequest, "unsupported_grant_type"},
	} {
		rec := formPost(t, r, "/oauth/token", tc.form)
		if rec.Code != tc.status {
			t.Errorf("%s: status = %d, want %d (%s)", name, rec.Code, tc.status, rec.Body)
			continue
		}
		var body struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error != tc.code {
			t.Errorf("%s: body = %s, want error %s", name, rec.Body, tc.code)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q", name, rec.Header().Get("Cache-Control"))
		}
	}

	rec := request(t, r, http.MethodPost, "/oauth/register", loopbackHost,
		map[string]string{"Content-Type": "application/json"},
		`{"redirect_uris":["http://app.example/cb"]}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_redirect_uri") {
		t.Errorf("register: %d %s, want 400 invalid_redirect_uri", rec.Code, rec.Body)
	}
}

// 一覧と取り消し。他人の許可は 404 で、取り消したトークンは `/mcp` を通らない。
func TestOAuthGrants_ListAndRevoke(t *testing.T) {
	t.Parallel()

	r, provider, _ := newOAuthRouter(t, oauthRouterOptions{})
	alice := signInAs(t, r, provider, "1", "alice")
	bob := signInAs(t, r, provider, "2", "bob")
	_, tokens := connectClient(t, r, alice)

	grants := decodeOK[[]apitypes.OAuthGrant](t, withCookie(t, r, http.MethodGet, "/api/oauth/grants", alice))
	if len(grants) != 1 || grants[0].ClientName != "Claude Code" || grants[0].Scope != "read" {
		t.Fatalf("grants = %+v", grants)
	}
	if theirs := decodeOK[[]apitypes.OAuthGrant](t,
		withCookie(t, r, http.MethodGet, "/api/oauth/grants", bob)); len(theirs) != 0 {
		t.Errorf("bob に alice の許可が見える: %+v", theirs)
	}

	rec := withCookie(t, r, http.MethodDelete, "/api/oauth/grants/"+grants[0].ID, bob)
	if rec.Code != http.StatusNotFound {
		t.Errorf("他人の取り消し: status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if rec := mcpListTools(t, r, map[string]string{"Authorization": "Bearer " + tokens.AccessToken}); rec.Code != http.StatusOK {
		t.Fatalf("他人が取り消そうとしただけで通らなくなった: %d", rec.Code)
	}

	rec = withCookie(t, r, http.MethodDelete, "/api/oauth/grants/"+grants[0].ID, alice)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("取り消し: status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if rec := mcpListTools(t, r, map[string]string{"Authorization": "Bearer " + tokens.AccessToken}); rec.Code != http.StatusUnauthorized {
		t.Errorf("取り消したトークンで status = %d, want 401", rec.Code)
	}

	caps := decodeOK[apitypes.Capabilities](t, withCookie(t, r, http.MethodGet, "/api/capabilities", alice))
	if !caps.McpConnections {
		t.Error("capabilities.mcpConnections = false, want true")
	}
}

// 19. ログアウトしても MCP の許可は消さない。ブラウザを閉じる操作が、手元の
// クライアントの接続を切るのは意図に合わない。
func TestMCP_SurvivesLogout(t *testing.T) {
	t.Parallel()

	r, _, _ := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	_, tokens := connectClient(t, r, cookie)

	if rec := doJSON(t, r, http.MethodPost, "/api/auth/logout", cookie, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body)
	}
	if rec := mcpListTools(t, r, map[string]string{"Authorization": "Bearer " + tokens.AccessToken}); rec.Code != http.StatusOK {
		t.Errorf("ログアウト後の /mcp: status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// refresh token で取り直したトークンも `/mcp` を通る。口を通した往復の確認。
func TestToken_RefreshOverHTTP(t *testing.T) {
	t.Parallel()

	r, _, _ := newOAuthRouter(t, oauthRouterOptions{})
	cookie := signIn(t, r)
	clientID, tokens := connectClient(t, r, cookie)

	rec := formPost(t, r, "/oauth/token", url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {tokens.RefreshToken}, "client_id": {clientID},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body)
	}
	var next tokenPair
	if err := json.Unmarshal(rec.Body.Bytes(), &next); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if next.RefreshToken == tokens.RefreshToken {
		t.Error("refresh token が入れ替わっていない")
	}
	if rec := mcpListTools(t, r, map[string]string{"Authorization": "Bearer " + next.AccessToken}); rec.Code != http.StatusOK {
		t.Errorf("更新したトークンで status = %d", rec.Code)
	}
}

// usecaseOAuthServer は Client ID Metadata Document を取りに行かない認可
// サーバーを作る。HTTP 面のテストは外へ出ない。
func usecaseOAuthServer(repo port.OAuthGrantRepository) *usecase.OAuthServer {
	return usecase.NewOAuthServer(repo, nil)
}
