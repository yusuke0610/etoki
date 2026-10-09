package usecase_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

var oauthNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

var oauthEP = usecase.OAuthEndpoints{
	Issuer:   "http://127.0.0.1:8080",
	Resource: "http://127.0.0.1:8080/mcp",
}

// fakeGrants は port.OAuthGrantRepository のメモリ実装。
//
// **期限と使用済みの扱いは SQLite の実装と同じにする**（期限ちょうどは
// 通さない、使用済みは 1 回目の控えを返す）。全部通すフェイクでは、使い回しの
// 検知を確かめられない。
type fakeGrants struct {
	mu      sync.Mutex
	clients map[string]port.OAuthClient
	codes   map[string]*fakeCode
	grants  map[string]port.OAuthGrant
	tokens  map[string]*fakeToken
}

type fakeCode struct {
	code port.OAuthCode
	used bool
}

type fakeToken struct {
	token   port.OAuthToken
	grantID string
	used    bool
}

func newFakeGrants() *fakeGrants {
	return &fakeGrants{
		clients: map[string]port.OAuthClient{},
		codes:   map[string]*fakeCode{},
		grants:  map[string]port.OAuthGrant{},
		tokens:  map[string]*fakeToken{},
	}
}

func (f *fakeGrants) CreateClient(_ context.Context, c port.OAuthClient, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[c.ID] = c
	return nil
}

func (f *fakeGrants) FindClient(_ context.Context, id string) (*port.OAuthClient, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.clients[id]
	if !ok {
		return nil, nil
	}
	return &c, nil
}

func (f *fakeGrants) SaveCode(_ context.Context, c port.OAuthCode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[c.CodeHash] = &fakeCode{code: c}
	return nil
}

func (f *fakeGrants) ConsumeCode(
	_ context.Context, hash, grantID string, now time.Time,
) (*port.OAuthCode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.codes[hash]
	if !ok || !c.code.ExpiresAt.After(now) {
		return nil, nil
	}
	out := c.code
	if c.used {
		out.Replayed = true
		return &out, nil
	}
	c.used = true
	c.code.GrantID = grantID
	out.GrantID = grantID
	return &out, nil
}

func (f *fakeGrants) CreateGrant(_ context.Context, g port.OAuthGrant, tokens []port.OAuthToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[g.ID] = g
	for _, t := range tokens {
		f.tokens[t.TokenHash] = &fakeToken{token: t, grantID: g.ID}
	}
	return nil
}

func (f *fakeGrants) FindToken(_ context.Context, hash string, now time.Time) (*port.OAuthTokenGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[hash]
	if !ok || t.used || !t.token.ExpiresAt.After(now) {
		return nil, nil
	}
	g, ok := f.grants[t.grantID]
	if !ok {
		return nil, nil
	}
	return &port.OAuthTokenGrant{Token: t.token, Grant: g}, nil
}

func (f *fakeGrants) UseRefreshToken(_ context.Context, hash string, now time.Time) (*port.OAuthRefresh, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tokens[hash]
	if !ok || t.token.Kind != port.OAuthRefreshToken || !t.token.ExpiresAt.After(now) {
		return nil, nil
	}
	g, ok := f.grants[t.grantID]
	if !ok {
		return nil, nil
	}
	if t.used {
		return &port.OAuthRefresh{Grant: g, Replayed: true}, nil
	}
	t.used = true
	return &port.OAuthRefresh{Grant: g}, nil
}

func (f *fakeGrants) AddTokens(_ context.Context, grantID string, tokens []port.OAuthToken, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.grants[grantID]
	if !ok {
		return fmt.Errorf("%w: grant %s", port.ErrNotFound, grantID)
	}
	g.LastUsedAt = now
	f.grants[grantID] = g
	for _, t := range tokens {
		f.tokens[t.TokenHash] = &fakeToken{token: t, grantID: grantID}
	}
	return nil
}

func (f *fakeGrants) FindGrant(_ context.Context, id string) (*port.OAuthGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.grants[id]
	if !ok {
		return nil, nil
	}
	return &g, nil
}

func (f *fakeGrants) ListGrants(_ context.Context, userID string) ([]port.OAuthGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []port.OAuthGrant
	for _, g := range f.grants {
		if g.UserID == userID {
			out = append(out, g)
		}
	}
	return out, nil
}

func (f *fakeGrants) ListAllGrants(context.Context) ([]port.OAuthGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []port.OAuthGrant
	for _, g := range f.grants {
		out = append(out, g)
	}
	return out, nil
}

func (f *fakeGrants) DeleteGrant(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.grants, id)
	for h, t := range f.tokens {
		if t.grantID == id {
			delete(f.tokens, h)
		}
	}
	return nil
}

// fakeFetcher は決められた文書を返す ClientMetadataFetcher。
type fakeFetcher struct {
	docs  map[string]usecase.ClientMetadata
	err   error
	calls []string
}

func (f *fakeFetcher) Fetch(_ context.Context, clientID string) (usecase.ClientMetadata, error) {
	f.calls = append(f.calls, clientID)
	if f.err != nil {
		return usecase.ClientMetadata{}, f.err
	}
	doc, ok := f.docs[clientID]
	if !ok {
		return usecase.ClientMetadata{}, errors.New("not found")
	}
	return doc, nil
}

// counterRandom は採番を予測できる値にする。発行した値を断言で名指しするため。
func counterRandom() func() (string, error) {
	var mu sync.Mutex
	n := 0
	return func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		return fmt.Sprintf("rand-%03d", n), nil
	}
}

type oauthClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *oauthClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *oauthClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newOAuthServer(t *testing.T) (*usecase.OAuthServer, *fakeGrants, *fakeFetcher, *oauthClock) {
	t.Helper()
	grants := newFakeGrants()
	fetcher := &fakeFetcher{docs: map[string]usecase.ClientMetadata{}}
	clock := &oauthClock{now: oauthNow}
	s := usecase.NewOAuthServer(grants, fetcher,
		usecase.WithOAuthClock(clock.Now), usecase.WithOAuthRandom(counterRandom()))
	return s, grants, fetcher, clock
}

const (
	testVerifier    = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	testRedirectURI = "http://127.0.0.1:33418/callback"
)

// testChallenge は testVerifier の S256（RFC 7636 付録 B の値）。
const testChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

func registerClient(t *testing.T, s *usecase.OAuthServer) port.OAuthClient {
	t.Helper()
	c, err := s.Register(t.Context(), usecase.ClientMetadata{
		ClientName:   "Claude Code",
		RedirectURIs: []string{"http://127.0.0.1/callback", "https://app.example/cb"},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return c
}

func authorizeRequest(clientID string) usecase.AuthorizeRequest {
	return usecase.AuthorizeRequest{
		ResponseType:        "code",
		ClientID:            clientID,
		RedirectURI:         testRedirectURI,
		CodeChallenge:       testChallenge,
		CodeChallengeMethod: "S256",
		Scope:               "read",
		State:               "xyz",
		Resource:            oauthEP.Resource,
	}
}

// approve は同意してコードを取り出す。
func approve(t *testing.T, s *usecase.OAuthServer, userID string, req usecase.AuthorizeRequest) string {
	t.Helper()
	location, err := s.Approve(t.Context(), userID, req, oauthEP)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	u, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse %q: %v", location, err)
	}
	return u.Query().Get("code")
}

func exchange(s *usecase.OAuthServer, ctx context.Context, clientID, code string) (usecase.TokenResponse, error) {
	return s.Token(ctx, usecase.TokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		RedirectURI:  testRedirectURI,
		ClientID:     clientID,
		CodeVerifier: testVerifier,
	}, oauthEP)
}

func requireOAuthError(t *testing.T, err error, code string) {
	t.Helper()
	var oe *usecase.OAuthError
	if !errors.As(err, &oe) {
		t.Fatalf("err = %v, want OAuthError %s", err, code)
	}
	if oe.Code != code {
		t.Errorf("OAuthError.Code = %q, want %q（%s）", oe.Code, code, oe.Description)
	}
}

// 1. 登録の戻り先は、ループバックの http（ポートは問わない）か https だけ。
func TestRegister_RedirectURIs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		uri  string
		want bool
	}{
		{"http://127.0.0.1/callback", true},
		{"http://127.0.0.1:33418/callback", true},
		{"http://[::1]:8000/cb", true},
		{"http://localhost:9999/cb", true},
		{"https://app.example/cb", true},
		{"https://app.example/cb?x=1", true},
		{"http://app.example/cb", false},
		{"http://10.0.0.1/cb", false},
		{"javascript:alert(1)", false},
		{"https://app.example/cb#frag", false},
		{"https://user@app.example/cb", false},
		{"/relative", false},
		{"", false},
		{"https://app.example/" + strings.Repeat("a", 2048), false},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			t.Parallel()
			s, grants, _, _ := newOAuthServer(t)
			c, err := s.Register(t.Context(), usecase.ClientMetadata{
				ClientName: "c", RedirectURIs: []string{tc.uri},
			})
			if tc.want {
				if err != nil {
					t.Fatalf("Register = %v, want ok", err)
				}
				if got, _ := grants.FindClient(t.Context(), c.ID); got == nil {
					t.Error("登録が保存されていない")
				}
				return
			}
			requireOAuthError(t, err, usecase.OAuthInvalidRedirectURI)
			if len(grants.clients) != 0 {
				t.Errorf("断った登録が書かれている: %v", grants.clients)
			}
		})
	}
}

func TestRegister_RejectsTooManyOrNoRedirectURIs(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newOAuthServer(t)

	_, err := s.Register(t.Context(), usecase.ClientMetadata{ClientName: "c"})
	requireOAuthError(t, err, usecase.OAuthInvalidRedirectURI)

	many := make([]string, 11)
	for i := range many {
		many[i] = fmt.Sprintf("http://127.0.0.1/cb%d", i)
	}
	_, err = s.Register(t.Context(), usecase.ClientMetadata{ClientName: "c", RedirectURIs: many})
	requireOAuthError(t, err, usecase.OAuthInvalidRedirectURI)

	_, err = s.Register(t.Context(), usecase.ClientMetadata{
		ClientName: strings.Repeat("名", 201), RedirectURIs: []string{"http://127.0.0.1/cb"},
	})
	requireOAuthError(t, err, usecase.OAuthInvalidClientMetadata)
}

// 2. 秘密を持つクライアントとしての登録は断る。
func TestRegister_RejectsConfidentialClients(t *testing.T) {
	t.Parallel()

	for _, meta := range []usecase.ClientMetadata{
		{TokenEndpointAuthMethod: "client_secret_basic"},
		{TokenEndpointAuthMethod: "client_secret_post"},
		{GrantTypes: []string{"client_credentials"}},
		{ResponseTypes: []string{"token"}},
	} {
		s, grants, _, _ := newOAuthServer(t)
		meta.ClientName = "c"
		meta.RedirectURIs = []string{"http://127.0.0.1/cb"}
		_, err := s.Register(t.Context(), meta)
		requireOAuthError(t, err, usecase.OAuthInvalidClientMetadata)
		if len(grants.clients) != 0 {
			t.Errorf("%+v: 断った登録が書かれている", meta)
		}
	}

	s, _, _, _ := newOAuthServer(t)
	if _, err := s.Register(t.Context(), usecase.ClientMetadata{
		ClientName: "c", RedirectURIs: []string{"http://127.0.0.1/cb"},
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
	}); err != nil {
		t.Errorf("公開クライアント = %v, want ok", err)
	}
}

// 2. 窓の中で上限を超えた登録は 429。上限ちょうどまでは通り、窓が過ぎれば戻る。
func TestRegister_RateLimited(t *testing.T) {
	t.Parallel()
	s, grants, _, clock := newOAuthServer(t)
	meta := usecase.ClientMetadata{ClientName: "c", RedirectURIs: []string{"http://127.0.0.1/cb"}}

	for i := range usecase.MaxClientRegistrations {
		if _, err := s.Register(t.Context(), meta); err != nil {
			t.Fatalf("Register #%d: %v", i, err)
		}
	}
	_, err := s.Register(t.Context(), meta)
	if !errors.Is(err, usecase.ErrRateLimited) {
		t.Fatalf("上限を超えた登録 = %v, want ErrRateLimited", err)
	}
	if len(grants.clients) != usecase.MaxClientRegistrations {
		t.Errorf("保存された登録 = %d, want %d（断った登録を書いてはいけない）",
			len(grants.clients), usecase.MaxClientRegistrations)
	}

	// 形の誤りで断った登録は数えない（上限の手前で確かめる）。
	clock.Advance(usecase.StateTTL)
	for range 5 {
		_, _ = s.Register(t.Context(), usecase.ClientMetadata{ClientName: "c"})
	}
	for i := range usecase.MaxClientRegistrations {
		if _, err := s.Register(t.Context(), meta); err != nil {
			t.Fatalf("窓が過ぎたあとの Register #%d: %v", i, err)
		}
	}
}

// 3. Client ID Metadata Document：文書の client_id が URL と一致しなければ断る。
// 行き先の絞り込み（SSRF の守り）は取りに行く側のテストにある。
func TestAuthorization_ClientIDMetadataDocument(t *testing.T) {
	t.Parallel()

	const id = "https://client.example/oauth/metadata.json"
	doc := usecase.ClientMetadata{
		ClientID: id, ClientName: "Example", RedirectURIs: []string{"http://127.0.0.1/callback"},
	}

	t.Run("一致すれば通る", func(t *testing.T) {
		t.Parallel()
		s, _, fetcher, _ := newOAuthServer(t)
		fetcher.docs[id] = doc
		view, err := s.Authorization(t.Context(), authorizeRequest(id), oauthEP)
		if err != nil {
			t.Fatalf("Authorization: %v", err)
		}
		if view.ClientName != "Example" || !view.ClientIDIsURL || view.ClientID != id {
			t.Errorf("view = %+v", view)
		}
	})

	t.Run("client_id が違えば断る", func(t *testing.T) {
		t.Parallel()
		s, _, fetcher, _ := newOAuthServer(t)
		other := doc
		other.ClientID = "https://evil.example/oauth/metadata.json"
		fetcher.docs[id] = other
		_, err := s.Authorization(t.Context(), authorizeRequest(id), oauthEP)
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("秘密を持つクライアントは断る", func(t *testing.T) {
		t.Parallel()
		s, _, fetcher, _ := newOAuthServer(t)
		secret := doc
		secret.TokenEndpointAuthMethod = "private_key_jwt"
		fetcher.docs[id] = secret
		_, err := s.Authorization(t.Context(), authorizeRequest(id), oauthEP)
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("取れなければ断る", func(t *testing.T) {
		t.Parallel()
		s, _, fetcher, _ := newOAuthServer(t)
		fetcher.err = errors.New("blocked address")
		_, err := s.Authorization(t.Context(), authorizeRequest(id), oauthEP)
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("URL の形が受けられなければ取りに行かない", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{
			"https://client.example",
			"https://client.example/",
			"https://client.example/a/../b",
			"https://user@client.example/meta",
			"https://client.example/meta#x",
		} {
			s, _, fetcher, _ := newOAuthServer(t)
			_, err := s.Authorization(t.Context(), authorizeRequest(bad), oauthEP)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Errorf("%s: err = %v, want ErrInvalidInput", bad, err)
			}
			if len(fetcher.calls) != 0 {
				t.Errorf("%s: 取りに行った: %v", bad, fetcher.calls)
			}
		}
	})
}

// 4. 認可の要求の検証。どれも ErrInvalidInput で、画面に出す（クライアントに
// 戻さない）。
func TestAuthorization_RejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	cases := map[string]func(r *usecase.AuthorizeRequest){
		"S256 でない":           func(r *usecase.AuthorizeRequest) { r.CodeChallengeMethod = "plain" },
		"challenge が無い":      func(r *usecase.AuthorizeRequest) { r.CodeChallenge = "" },
		"challenge の形が違う":    func(r *usecase.AuthorizeRequest) { r.CodeChallenge = "short" },
		"resource が違う":       func(r *usecase.AuthorizeRequest) { r.Resource = "http://127.0.0.1:8080/api" },
		"戻り先が登録に無い":          func(r *usecase.AuthorizeRequest) { r.RedirectURI = "https://evil.example/cb" },
		"戻り先のパスが違う":          func(r *usecase.AuthorizeRequest) { r.RedirectURI = "http://127.0.0.1:1/other" },
		"戻り先が無い":             func(r *usecase.AuthorizeRequest) { r.RedirectURI = "" },
		"response_type が違う":  func(r *usecase.AuthorizeRequest) { r.ResponseType = "token" },
		"scope が違う":          func(r *usecase.AuthorizeRequest) { r.Scope = "write" },
		"client_id が無い":      func(r *usecase.AuthorizeRequest) { r.ClientID = "" },
		"client_id が知らないもの":  func(r *usecase.AuthorizeRequest) { r.ClientID = "unknown" },
		"ループバックでホスト名が違う":     func(r *usecase.AuthorizeRequest) { r.RedirectURI = "http://localhost:1/callback" },
		"https はポートの違いを許さない": func(r *usecase.AuthorizeRequest) { r.RedirectURI = "https://app.example:8443/cb" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, grants, _, _ := newOAuthServer(t)
			c := registerClient(t, s)
			req := authorizeRequest(c.ID)
			mutate(&req)

			if _, err := s.Authorization(t.Context(), req, oauthEP); !errors.Is(err, usecase.ErrInvalidInput) {
				t.Errorf("Authorization = %v, want ErrInvalidInput", err)
			}
			if _, err := s.Approve(t.Context(), "u1", req, oauthEP); !errors.Is(err, usecase.ErrInvalidInput) {
				t.Errorf("Approve = %v, want ErrInvalidInput", err)
			}
			if len(grants.codes) != 0 {
				t.Errorf("断った要求でコードが書かれている")
			}
		})
	}
}

// resource と scope は無くてもよい。無ければ `/mcp` の read として扱う。
func TestAuthorization_DefaultsResourceAndScope(t *testing.T) {
	t.Parallel()
	s, grants, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	req := authorizeRequest(c.ID)
	req.Resource, req.Scope = "", ""

	code := approve(t, s, "u1", req)
	saved := grants.codes[hashOf(code)]
	if saved == nil {
		t.Fatal("コードが保存されていない")
	}
	if saved.code.Resource != oauthEP.Resource || saved.code.Scope != "read" {
		t.Errorf("code = %+v, want resource %s, scope read", saved.code, oauthEP.Resource)
	}
}

// 5. 同意すると code・state・iss を付けて戻す。断ったら access_denied を付けて
// 戻し、何も書かない。
func TestApproveAndDeny(t *testing.T) {
	t.Parallel()
	s, grants, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	req := authorizeRequest(c.ID)

	location, err := s.Approve(t.Context(), "u1", req, oauthEP)
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	u, _ := url.Parse(location)
	q := u.Query()
	if u.Scheme+"://"+u.Host+u.Path != testRedirectURI {
		t.Errorf("戻り先 = %s, want %s", location, testRedirectURI)
	}
	if q.Get("code") == "" || q.Get("state") != "xyz" || q.Get("iss") != oauthEP.Issuer {
		t.Errorf("query = %v", q)
	}
	saved := grants.codes[hashOf(q.Get("code"))]
	if saved == nil || saved.code.UserID != "u1" || saved.code.ClientName != "Claude Code" ||
		!saved.code.ExpiresAt.Equal(oauthNow.Add(usecase.AuthCodeTTL)) {
		t.Errorf("保存したコード = %+v", saved)
	}

	before := len(grants.codes)
	location, err = s.Deny(t.Context(), req, oauthEP)
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}
	u, _ = url.Parse(location)
	if u.Query().Get("error") != "access_denied" || u.Query().Get("state") != "xyz" ||
		u.Query().Get("code") != "" {
		t.Errorf("Deny の戻り先 = %s", location)
	}
	if len(grants.codes) != before {
		t.Error("断ったのにコードが書かれている")
	}

	// 戻り先を確かめずに戻さない。
	bad := req
	bad.RedirectURI = "https://evil.example/cb"
	if _, err := s.Deny(t.Context(), bad, oauthEP); !errors.Is(err, usecase.ErrInvalidInput) {
		t.Errorf("登録に無い戻り先への Deny = %v, want ErrInvalidInput", err)
	}

	if _, err := s.Approve(t.Context(), "", req, oauthEP); !errors.Is(err, port.ErrNotAuthenticated) {
		t.Errorf("利用者の無い Approve = %v, want ErrNotAuthenticated", err)
	}
}

// 6. code は単回使用。2 回目が来たら、その code で出したトークンも失効させる。
func TestToken_CodeReplayRevokesGrant(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	code := approve(t, s, "u1", authorizeRequest(c.ID))

	first, err := exchange(s, t.Context(), c.ID, code)
	if err != nil {
		t.Fatalf("1 回目: %v", err)
	}
	if v, _ := s.Verify(t.Context(), first.AccessToken, oauthEP); v == nil {
		t.Fatal("1 回目のトークンが通らない")
	}

	_, err = exchange(s, t.Context(), c.ID, code)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)

	if v, _ := s.Verify(t.Context(), first.AccessToken, oauthEP); v != nil {
		t.Error("使い回しのあとも 1 回目のアクセストークンが通る")
	}
	_, err = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: first.RefreshToken, ClientID: c.ID,
	}, oauthEP)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)
}

func TestToken_ExchangeIssuesTokens(t *testing.T) {
	t.Parallel()
	s, grants, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	code := approve(t, s, "u1", authorizeRequest(c.ID))

	got, err := exchange(s, t.Context(), c.ID, code)
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if got.AccessToken == "" || got.RefreshToken == "" || got.AccessToken == got.RefreshToken {
		t.Errorf("tokens = %+v", got)
	}
	if got.ExpiresIn != 3600 || got.Scope != "read" {
		t.Errorf("ExpiresIn = %d, Scope = %q", got.ExpiresIn, got.Scope)
	}

	v, err := s.Verify(t.Context(), got.AccessToken, oauthEP)
	if err != nil || v == nil {
		t.Fatalf("Verify = %v, %v", v, err)
	}
	if v.UserID != "u1" || !slices.Equal(v.Scopes, []string{"read"}) ||
		!v.ExpiresAt.Equal(oauthNow.Add(usecase.AccessTokenTTL)) {
		t.Errorf("Verify = %+v", *v)
	}

	list, _ := grants.ListGrants(t.Context(), "u1")
	if len(list) != 1 || list[0].ClientID != c.ID || list[0].ClientName != "Claude Code" {
		t.Errorf("grants = %+v", list)
	}

	// refresh token を `/mcp` に送っても通さない。
	if v, _ := s.Verify(t.Context(), got.RefreshToken, oauthEP); v != nil {
		t.Error("refresh token が `/mcp` を通る")
	}
}

// 7. verifier の不一致・別のクライアントの code・戻り先の不一致・resource の
// 不一致は断る。断ったコードも使い切る（総当たりさせない）。
func TestToken_RejectsMismatches(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(r *usecase.TokenRequest)
		want   string
	}{
		"verifier が違う": {func(r *usecase.TokenRequest) {
			r.CodeVerifier = strings.Repeat("a", 43)
		}, usecase.OAuthInvalidGrant},
		"verifier の形が違う": {func(r *usecase.TokenRequest) {
			r.CodeVerifier = "short"
		}, usecase.OAuthInvalidGrant},
		"別のクライアント": {func(r *usecase.TokenRequest) {
			r.ClientID = "other-client"
		}, usecase.OAuthInvalidGrant},
		"戻り先が違う": {func(r *usecase.TokenRequest) {
			r.RedirectURI = "http://127.0.0.1:1/callback"
		}, usecase.OAuthInvalidGrant},
		"resource が違う": {func(r *usecase.TokenRequest) {
			r.Resource = "http://127.0.0.1:8080/api"
		}, usecase.OAuthInvalidTarget},
		"scope が違う": {func(r *usecase.TokenRequest) {
			r.Scope = "write"
		}, usecase.OAuthInvalidScope},
		"client_id が無い": {func(r *usecase.TokenRequest) {
			r.ClientID = ""
		}, usecase.OAuthInvalidClient},
		"grant_type が違う": {func(r *usecase.TokenRequest) {
			r.GrantType = "client_credentials"
		}, usecase.OAuthUnsupportedGrantType},
		"code が知らないもの": {func(r *usecase.TokenRequest) {
			r.Code = "unknown"
		}, usecase.OAuthInvalidGrant},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, grants, _, _ := newOAuthServer(t)
			c := registerClient(t, s)
			code := approve(t, s, "u1", authorizeRequest(c.ID))

			req := usecase.TokenRequest{
				GrantType: "authorization_code", Code: code, RedirectURI: testRedirectURI,
				ClientID: c.ID, CodeVerifier: testVerifier,
			}
			tc.mutate(&req)
			_, err := s.Token(t.Context(), req, oauthEP)
			requireOAuthError(t, err, tc.want)
			if len(grants.grants) != 0 {
				t.Error("断ったのに許可が書かれている")
			}
		})
	}

	// 断ったあとに正しい verifier で来ても通さない。
	s, _, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	code := approve(t, s, "u1", authorizeRequest(c.ID))
	_, _ = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "authorization_code", Code: code, RedirectURI: testRedirectURI,
		ClientID: c.ID, CodeVerifier: strings.Repeat("a", 43),
	}, oauthEP)
	_, err := exchange(s, t.Context(), c.ID, code)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)
}

// 7. 別の Host で発行したコードは、その Host でしか引き換えられない。
func TestToken_CodeBoundToResource(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	code := approve(t, s, "u1", authorizeRequest(c.ID))

	other := usecase.OAuthEndpoints{Issuer: "http://localhost:8080", Resource: "http://localhost:8080/mcp"}
	_, err := s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "authorization_code", Code: code, RedirectURI: testRedirectURI,
		ClientID: c.ID, CodeVerifier: testVerifier,
	}, other)
	requireOAuthError(t, err, usecase.OAuthInvalidTarget)
}

// 8. refresh token は使うたびに入れ替える。古いほうが再び来たら許可ごと失効し、
// 新しいほうも使えなくなる。
func TestToken_RefreshRotationAndReplay(t *testing.T) {
	t.Parallel()
	s, grants, _, clock := newOAuthServer(t)
	c := registerClient(t, s)
	first, err := exchange(s, t.Context(), c.ID, approve(t, s, "u1", authorizeRequest(c.ID)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	clock.Advance(2 * time.Hour)
	second, err := s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: first.RefreshToken, ClientID: c.ID,
	}, oauthEP)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken || second.AccessToken == first.AccessToken {
		t.Errorf("入れ替わっていない: %+v", second)
	}
	if v, _ := s.Verify(t.Context(), second.AccessToken, oauthEP); v == nil {
		t.Fatal("更新したアクセストークンが通らない")
	}
	list, _ := grants.ListGrants(t.Context(), "u1")
	if len(list) != 1 || !list[0].LastUsedAt.Equal(oauthNow.Add(2*time.Hour)) {
		t.Errorf("LastUsedAt が進んでいない: %+v", list)
	}

	// 古い refresh token がもう一度来た。
	_, err = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: first.RefreshToken, ClientID: c.ID,
	}, oauthEP)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)

	if v, _ := s.Verify(t.Context(), second.AccessToken, oauthEP); v != nil {
		t.Error("使い回しのあとも新しいアクセストークンが通る")
	}
	_, err = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: second.RefreshToken, ClientID: c.ID,
	}, oauthEP)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)
}

func TestToken_RefreshFromAnotherClient(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	first, err := exchange(s, t.Context(), c.ID, approve(t, s, "u1", authorizeRequest(c.ID)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	_, err = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: first.RefreshToken, ClientID: "other",
	}, oauthEP)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)
}

// 9. 期限ちょうどのアクセストークンは通さない。30 日を過ぎた refresh token も。
func TestVerify_Expiry(t *testing.T) {
	t.Parallel()
	s, _, _, clock := newOAuthServer(t)
	c := registerClient(t, s)
	tokens, err := exchange(s, t.Context(), c.ID, approve(t, s, "u1", authorizeRequest(c.ID)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	clock.Advance(usecase.AccessTokenTTL - time.Nanosecond)
	if v, _ := s.Verify(t.Context(), tokens.AccessToken, oauthEP); v == nil {
		t.Error("期限の直前で通らない")
	}
	clock.Advance(time.Nanosecond)
	if v, _ := s.Verify(t.Context(), tokens.AccessToken, oauthEP); v != nil {
		t.Error("期限ちょうどで通る")
	}

	clock.Advance(usecase.RefreshTokenTTL - usecase.AccessTokenTTL)
	_, err = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: tokens.RefreshToken, ClientID: c.ID,
	}, oauthEP)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)
}

// 9. 期限を過ぎたコードは引き換えられない。
func TestToken_CodeExpiry(t *testing.T) {
	t.Parallel()
	s, _, _, clock := newOAuthServer(t)
	c := registerClient(t, s)
	code := approve(t, s, "u1", authorizeRequest(c.ID))

	clock.Advance(usecase.AuthCodeTTL)
	_, err := exchange(s, t.Context(), c.ID, code)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)
}

// 別の Host の `/mcp` には使わせない。
func TestVerify_BoundToResource(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	tokens, err := exchange(s, t.Context(), c.ID, approve(t, s, "u1", authorizeRequest(c.ID)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	other := usecase.OAuthEndpoints{Resource: "http://localhost:8080/mcp"}
	if v, _ := s.Verify(t.Context(), tokens.AccessToken, other); v != nil {
		t.Error("別の resource で通る")
	}
}

// 10. コードもトークンも平文で保存しない。保存されるのは SHA-256 だけ。
func TestOAuth_StoresOnlyHashes(t *testing.T) {
	t.Parallel()
	s, grants, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	code := approve(t, s, "u1", authorizeRequest(c.ID))
	tokens, err := exchange(s, t.Context(), c.ID, code)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	if _, ok := grants.codes[code]; ok {
		t.Error("コードが平文で保存されている")
	}
	if _, ok := grants.codes[hashOf(code)]; !ok {
		t.Error("コードのハッシュで引けない")
	}
	for _, v := range []string{tokens.AccessToken, tokens.RefreshToken} {
		if _, ok := grants.tokens[v]; ok {
			t.Errorf("トークン %q が平文で保存されている", v)
		}
		if _, ok := grants.tokens[hashOf(v)]; !ok {
			t.Errorf("トークン %q のハッシュで引けない", v)
		}
	}
}

// 11. 取り消すと、その許可のトークンは直ちに通らなくなる。他人の許可は
// 見つからない扱い。
func TestRevoke(t *testing.T) {
	t.Parallel()
	s, _, _, _ := newOAuthServer(t)
	c := registerClient(t, s)
	tokens, err := exchange(s, t.Context(), c.ID, approve(t, s, "u1", authorizeRequest(c.ID)))
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	list, err := s.Grants(t.Context(), "u1")
	if err != nil || len(list) != 1 {
		t.Fatalf("Grants = %v, %v", list, err)
	}

	if err := s.Revoke(t.Context(), "u2", list[0].ID); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("他人の取り消し = %v, want ErrNotFound", err)
	}
	if v, _ := s.Verify(t.Context(), tokens.AccessToken, oauthEP); v == nil {
		t.Fatal("他人が取り消そうとしただけで通らなくなった")
	}

	if err := s.Revoke(t.Context(), "u1", list[0].ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if v, _ := s.Verify(t.Context(), tokens.AccessToken, oauthEP); v != nil {
		t.Error("取り消したあともアクセストークンが通る")
	}
	_, err = s.Token(t.Context(), usecase.TokenRequest{
		GrantType: "refresh_token", RefreshToken: tokens.RefreshToken, ClientID: c.ID,
	}, oauthEP)
	requireOAuthError(t, err, usecase.OAuthInvalidGrant)

	if err := s.Revoke(t.Context(), "u1", list[0].ID); !errors.Is(err, port.ErrNotFound) {
		t.Errorf("2 回目の取り消し = %v, want ErrNotFound", err)
	}
}

// 12. 掃除の範囲は CreateClient に渡す境界で決まる。境界は登録した時刻から
// UnusedClientTTL を引いた値。
func TestRegister_PassesUnusedCutoff(t *testing.T) {
	t.Parallel()
	grants := &cutoffRecorder{fakeGrants: newFakeGrants()}
	s := usecase.NewOAuthServer(grants, nil,
		usecase.WithOAuthClock(func() time.Time { return oauthNow }),
		usecase.WithOAuthRandom(counterRandom()))
	if _, err := s.Register(t.Context(), usecase.ClientMetadata{
		ClientName: "c", RedirectURIs: []string{"http://127.0.0.1/cb"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if want := oauthNow.Add(-usecase.UnusedClientTTL); !grants.cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", grants.cutoff, want)
	}
}

type cutoffRecorder struct {
	*fakeGrants
	cutoff time.Time
}

func (c *cutoffRecorder) CreateClient(ctx context.Context, cl port.OAuthClient, cutoff time.Time) error {
	c.cutoff = cutoff
	return c.fakeGrants.CreateClient(ctx, cl, cutoff)
}

// fetcher が無い構成では、Client ID Metadata Document のクライアントを断る。
func TestAuthorization_WithoutFetcher(t *testing.T) {
	t.Parallel()
	s := usecase.NewOAuthServer(newFakeGrants(), nil)
	_, err := s.Authorization(t.Context(),
		authorizeRequest("https://client.example/meta.json"), oauthEP)
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
}

func hashOf(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// testChallenge が testVerifier の S256 であることを固定する。値を書き換えた
// ときに、テストの前提が崩れたことに気づけるように。
func TestPKCEFixture(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256([]byte(testVerifier))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != testChallenge {
		t.Fatalf("challenge = %s, want %s", got, testChallenge)
	}
}
