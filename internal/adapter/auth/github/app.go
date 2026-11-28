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
	"strconv"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// ProviderName は Identity.Provider に入る値。
const ProviderName = "github"

// 既定のホスト。差し替えるのはテストだけ。**GHES に向けられるとは言わない**
// （ADR 0050）。ConfigFromEnv はこれを読まず、GHES では確かめていない。
const (
	// DefaultAuthBaseURL は認可とトークン交換のホスト。api.github.com ではない。
	DefaultAuthBaseURL = "https://github.com"
	// DefaultAPIBaseURL は利用者情報を引く REST のホスト。
	DefaultAPIBaseURL = "https://api.github.com"
)

// 環境変数名。
const (
	EnvClientID     = "ETOKI_GITHUB_APP_CLIENT_ID"
	EnvClientSecret = "ETOKI_GITHUB_APP_CLIENT_SECRET" //nolint:gosec // 環境変数の名前で、資格情報の値ではない（G101）
	// EnvAllowedUserID はログインを通す 1 人の、GitHub の数値の利用者 ID（#492）。
	EnvAllowedUserID = "ETOKI_ALLOWED_GITHUB_USER_ID"
)

// defaultTimeout は 1 回の呼び出しを待つ上限。
const defaultTimeout = 30 * time.Second

// maxResponseBytes は応答ボディを読む上限。
const maxResponseBytes = 1 << 20 // 1 MiB

// ErrClientCredentials は client_id / client_secret が揃っていないことを表す。
var ErrClientCredentials = errors.New("etoki: github app client id and secret are required")

// ErrAllowedUserID はログインを通す利用者が設定されていないか、形が違うことを表す。
var ErrAllowedUserID = errors.New("etoki: the github user allowed to sign in is not set correctly")

// Config は Provider の設定。
type Config struct {
	// ClientID と ClientSecret は GitHub App のもの。どちらも必須。
	ClientID     string
	ClientSecret string
	// AllowedUserID はログインを通す 1 人の、GitHub の数値の利用者 ID。必須。
	//
	// **login ではなく数値の ID で持つ。** login は改名で変わり、空いた login は
	// 別人が取れる（ADR 0053）。照らす相手は Identity.Subject と同じ形の文字列。
	AllowedUserID string
	// AuthBaseURL は認可とトークン交換のホスト。空なら DefaultAuthBaseURL。
	AuthBaseURL string
	// APIBaseURL は利用者情報を引くホスト。空なら DefaultAPIBaseURL。
	APIBaseURL string
	// HTTPClient は差し替え用。nil なら既定のタイムアウトを持つものを作る。
	HTTPClient *http.Client
}

// ConfigFromEnv は ETOKI_GITHUB_APP_* と ETOKI_ALLOWED_GITHUB_USER_ID から設定を読む。
func ConfigFromEnv() Config {
	return Config{
		ClientID:      os.Getenv(EnvClientID),
		ClientSecret:  os.Getenv(EnvClientSecret),
		AllowedUserID: os.Getenv(EnvAllowedUserID),
	}
}

// Configured は OAuth を使う設定になっているかを返す。
//
// どれか 1 つでも設定されている状態は「使うつもりで書き損じた」とみなす。New が
// エラーにするので、起動時に気づける。**通す利用者だけを書いた状態も含める。**
// 含めないと、認証の設定が抜けたことを、通す相手を決めたつもりのまま見落とす。
func (c Config) Configured() bool {
	return c.ClientID != "" || c.ClientSecret != "" || c.AllowedUserID != ""
}

// Provider は GitHub App の Web Flow を実装する。
type Provider struct {
	clientID       string
	clientSecret   string
	allowedSubject string
	authBase       string
	apiBase        string
	http           *http.Client
}

var _ port.IdentityProvider = (*Provider)(nil)

// New は Config を検証して Provider を作る。
func New(cfg Config) (*Provider, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, fmt.Errorf("%w: set %s and %s", ErrClientCredentials, EnvClientID, EnvClientSecret)
	}
	if err := checkUserID(cfg.AllowedUserID); err != nil {
		return nil, err
	}

	p := &Provider{
		clientID:       cfg.ClientID,
		clientSecret:   cfg.ClientSecret,
		allowedSubject: cfg.AllowedUserID,
		authBase:       strings.TrimRight(orDefault(cfg.AuthBaseURL, DefaultAuthBaseURL), "/"),
		apiBase:        strings.TrimRight(orDefault(cfg.APIBaseURL, DefaultAPIBaseURL), "/"),
		http:           cfg.HTTPClient,
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

// checkUserID は通す利用者の ID が、Identity.Subject と同じ形かを見る。
//
// **形の違う値を直さずに弾く。** 照らすのは文字列の一致なので、先頭の 0 や空白を
// 残したまま受けると、誰もログインできないまま起動する。黙って直すと、書いた値と
// 照らしている値が食い違っていることに気づけない。形は viewer が作る `%d`。
func checkUserID(raw string) error {
	if raw == "" {
		return fmt.Errorf("%w: set %s to your numeric github user id", ErrAllowedUserID, EnvAllowedUserID)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != raw {
		return fmt.Errorf("%w: %s must be a numeric github user id, got %q",
			ErrAllowedUserID, EnvAllowedUserID, raw)
	}
	return nil
}

// Name は Identity.Provider に入る値を返す。
func (p *Provider) Name() string { return ProviderName }

// AllowedSubject はログインを通す利用者の ID を返す。New が形を確かめたもの。
//
// **照らすのはユースケース層**（etoki.NewAuthenticator に渡す）。Exchange で断る
// 形にしない。すでにあるセッションと MCP のトークンも同じ相手で照らすので、
// 照らす場所を 1 つにしておく。
func (p *Provider) AllowedSubject() string { return p.allowedSubject }

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

// Exchange はコールバックの code を利用者に引き換える。
//
// 引き換えで得た user-to-server トークンは、持ち主を引く（GET /user）のに
// 1 回使って捨てる。GitHub を叩く資格情報は PAT で、ログインとは別（ADR 0106）。
func (p *Provider) Exchange(ctx context.Context, code, redirectURI string) (port.Identity, error) {
	if code == "" {
		return port.Identity{}, errors.New("github auth: code is required")
	}

	token, err := p.token(ctx, url.Values{
		"client_id":     {p.clientID},
		"client_secret": {p.clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		return port.Identity{}, err
	}

	return p.viewer(ctx, token)
}

// tokenResponse はトークン交換の応答。
//
// 失敗も HTTP 200 で返り、error フィールドに入る。ステータスコードだけを見て
// 成功と判断すると、空のトークンで先に進んでしまう。
//
// refresh_token と期限も返るが読まない。トークンを保存も更新もしないので
// （ADR 0106）、読む先が無い。
type tokenResponse struct {
	AccessToken string `json:"access_token"`

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// token は code をアクセストークンに引き換える。
func (p *Provider) token(ctx context.Context, form url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.authBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("github auth: build request: %w", err)
	}
	req.Header.Set("content-type", "application/x-www-form-urlencoded")
	req.Header.Set("accept", "application/json")

	var out tokenResponse
	if err := p.do(req, &out); err != nil {
		return "", err
	}

	// GitHub は使い切った・期限の切れた code にも 200 を返し、error に入れてくる。
	if out.Error != "" {
		if out.Error == "bad_verification_code" {
			// もう一度ログインすれば直る種類。UI が案内できるよう sentinel に寄せる。
			return "", fmt.Errorf("%w: github: %s", port.ErrNotAuthenticated, out.Error)
		}
		return "", fmt.Errorf("github auth: %s: %s", out.Error, out.ErrorDescription)
	}
	if out.AccessToken == "" {
		return "", errors.New("github auth: response had no access token")
	}

	return out.AccessToken, nil
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
