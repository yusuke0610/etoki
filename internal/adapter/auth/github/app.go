// Package github は GitHub App の Web Flow で port.IdentityProvider を実装する。
//
// OAuth App ではなく GitHub App を使う。PAT に求めている粒度（repo の read と
// Projects の read/write）をそのまま要求できるのは GitHub App だけで、OAuth App
// では全リポジトリの読み書きを預かることになる（ADR 0015）。
//
// これは既定実装であって前提ではない。別の基盤に載せ替えるなら
// port.IdentityProvider を自前で実装して etoki.New に渡す。
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// ProviderName は Identity.Provider に入る値。
const ProviderName = "github"

// 既定のホスト。GHE では差し替える。
const (
	// DefaultAuthBaseURL は認可とトークン交換のホスト。api.github.com ではない。
	DefaultAuthBaseURL = "https://github.com"
	// DefaultAPIBaseURL は利用者情報を引く REST のホスト。
	DefaultAPIBaseURL = "https://api.github.com"
)

// 環境変数名。
const (
	EnvClientID     = "ETOKI_GITHUB_APP_CLIENT_ID"
	EnvClientSecret = "ETOKI_GITHUB_APP_CLIENT_SECRET"
)

// defaultTimeout は 1 回の呼び出しを待つ上限。
const defaultTimeout = 30 * time.Second

// maxResponseBytes は応答ボディを読む上限。
const maxResponseBytes = 1 << 20 // 1 MiB

// ErrClientCredentials は client_id / client_secret が揃っていないことを表す。
var ErrClientCredentials = errors.New("etoki: github app client id and secret are required")

// Config は Provider の設定。
type Config struct {
	// ClientID と ClientSecret は GitHub App のもの。どちらも必須。
	ClientID     string
	ClientSecret string
	// AuthBaseURL は認可とトークン交換のホスト。空なら DefaultAuthBaseURL。
	AuthBaseURL string
	// APIBaseURL は利用者情報を引くホスト。空なら DefaultAPIBaseURL。
	APIBaseURL string
	// HTTPClient は差し替え用。nil なら既定のタイムアウトを持つものを作る。
	HTTPClient *http.Client
}

// ConfigFromEnv は ETOKI_GITHUB_APP_* から設定を読む。
func ConfigFromEnv() Config {
	return Config{
		ClientID:     os.Getenv(EnvClientID),
		ClientSecret: os.Getenv(EnvClientSecret),
	}
}

// Configured は OAuth を使う設定になっているかを返す。
//
// 片方だけ設定されている状態は「使うつもりで書き損じた」とみなす。New が
// エラーにするので、起動時に気づける。
func (c Config) Configured() bool {
	return c.ClientID != "" || c.ClientSecret != ""
}

// Provider は GitHub App の Web Flow を実装する。
type Provider struct {
	clientID     string
	clientSecret string
	authBase     string
	apiBase      string
	http         *http.Client
}

var _ port.IdentityProvider = (*Provider)(nil)

// New は Config を検証して Provider を作る。
func New(cfg Config) (*Provider, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("%w: set %s and %s", ErrClientCredentials, EnvClientID, EnvClientSecret)
	}

	p := &Provider{
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		authBase:     strings.TrimRight(orDefault(cfg.AuthBaseURL, DefaultAuthBaseURL), "/"),
		apiBase:      strings.TrimRight(orDefault(cfg.APIBaseURL, DefaultAPIBaseURL), "/"),
		http:         cfg.HTTPClient,
	}
	if p.http == nil {
		p.http = &http.Client{Timeout: defaultTimeout}
	}

	return p, nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// Name は Identity.Provider に入る値を返す。
func (p *Provider) Name() string { return ProviderName }

// AuthorizeURL は利用者を送り出す先を返す。
//
// scope は載せない。GitHub App の権限は App の設定で決まり、認可のたびに
// 要求するものではない。OAuth App との一番大きな違い。
func (p *Provider) AuthorizeURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":    {p.clientID},
		"state":        {state},
		"redirect_uri": {redirectURI},
	}
	return p.authBase + "/login/oauth/authorize?" + q.Encode()
}

// Exchange はコールバックの code を利用者と資格情報に引き換える。
func (p *Provider) Exchange(
	ctx context.Context, code, redirectURI string,
) (port.Identity, port.Credentials, error) {
	if code == "" {
		return port.Identity{}, port.Credentials{}, errors.New("github auth: code is required")
	}

	creds, err := p.token(ctx, url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		return port.Identity{}, port.Credentials{}, err
	}

	id, err := p.viewer(ctx, creds.AccessToken)
	if err != nil {
		return port.Identity{}, port.Credentials{}, err
	}

	return id, creds, nil
}

// Refresh は資格情報を更新する。
//
// GitHub は使った refresh token を無効にする。呼び出し側は利用者ごとに
// 直列化する必要がある（ADR 0015）。
func (p *Provider) Refresh(ctx context.Context, refreshToken string) (port.Credentials, error) {
	if refreshToken == "" {
		return port.Credentials{}, fmt.Errorf("%w: no refresh token", port.ErrNotAuthenticated)
	}

	return p.token(ctx, url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

// tokenResponse はトークン交換の応答。
//
// 失敗も HTTP 200 で返り、error フィールドに入る。ステータスコードだけを見て
// 成功と判断すると、空のトークンで先に進んでしまう。
type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	TokenType             string `json:"token_type"`

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// token はトークンの発行・更新の共通部分。
func (p *Provider) token(ctx context.Context, form url.Values) (port.Credentials, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.authBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return port.Credentials{}, fmt.Errorf("github auth: build request: %w", err)
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")

	var out tokenResponse
	if err := p.do(req, &out); err != nil {
		return port.Credentials{}, err
	}

	// GitHub は失効した refresh token にも 200 を返し、error に入れてくる。
	if out.Error != "" {
		if out.Error == "bad_refresh_token" || out.Error == "bad_verification_code" {
			// 再ログインで直る種類。UI が案内できるよう sentinel に寄せる。
			return port.Credentials{}, fmt.Errorf("%w: github: %s", port.ErrNotAuthenticated, out.Error)
		}
		return port.Credentials{}, fmt.Errorf("github auth: %s: %s", out.Error, out.ErrorDescription)
	}
	if out.AccessToken == "" {
		return port.Credentials{}, errors.New("github auth: response had no access token")
	}

	// 「Expire user authorization tokens」を切った App では expires_in と
	// refresh_token が返らない。その場合は失効しないものとして扱う。
	now := time.Now()
	creds := port.Credentials{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken}
	if out.ExpiresIn > 0 {
		creds.ExpiresAt = now.Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	if out.RefreshTokenExpiresIn > 0 {
		creds.RefreshExpiresAt = now.Add(time.Duration(out.RefreshTokenExpiresIn) * time.Second)
	}

	return creds, nil
}

// viewerResponse は GET /user の応答のうち必要なぶん。
type viewerResponse struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// viewer はトークンの持ち主を引く。
//
// id を Subject にする。login は改名で変わるので同定には使えない。
func (p *Provider) viewer(ctx context.Context, token string) (port.Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.apiBase+"/user", nil)
	if err != nil {
		return port.Identity{}, fmt.Errorf("github auth: build request: %w", err)
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("authorization", "Bearer "+token)

	var out viewerResponse
	if err := p.do(req, &out); err != nil {
		return port.Identity{}, err
	}
	if out.ID == 0 {
		return port.Identity{}, errors.New("github auth: viewer had no id")
	}

	name := out.Name
	if name == "" {
		name = out.Login
	}

	return port.Identity{
		Provider:    ProviderName,
		Subject:     fmt.Sprintf("%d", out.ID),
		Login:       out.Login,
		DisplayName: name,
	}, nil
}

// do はリクエストを 1 回投げ、JSON を out に詰める。
func (p *Provider) do(req *http.Request, out any) error {
	resp, err := p.http.Do(req)
	if err != nil {
		// url.Error は URL を含むが、資格情報はヘッダとボディにしかない。
		return fmt.Errorf("github auth: call %s: %w", req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("github auth: read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: github returned 401", port.ErrNotAuthenticated)
	}
	if resp.StatusCode != http.StatusOK {
		// 本文をそのまま載せない。client_secret を送った先の応答なので、
		// 何が返るか読み切れない。
		return fmt.Errorf("github auth: %s returned %d", req.URL.Path, resp.StatusCode)
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("github auth: decode response: %w", err)
	}

	return nil
}
