package usecase

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/yusuke0610/etoki/internal/loopback"
	"github.com/yusuke0610/etoki/port"
)

// MCP のクライアントに etoki が発行する許可（ADR 0076）に固有の設定。
const (
	// OAuthScopeRead は読み取りの道具を使う scope。いまある scope はこれだけ。
	//
	// 道具が読み取りだけなので（ADR 0071）。書き込みの道具を足すときに scope を
	// 増やし、同意の画面に出す。
	OAuthScopeRead = "read"

	// AccessTokenTTL はアクセストークンの寿命。
	//
	// 取り消しは DB を引いてすぐ効くので、寿命で守っているのは「漏れた値が
	// 使える長さ」だけ。短くすると更新の往復が増える。
	AccessTokenTTL = time.Hour

	// RefreshTokenTTL は refresh token の寿命。使うたびに入れ替えるので、
	// 実際には「最後に使ってから」の長さになる。
	//
	// 画面のセッション（SessionTTL）より長いのは、MCP のクライアントは
	// ブラウザと違ってやり直しの導線が重いため（同意の画面を開き直す）。
	RefreshTokenTTL = 30 * 24 * time.Hour

	// AuthCodeTTL は認可コードを引き換えられる期間。
	//
	// クライアントは戻ってきたらすぐ引き換える。長くしても、漏れた値が使える
	// 窓が広がるだけ。
	AuthCodeTTL = 5 * time.Minute

	// UnusedClientTTL は一度も許可を得なかった登録を残す期間。
	//
	// 登録から同意までにかかる時間より十分長ければよい。
	UnusedClientTTL = 24 * time.Hour

	// MaxClientRegistrations は StateTTL のあいだに受ける登録の回数。
	//
	// **登録は、ログインしていなくても書き込みが起きる 2 つめの口**（1 つめは
	// ログインの開始、MaxLoginStarts）。同じ形で絞る。60 に根拠は無く、
	// MaxLoginStarts と揃えただけ。実際に詰まるなら動かす。
	MaxClientRegistrations = 60

	// 登録とメタデータで受ける大きさの上限。名前は同意の画面に出すので、
	// 画面を埋めない長さに絞る。
	maxRedirectURIs    = 10
	maxRedirectURILen  = 2048
	maxClientNameRunes = 200
)

// OAuthError は OAuth の口（`/oauth/*`）で返す誤り。
//
// **sentinel ではなく型にする。** code は RFC が決める語彙で、画面のための
// 契約の code（ADR 0034）ではない。errors.go の表に載せると、画面が読まない
// code が契約に混ざる。
type OAuthError struct {
	// Code は RFC 6749 / 7591 / 8707 の error。
	Code string
	// Description は error_description。手掛かりであって利用者向けの文言ではない。
	Description string
}

func (e *OAuthError) Error() string {
	return "etoki: oauth " + e.Code + ": " + e.Description
}

func oauthError(code, format string, args ...any) error {
	return &OAuthError{Code: code, Description: fmt.Sprintf(format, args...)}
}

// RFC が決める error の値。
const (
	OAuthInvalidRequest          = "invalid_request"
	OAuthInvalidClient           = "invalid_client"
	OAuthInvalidGrant            = "invalid_grant"
	OAuthUnsupportedGrantType    = "unsupported_grant_type"
	OAuthInvalidScope            = "invalid_scope"
	OAuthInvalidTarget           = "invalid_target"
	OAuthInvalidRedirectURI      = "invalid_redirect_uri"
	OAuthInvalidClientMetadata   = "invalid_client_metadata"
	OAuthAccessDenied            = "access_denied"
	oauthResponseTypeCode        = "code"
	oauthChallengeMethodS256     = "S256"
	oauthGrantAuthorizationCode  = "authorization_code"
	oauthGrantRefreshToken       = "refresh_token"
	oauthTokenEndpointAuthNone   = "none"
	clientMetadataDocumentScheme = "https://"
)

// ClientMetadata はクライアントが名乗ったメタデータ。
//
// 動的登録の本文と Client ID Metadata Document の両方をこの形で受ける。
type ClientMetadata struct {
	ClientID                string
	ClientName              string
	RedirectURIs            []string
	TokenEndpointAuthMethod string
	GrantTypes              []string
	ResponseTypes           []string
}

// ClientMetadataFetcher は Client ID Metadata Document を取りに行く。
//
// **外向きの通信なのでインターフェースで切る。** 行き先の絞り込み（SSRF の
// 守り）とキャッシュは実装の責務で、ここは取れた文書の中身だけを見る。
// port に置かないのは、差し替える当てが外に無いため（ADR 0015）。
type ClientMetadataFetcher interface {
	Fetch(ctx context.Context, clientID string) (ClientMetadata, error)
}

// OAuthEndpoints はリクエストごとに決まる自分の URL。
//
// issuer と resource は ETOKI_PUBLIC_URL か、リクエストの Host から組む
// （ログインの redirect_uri と同じ規則）。組むのは HTTP 層の仕事。
type OAuthEndpoints struct {
	// Issuer は認可サーバーの識別子（RFC 8414）。認可の応答の iss に載せる。
	Issuer string
	// Resource は `/mcp` の正規の URL（RFC 8707）。トークンを使える先。
	Resource string
}

// AuthorizeRequest は認可の要求（`/oauth/authorize` のクエリ）。
type AuthorizeRequest struct {
	ResponseType        string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	Scope               string
	State               string
	Resource            string
}

// AuthorizationView は同意の画面に出すもの。
type AuthorizationView struct {
	ClientID   string
	ClientName string
	// ClientIDIsURL は Client ID Metadata Document で名乗ったなら真。
	//
	// 名前はどちらもクライアントの自称だが、URL で名乗ったものは client_id の
	// ドメインが出どころになる。画面はそれを見せる。
	ClientIDIsURL bool
	RedirectURI   string
	Scope         string
}

// TokenRequest はトークンの要求（`/oauth/token` の本文）。
type TokenRequest struct {
	GrantType    string
	Code         string
	RedirectURI  string
	ClientID     string
	CodeVerifier string
	RefreshToken string
	Resource     string
	Scope        string
}

// TokenResponse はトークンの応答（RFC 6749 5.1）。
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	Scope        string
}

// VerifiedToken は `/mcp` に届いたアクセストークンを検証した結果。
type VerifiedToken struct {
	UserID    string
	Scopes    []string
	ExpiresAt time.Time
}

// OAuthServer は MCP のための認可サーバー（ADR 0076）。
//
// 「誰であるか」は持たない。同意を受けるのは画面のログインを通った利用者で、
// 利用者 ID は呼び出し側が渡す。
type OAuthServer struct {
	grants  port.OAuthGrantRepository
	fetcher ClientMetadataFetcher
	now     func() time.Time
	random  func() (string, error)

	// mu と registrations は StateTTL の窓の中で受けた登録の時刻。
	//
	// **状態はメモリだけ。** 理由は AuthService.loginStarts と同じ。
	mu            sync.Mutex
	registrations []time.Time
}

// OAuthServerOption は OAuthServer の依存を差し替える。
type OAuthServerOption func(*OAuthServer)

// WithOAuthClock は時刻の取得方法を差し替える。
func WithOAuthClock(f func() time.Time) OAuthServerOption {
	return func(s *OAuthServer) { s.now = f }
}

// WithOAuthRandom はコード・トークン・ID の採番を差し替える。
func WithOAuthRandom(f func() (string, error)) OAuthServerOption {
	return func(s *OAuthServer) { s.random = f }
}

// NewOAuthServer は OAuthServer を作る。
//
// fetcher は nil でもよい。そのときは Client ID Metadata Document で名乗る
// クライアントを断る。
func NewOAuthServer(
	grants port.OAuthGrantRepository, fetcher ClientMetadataFetcher, opts ...OAuthServerOption,
) *OAuthServer {
	s := &OAuthServer{grants: grants, fetcher: fetcher, now: time.Now, random: randomToken}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// hashOAuthSecret はコードとトークンを保存できる形にする。
//
// 値そのものは保存しない。DB が漏れても生きたトークンにならないようにする
// ため（ADR 0015 のセッションと同じ）。
func hashOAuthSecret(v string) string {
	return HashSessionToken(v)
}

// Register は動的登録（RFC 7591）を受ける。
//
// **検証を通ったものだけを数える。** 形の誤りで断ったリクエストは何も書かない
// ので、窓を食わせる理由が無い。
func (s *OAuthServer) Register(ctx context.Context, meta ClientMetadata) (port.OAuthClient, error) {
	name, uris, err := validateClientMetadata(meta)
	if err != nil {
		return port.OAuthClient{}, err
	}

	now := s.now()
	if err := s.admitRegistration(now); err != nil {
		return port.OAuthClient{}, err
	}

	id, err := s.random()
	if err != nil {
		return port.OAuthClient{}, err
	}
	client := port.OAuthClient{ID: id, Name: name, RedirectURIs: uris, CreatedAt: now}
	if err := s.grants.CreateClient(ctx, client, now.Add(-UnusedClientTTL)); err != nil {
		return port.OAuthClient{}, err
	}
	return client, nil
}

// admitRegistration は窓の中の登録回数を見て、1 回ぶんを数える。
// 考え方は AuthService.admitLoginStart と同じ。
func (s *OAuthServer) admitRegistration(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.registrations = withinWindow(s.registrations, now.Add(-StateTTL))
	if len(s.registrations) >= MaxClientRegistrations {
		return fmt.Errorf("%w: %d client registrations within %s, limit is %d",
			ErrRateLimited, len(s.registrations), StateTTL, MaxClientRegistrations)
	}
	s.registrations = append(s.registrations, now)
	return nil
}

// validateClientMetadata はクライアントのメタデータを検証し、名前と戻り先を返す。
//
// 動的登録と Client ID Metadata Document で同じ規則を当てる。
func validateClientMetadata(meta ClientMetadata) (string, []string, error) {
	// **公開クライアントだけを受ける。** MCP のクライアントは利用者の手元で
	// 動き、秘密を守れない。秘密を持つ前提の登録を受けると、守れない秘密で
	// 守っているつもりになる。
	if m := meta.TokenEndpointAuthMethod; m != "" && m != oauthTokenEndpointAuthNone {
		return "", nil, oauthError(OAuthInvalidClientMetadata,
			"token_endpoint_auth_method must be none, got %q", m)
	}
	for _, g := range meta.GrantTypes {
		if g != oauthGrantAuthorizationCode && g != oauthGrantRefreshToken {
			return "", nil, oauthError(OAuthInvalidClientMetadata, "unsupported grant_type %q", g)
		}
	}
	for _, r := range meta.ResponseTypes {
		if r != oauthResponseTypeCode {
			return "", nil, oauthError(OAuthInvalidClientMetadata, "unsupported response_type %q", r)
		}
	}

	name := strings.TrimSpace(meta.ClientName)
	if utf8.RuneCountInString(name) > maxClientNameRunes {
		return "", nil, oauthError(OAuthInvalidClientMetadata,
			"client_name must be at most %d characters", maxClientNameRunes)
	}

	if len(meta.RedirectURIs) == 0 || len(meta.RedirectURIs) > maxRedirectURIs {
		return "", nil, oauthError(OAuthInvalidRedirectURI,
			"redirect_uris must have 1 to %d entries", maxRedirectURIs)
	}
	for _, u := range meta.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			return "", nil, err
		}
	}
	return name, meta.RedirectURIs, nil
}

// validateRedirectURI は戻り先が受けられる形かを見る。
//
// **ループバックの http か、https だけ。** それ以外の http を許すと、code が
// 平文で他所へ流れる。`javascript:` のようなスキームは論外。フラグメントは
// RFC 6749 3.1.2 が禁じている。
func validateRedirectURI(raw string) error {
	if len(raw) > maxRedirectURILen {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri must be an absolute URL")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri must not have a fragment")
	}
	if u.User != nil {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri must not have userinfo")
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && loopback.Hostname(u.Hostname()):
		return nil
	default:
		return oauthError(OAuthInvalidRedirectURI,
			"redirect_uri must be https or http on a loopback address")
	}
}

// redirectURIMatches は要求の戻り先が登録のどれかと一致するかを返す。
//
// **ループバックの http だけはポートの違いを許す**（RFC 8252 7.3）。ネイティブの
// クライアントは、そのとき空いているポートで待つ。それ以外は文字列の完全一致。
func redirectURIMatches(registered []string, requested string) bool {
	req, err := url.Parse(requested)
	if err != nil {
		return false
	}
	for _, r := range registered {
		if r == requested {
			return true
		}
		reg, err := url.Parse(r)
		if err != nil {
			continue
		}
		if reg.Scheme == "http" && req.Scheme == "http" &&
			loopback.Hostname(reg.Hostname()) && reg.Hostname() == req.Hostname() &&
			reg.EscapedPath() == req.EscapedPath() && reg.RawQuery == req.RawQuery {
			return true
		}
	}
	return false
}

// resolvedClient は client_id を引いた結果。
type resolvedClient struct {
	id           string
	name         string
	redirectURIs []string
	isURL        bool
}

// resolveClient は client_id からクライアントを引く。
//
// https で始まる client_id は Client ID Metadata Document として取りに行く。
// それ以外は動的登録の ID として表を引く。動的登録の ID は乱数なので https で
// 始まることは無い。
func (s *OAuthServer) resolveClient(ctx context.Context, clientID string) (resolvedClient, error) {
	if clientID == "" {
		return resolvedClient{}, fmt.Errorf("%w: client_id is required", ErrInvalidInput)
	}

	if !strings.HasPrefix(clientID, clientMetadataDocumentScheme) {
		c, err := s.grants.FindClient(ctx, clientID)
		if err != nil {
			return resolvedClient{}, err
		}
		if c == nil {
			return resolvedClient{}, fmt.Errorf("%w: unknown client_id", ErrInvalidInput)
		}
		return resolvedClient{id: c.ID, name: c.Name, redirectURIs: c.RedirectURIs}, nil
	}

	if err := validateClientIDURL(clientID); err != nil {
		return resolvedClient{}, err
	}
	if s.fetcher == nil {
		return resolvedClient{}, fmt.Errorf(
			"%w: client id metadata documents are not supported", ErrInvalidInput)
	}
	meta, err := s.fetcher.Fetch(ctx, clientID)
	if err != nil {
		return resolvedClient{}, fmt.Errorf("%w: fetch client metadata: %w", ErrInvalidInput, err)
	}
	// **文書の client_id が URL と一致しなければ断る**（CIMD の定め）。別の
	// URL の文書を置かれると、名乗る名前の出どころが URL のドメインでなくなる。
	if meta.ClientID != clientID {
		return resolvedClient{}, fmt.Errorf(
			"%w: client metadata document has client_id %q", ErrInvalidInput, meta.ClientID)
	}
	name, uris, err := validateClientMetadata(meta)
	if err != nil {
		return resolvedClient{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return resolvedClient{id: clientID, name: name, redirectURIs: uris, isURL: true}, nil
}

// validateClientIDURL は Client ID Metadata Document の URL の形を見る。
//
// 行き先の絞り込み（プライベートなアドレスに繋がない、など）は取りに行く側の
// 仕事で、ここは URL として受けられるかだけを見る。
func validateClientIDURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("%w: client_id must be an https URL", ErrInvalidInput)
	}
	// パスを必須にするのは CIMD の定め。ホストだけの URL を許すと、同じ
	// ドメインの別のクライアントと見分けられない。
	if u.Path == "" || u.Path == "/" {
		return fmt.Errorf("%w: client_id URL must have a path", ErrInvalidInput)
	}
	if u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return fmt.Errorf("%w: client_id URL must not have a fragment or userinfo", ErrInvalidInput)
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: client_id URL must not have dot segments", ErrInvalidInput)
		}
	}
	return nil
}

// checkAuthorize は認可の要求を検証し、クライアントを引く。
//
// **誤りはクライアントに戻さず、画面に出す。** 戻り先を確かめる前に戻すと
// オープンリダイレクトになる。確かめたあとの誤りも画面に出すのは、戻すかどうかで
// 分岐を持つほどの得が無いため。利用者は画面で「戻る」を押せる（Deny）。
func (s *OAuthServer) checkAuthorize(
	ctx context.Context, req AuthorizeRequest, ep OAuthEndpoints,
) (resolvedClient, error) {
	client, err := s.resolveClient(ctx, req.ClientID)
	if err != nil {
		return resolvedClient{}, err
	}
	if req.RedirectURI == "" || !redirectURIMatches(client.redirectURIs, req.RedirectURI) {
		return resolvedClient{}, fmt.Errorf("%w: redirect_uri is not registered", ErrInvalidInput)
	}

	if req.ResponseType != oauthResponseTypeCode {
		return resolvedClient{}, fmt.Errorf("%w: response_type must be code", ErrInvalidInput)
	}
	// PKCE は S256 だけ。plain を許すと、challenge がそのまま verifier になる。
	if req.CodeChallengeMethod != oauthChallengeMethodS256 {
		return resolvedClient{}, fmt.Errorf("%w: code_challenge_method must be S256", ErrInvalidInput)
	}
	if !isBase64URL(req.CodeChallenge, 43, 43) {
		return resolvedClient{}, fmt.Errorf("%w: code_challenge must be a S256 value", ErrInvalidInput)
	}
	if req.Scope != "" && req.Scope != OAuthScopeRead {
		return resolvedClient{}, fmt.Errorf("%w: scope must be %q", ErrInvalidInput, OAuthScopeRead)
	}
	// resource は無くてもよい（RFC 8707 は任意）。無ければ `/mcp` を指したものと
	// して扱う。**在るのに違うものは断る。** 別の資源のためのトークンを
	// `/mcp` 向けに出すことになる。
	if req.Resource != "" && req.Resource != ep.Resource {
		return resolvedClient{}, fmt.Errorf("%w: resource must be %s", ErrInvalidInput, ep.Resource)
	}
	return client, nil
}

// Authorization は同意の画面に出すものを返す。何も書かない。
func (s *OAuthServer) Authorization(
	ctx context.Context, req AuthorizeRequest, ep OAuthEndpoints,
) (AuthorizationView, error) {
	client, err := s.checkAuthorize(ctx, req, ep)
	if err != nil {
		return AuthorizationView{}, err
	}
	return AuthorizationView{
		ClientID:      client.id,
		ClientName:    client.name,
		ClientIDIsURL: client.isURL,
		RedirectURI:   req.RedirectURI,
		Scope:         OAuthScopeRead,
	}, nil
}

// Approve は利用者の同意を受けてコードを発行し、クライアントに戻す URL を返す。
//
// **要求は受け取り直して検証し直す。** 同意の画面に出したときの検証を
// 信じると、画面とこの口のあいだで要求を差し替えられる。
func (s *OAuthServer) Approve(
	ctx context.Context, userID string, req AuthorizeRequest, ep OAuthEndpoints,
) (string, error) {
	if userID == "" {
		return "", fmt.Errorf("%w: approving requires a signed-in user", port.ErrNotAuthenticated)
	}
	client, err := s.checkAuthorize(ctx, req, ep)
	if err != nil {
		return "", err
	}

	code, err := s.random()
	if err != nil {
		return "", err
	}
	now := s.now()
	if err := s.grants.SaveCode(ctx, port.OAuthCode{
		CodeHash:      hashOAuthSecret(code),
		UserID:        userID,
		ClientID:      client.id,
		ClientName:    client.name,
		Scope:         OAuthScopeRead,
		Resource:      ep.Resource,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
		CreatedAt:     now,
		ExpiresAt:     now.Add(AuthCodeTTL),
	}); err != nil {
		return "", err
	}

	return redirectWith(req.RedirectURI, url.Values{
		"code":  {code},
		"state": optional(req.State),
		"iss":   {ep.Issuer},
	})
}

// Deny は利用者が断ったことをクライアントに戻す URL を返す。何も書かない。
//
// 戻り先は確かめてから使う。確かめずに戻すとオープンリダイレクトになる。
func (s *OAuthServer) Deny(ctx context.Context, req AuthorizeRequest, ep OAuthEndpoints) (string, error) {
	client, err := s.resolveClient(ctx, req.ClientID)
	if err != nil {
		return "", err
	}
	if req.RedirectURI == "" || !redirectURIMatches(client.redirectURIs, req.RedirectURI) {
		return "", fmt.Errorf("%w: redirect_uri is not registered", ErrInvalidInput)
	}
	return redirectWith(req.RedirectURI, url.Values{
		"error": {OAuthAccessDenied},
		"state": optional(req.State),
		"iss":   {ep.Issuer},
	})
}

func optional(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}

// redirectWith は戻り先に応答のパラメータを足す。登録した戻り先が持っていた
// クエリは残す（RFC 6749 3.1.2）。
func redirectWith(redirectURI string, params url.Values) (string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("%w: redirect_uri: %w", ErrInvalidInput, err)
	}
	q := u.Query()
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Token はトークンの要求を受ける（`/oauth/token`）。
func (s *OAuthServer) Token(ctx context.Context, req TokenRequest, ep OAuthEndpoints) (TokenResponse, error) {
	if req.ClientID == "" {
		// 公開クライアントは client_id を本文に載せる（RFC 6749 3.2.1）。
		return TokenResponse{}, oauthError(OAuthInvalidClient, "client_id is required")
	}
	if req.Scope != "" && req.Scope != OAuthScopeRead {
		return TokenResponse{}, oauthError(OAuthInvalidScope, "scope must be %q", OAuthScopeRead)
	}
	if req.Resource != "" && req.Resource != ep.Resource {
		return TokenResponse{}, oauthError(OAuthInvalidTarget, "resource must be %s", ep.Resource)
	}

	switch req.GrantType {
	case oauthGrantAuthorizationCode:
		return s.exchangeCode(ctx, req, ep)
	case oauthGrantRefreshToken:
		return s.refresh(ctx, req, ep)
	default:
		return TokenResponse{}, oauthError(OAuthUnsupportedGrantType, "grant_type %q", req.GrantType)
	}
}

func (s *OAuthServer) exchangeCode(
	ctx context.Context, req TokenRequest, ep OAuthEndpoints,
) (TokenResponse, error) {
	if req.Code == "" || req.CodeVerifier == "" || req.RedirectURI == "" {
		return TokenResponse{}, oauthError(OAuthInvalidRequest,
			"code, code_verifier and redirect_uri are required")
	}

	grantID, err := s.random()
	if err != nil {
		return TokenResponse{}, err
	}
	now := s.now()

	// **照合より先に使用済みにする。** PKCE や戻り先の不一致で断った場合も
	// コードは使い切る。試すたびに使えると、verifier を総当たりできる。
	code, err := s.grants.ConsumeCode(ctx, hashOAuthSecret(req.Code), grantID, now)
	if err != nil {
		return TokenResponse{}, err
	}
	if code == nil {
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "code is unknown or expired")
	}
	if code.Replayed {
		// 使い回されたコードは漏れたと見なし、そこから出した許可ごと失効させる
		// （RFC 6749 4.1.2）。正規のクライアントも止まるが、漏れたトークンを
		// 生かしておくよりよい。
		if err := s.grants.DeleteGrant(ctx, code.GrantID); err != nil {
			return TokenResponse{}, err
		}
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "code was already used")
	}

	if code.ClientID != req.ClientID {
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "code was issued to another client")
	}
	if code.RedirectURI != req.RedirectURI {
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "redirect_uri does not match")
	}
	if !verifyPKCE(req.CodeVerifier, code.CodeChallenge) {
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "code_verifier does not match")
	}
	if code.Resource != ep.Resource {
		// 発行したときと今とで `/mcp` の URL が違う（別の Host で来た）。
		return TokenResponse{}, oauthError(OAuthInvalidTarget, "code was issued for another resource")
	}

	access, refresh, tokens, err := s.newTokens(now)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := s.grants.CreateGrant(ctx, port.OAuthGrant{
		ID:         grantID,
		UserID:     code.UserID,
		ClientID:   code.ClientID,
		ClientName: code.ClientName,
		Scope:      code.Scope,
		Resource:   code.Resource,
		CreatedAt:  now,
		LastUsedAt: now,
	}, tokens); err != nil {
		return TokenResponse{}, err
	}
	return tokenResponse(access, refresh, code.Scope), nil
}

func (s *OAuthServer) refresh(ctx context.Context, req TokenRequest, ep OAuthEndpoints) (TokenResponse, error) {
	if req.RefreshToken == "" {
		return TokenResponse{}, oauthError(OAuthInvalidRequest, "refresh_token is required")
	}

	now := s.now()
	used, err := s.grants.UseRefreshToken(ctx, hashOAuthSecret(req.RefreshToken), now)
	if err != nil {
		return TokenResponse{}, err
	}
	if used == nil {
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "refresh_token is unknown or expired")
	}
	if used.Replayed {
		// 入れ替えたあとの古い値が来た。どちらかが漏れた値なので、許可ごと
		// 失効させる（OAuth 2.1 4.3.1）。
		if err := s.grants.DeleteGrant(ctx, used.Grant.ID); err != nil {
			return TokenResponse{}, err
		}
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "refresh_token was already used")
	}
	if used.Grant.ClientID != req.ClientID {
		return TokenResponse{}, oauthError(OAuthInvalidGrant, "refresh_token was issued to another client")
	}
	if used.Grant.Resource != ep.Resource {
		return TokenResponse{}, oauthError(OAuthInvalidTarget, "refresh_token was issued for another resource")
	}

	access, refresh, tokens, err := s.newTokens(now)
	if err != nil {
		return TokenResponse{}, err
	}
	if err := s.grants.AddTokens(ctx, used.Grant.ID, tokens, now); err != nil {
		if errors.Is(err, port.ErrNotFound) {
			// 使ってから足すまでのあいだに取り消された。
			return TokenResponse{}, oauthError(OAuthInvalidGrant, "grant was revoked")
		}
		return TokenResponse{}, err
	}
	return tokenResponse(access, refresh, used.Grant.Scope), nil
}

// newTokens はアクセストークンと refresh token の組を作る。
func (s *OAuthServer) newTokens(now time.Time) (string, string, []port.OAuthToken, error) {
	access, err := s.random()
	if err != nil {
		return "", "", nil, err
	}
	refresh, err := s.random()
	if err != nil {
		return "", "", nil, err
	}
	return access, refresh, []port.OAuthToken{
		{TokenHash: hashOAuthSecret(access), Kind: port.OAuthAccessToken,
			CreatedAt: now, ExpiresAt: now.Add(AccessTokenTTL)},
		{TokenHash: hashOAuthSecret(refresh), Kind: port.OAuthRefreshToken,
			CreatedAt: now, ExpiresAt: now.Add(RefreshTokenTTL)},
	}, nil
}

func tokenResponse(access, refresh, scope string) TokenResponse {
	return TokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
		Scope:        scope,
	}
}

// verifyPKCE は verifier が challenge（S256）と一致するかを返す。
func verifyPKCE(verifier, challenge string) bool {
	// RFC 7636 4.1 の文字種と長さ。外れたものは一致させない。
	if !isPKCEVerifier(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func isPKCEVerifier(v string) bool {
	if len(v) < 43 || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if !isUnreserved(r) {
			return false
		}
	}
	return true
}

func isUnreserved(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
		r == '-' || r == '.' || r == '_' || r == '~'
}

func isBase64URL(v string, minLen, maxLen int) bool {
	if len(v) < minLen || len(v) > maxLen {
		return false
	}
	for _, r := range v {
		if !isBase64URLRune(r) {
			return false
		}
	}
	return true
}

func isBase64URLRune(r rune) bool {
	return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
		r == '-' || r == '_'
}

// Verify は `/mcp` に届いたアクセストークンを検証する。通らなければ nil。
//
// **resource も見る。** 別の Host で発行されたトークンは、その Host の `/mcp`
// にしか使わせない（RFC 8707）。
func (s *OAuthServer) Verify(ctx context.Context, token string, ep OAuthEndpoints) (*VerifiedToken, error) {
	if token == "" {
		return nil, nil
	}
	found, err := s.grants.FindToken(ctx, hashOAuthSecret(token), s.now())
	if err != nil {
		return nil, err
	}
	if found == nil || found.Token.Kind != port.OAuthAccessToken ||
		found.Grant.Resource != ep.Resource {
		return nil, nil
	}
	return &VerifiedToken{
		UserID:    found.Grant.UserID,
		Scopes:    strings.Fields(found.Grant.Scope),
		ExpiresAt: found.Token.ExpiresAt,
	}, nil
}

// Grants は利用者が許した許可を新しい順に返す。
func (s *OAuthServer) Grants(ctx context.Context, userID string) ([]port.OAuthGrant, error) {
	return s.grants.ListGrants(ctx, userID)
}

// Revoke は利用者の許可を取り消す。
//
// **他人の許可は「見つからない」にする。** 403 にすると、ID を総当たりして
// 他人の許可の存在を確かめられる（ボードの 404 と同じ考え、ADR 0017）。
func (s *OAuthServer) Revoke(ctx context.Context, userID, grantID string) error {
	g, err := s.grants.FindGrant(ctx, grantID)
	if err != nil {
		return err
	}
	if g == nil || g.UserID != userID {
		return fmt.Errorf("%w: oauth grant %s", port.ErrNotFound, grantID)
	}
	return s.grants.DeleteGrant(ctx, grantID)
}
