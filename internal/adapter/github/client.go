// Package github は port.GitHubClient の実装を提供する。
//
// GraphQL を net/http で直接叩く。GitHub SDK を go.mod に持ち込まないのは、
// コアが特定の基盤を意識しないという方針の帰結（ADR 0001）。
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// DefaultBaseURL は GitHub API のホスト。
const DefaultBaseURL = "https://api.github.com"

// 環境変数名。
const (
	envToken   = "ETOKI_GITHUB_TOKEN"
	envBaseURL = "ETOKI_GITHUB_BASE_URL"
)

// defaultTimeout は 1 回の呼び出しを待つ上限。
const defaultTimeout = 30 * time.Second

// maxResponseBytes は応答ボディを読む上限。
const maxResponseBytes = 10 << 20 // 10 MiB

// ErrTokenRequired はトークンが設定されていないことを表す。
var ErrTokenRequired = errors.New("etoki: github token is required")

// Mode は「使えるリポジトリ」の決まり方。
//
// GitHub App と PAT で定義そのものが違うので、アダプタの中で推測せず外から
// 渡す。判断できるのは、認証をどう設定したかを知っている cmd/etoki だけ
// （ADR 0015）。
type Mode int

const (
	// ModePAT は PAT で叩く構成。利用者が見えるリポジトリすべてが対象。
	ModePAT Mode = iota
	// ModeApp は GitHub App の user-to-server トークンで叩く構成。
	// アプリをインストールしたリポジトリだけが対象。
	ModeApp
)

// Config は Client の設定。
type Config struct {
	// BaseURL は GitHub API のルート。空なら DefaultBaseURL。
	//
	// GraphQL は {BaseURL}/graphql、REST は {BaseURL}{path} に送る。GHES に
	// 向けられるとは言わない。認可側（auth/github）のホストを差し替える口が
	// 無く、GHES で確かめてもいない。
	BaseURL string
	// Token は Authorization ヘッダに載せるトークン。
	//
	// TokenSource を指定するなら空でよい。両方あれば TokenSource が勝つ。
	Token string
	// TokenSource はリクエストごとにトークンを引く。
	//
	// 利用者ごとにトークンが変わる構成（OAuth）で使う。nil なら Token を
	// 固定で使う（ADR 0015）。
	TokenSource port.GitHubTokenSource
	// Mode は「使えるリポジトリ」の決まり方。既定は ModePAT。
	Mode Mode
	// HTTPClient は差し替え用。nil なら既定のタイムアウトを持つものを作る。
	HTTPClient *http.Client
}

// ConfigFromEnv は ETOKI_GITHUB_TOKEN と ETOKI_GITHUB_BASE_URL から設定を読む。
//
// 値の検証は行わない。既定への差し戻しと BaseURL の検証は New が行う。
func ConfigFromEnv() Config {
	return Config{
		Token:   os.Getenv(envToken),
		BaseURL: os.Getenv(envBaseURL),
	}
}

// staticToken は Config.Token をトークン源として扱うためのラッパー。
type staticToken string

func (t staticToken) Token(context.Context) (string, error) { return string(t), nil }

// Client は GitHub Projects v2 を GraphQL で操作する。
type Client struct {
	base   string
	tokens port.GitHubTokenSource
	mode   Mode
	http   *http.Client
}

// New は Config を検証して Client を作る。
//
// BaseURL は起動時に検証する。綴り間違いを実行時まで持ち越すと、作成先の
// 一覧や作成の失敗として現れて原因の切り分けが遠回りになる。
func New(cfg Config) (*Client, error) {
	tokens := cfg.TokenSource
	if tokens == nil {
		if cfg.Token == "" {
			return nil, fmt.Errorf("%w: set Config.Token, Config.TokenSource, or %s",
				ErrTokenRequired, envToken)
		}
		tokens = staticToken(cfg.Token)
	}

	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("etoki: invalid github base url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("etoki: invalid github base url %q: scheme must be http or https", u.Redacted())
	}
	// url.Parse は "http://" や "https:api.github.com" も通す。
	if u.Hostname() == "" {
		return nil, fmt.Errorf("etoki: invalid github base url %q: host is missing", u.Redacted())
	}
	// **http はループバックにだけ許す。** この口を開けたのは手元の偽物に向ける
	// ため（ADR 0050）で、http で届く GitHub は無い。綴りの誤り 1 つで本物の
	// トークンを平文で外へ送らない。
	if u.Scheme == "http" && !isLoopbackHostname(u.Hostname()) {
		return nil, fmt.Errorf("etoki: invalid github base url %q: http is allowed only for loopback", u.Redacted())
	}
	// クエリと fragment は弾く。送り先は base に "/graphql" を足した文字列
	// なので、"?x=1" が付いていると "...?x=1/graphql" になり、足したぶんが
	// path ではなくクエリの一部として飛ぶ。黙って別の URL を叩くより、
	// 起動時に落とす。
	if u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") {
		return nil, fmt.Errorf(
			"etoki: invalid github base url %q: query and fragment are not allowed", u.Redacted())
	}

	c := &Client{
		base:   strings.TrimRight(base, "/"),
		tokens: tokens,
		mode:   cfg.Mode,
		http:   cfg.HTTPClient,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: defaultTimeout}
	}

	return c, nil
}

// isLoopbackHostname はホスト名（ポートを含まない）がループバックを指すかを返す。
//
// httpapi の判定（Host ヘッダの host[:port] を読む）は使わない。アダプタが
// httpapi に依存する向きを作らないため。
func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// nextCursor は次のページのカーソルを返す。次が無ければ (nil, nil)。
//
// カーソルが空、あるいは前回から進んでいないなら、辿り続けても同じページを
// 取り直すだけで終わらない。打ち切ってエラーにする。
func nextCursor(what string, hasNext bool, endCursor string, after *string) (*string, error) {
	if !hasNext {
		return nil, nil
	}
	if endCursor == "" || (after != nil && endCursor == *after) {
		return nil, fmt.Errorf("github: %s pagination did not advance (cursor %q)", what, endCursor)
	}
	return &endCursor, nil
}

// do は GraphQL を 1 回叩き、data を out に詰める。
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: vars})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/graphql", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")

	// トークンはリクエストごとに引く。OAuth では利用者ごとに変わり、失効
	// 間際なら差し替わる（ADR 0015）。
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error は URL を含むが、トークンはヘッダにしか無いので漏れない。
		return fmt.Errorf("call github graphql: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return statusError(resp, raw)
	}

	var envelope graphQLResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// GraphQL は HTTP 200 でもボディに errors を返す。ステータスコードだけを
	// 見て成功と判断すると、何も作られていないのに成功として進んでしまう。
	if len(envelope.Errors) > 0 {
		// 権限不足は HTTP 200 のボディに載って返る。文字列のままだと呼び出し側が
		// 「作れない相手だった」と判断できず、500 に落ちる（ADR 0017）。
		if hasForbidden(envelope.Errors) {
			return fmt.Errorf("%w: github graphql: %s",
				port.ErrForbidden, joinErrors(envelope.Errors))
		}
		return fmt.Errorf("github graphql: %s", joinErrors(envelope.Errors))
	}
	if len(envelope.Data) == 0 {
		return errors.New("github graphql: response had no data")
	}

	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}

	return nil
}

// statusError は 2xx 以外の応答をエラーにする。
//
// レート制限は原因が分かるようリセット時刻を添える。トークンはヘッダにしか
// 載せていないので、応答をそのまま含めても漏れない。
func statusError(resp *http.Response, raw []byte) error {
	var msg struct {
		Message string `json:"message"`
	}
	detail := truncate(string(raw), 200)
	if err := json.Unmarshal(raw, &msg); err == nil && msg.Message != "" {
		detail = msg.Message
	}

	// 401 は sentinel に寄せる。文字列のままだと UI が「再ログインが要る」と
	// 判断できない（ADR 0015）。ステータスは他と揃えて残す。
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w: github api: %d: %s", port.ErrNotAuthenticated, resp.StatusCode, detail)
	}

	// **レート制限の判定を 403 より先に置く。** GitHub はレート制限も 403 で
	// 返すが、それは権限の問題ではない。待てば通るものを「権限がありません」と
	// 見せると、利用者は直しようのない原因を疑うことになる。
	//
	// 二次レート制限は残数を減らさず、retry-after だけを付けて 403 を返す。
	// 残数しか見ていないと、これが権限不足に化ける。
	if after := resp.Header.Get("retry-after"); after != "" {
		return fmt.Errorf("github api: %d: %s (rate limited, retry-after: %s)",
			resp.StatusCode, detail, after)
	}
	if remaining := resp.Header.Get("x-ratelimit-remaining"); remaining == "0" {
		return fmt.Errorf("github api: %d: %s (rate limit resets at %s)",
			resp.StatusCode, detail, resp.Header.Get("x-ratelimit-reset"))
	}

	// 403 も 401 と同じく sentinel に寄せる。ただし分けておく。ログインし直しても
	// 解決しないので、UI の打ち手が違う。招待されただけでリポジトリに権限が無い
	// 利用者はここに来る（ADR 0017）。
	if resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%w: github api: %d: %s", port.ErrForbidden, resp.StatusCode, detail)
	}

	return fmt.Errorf("github api: %d: %s", resp.StatusCode, detail)
}

// hasForbidden は GraphQL のエラーに権限不足が含まれるかを返す。
//
// GitHub は権限で拒むとき type に FORBIDDEN、スコープ不足のときに
// INSUFFICIENT_SCOPES を入れる。前者は相手の権限、後者はトークンの権限で、
// 利用者から見ればどちらも「書けない」。
func hasForbidden(errs []graphQLError) bool {
	for _, e := range errs {
		if e.Type == "FORBIDDEN" || e.Type == "INSUFFICIENT_SCOPES" {
			return true
		}
	}
	return false
}

// joinErrors は GraphQL のエラーを 1 行にまとめる。
func joinErrors(errs []graphQLError) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		if e.Type == "" {
			msgs[i] = e.Message
			continue
		}
		msgs[i] = e.Type + ": " + e.Message
	}
	return strings.Join(msgs, "; ")
}

// truncate は s を n ルーンまでに切る。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// graphQLRequest は GraphQL のリクエストボディ。
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphQLResponse は GraphQL の応答エンベロープ。
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// pageInfo は GraphQL の Relay ページング情報。
type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}
