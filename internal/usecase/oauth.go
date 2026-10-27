package usecase

import (
	"context"
	"fmt"
	"sync"
	"time"

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
