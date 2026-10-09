// Package cimd は Client ID Metadata Document（MCP 2025-11-25）を取りに行く。
//
// client_id の URL は他人が決める値で、etoki はそれを取りに行く。何もしなければ
// SSRF の入口になる。ここで行き先を絞る（ADR 0076）。
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
)

const (
	// maxDocumentBytes は文書の大きさの上限。MCP の仕様が勧める値。
	maxDocumentBytes = 5 << 10
	// fetchTimeout は 1 回の取得にかける上限。同意の画面を開くたびに走るので、
	// 相手が遅いときに画面を待たせ続けない。
	fetchTimeout = 5 * time.Second
	// maxCacheAge はキャッシュの上限。相手が長い max-age を返しても、名前や
	// 戻り先の変更がこれより長く反映されないことはない。
	maxCacheAge = time.Hour
	// maxCacheEntries はキャッシュに持つ文書の数の上限。同意の画面を開いた
	// 回数だけ育たないようにする。
	maxCacheEntries = 256
)

// errBlockedAddress は繋がない先に繋ごうとしたことを表す。
var errBlockedAddress = errors.New("cimd: refusing to connect to a non-public address")

// Fetcher は usecase.ClientMetadataFetcher の実装。
type Fetcher struct {
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	meta    usecase.ClientMetadata
	expires time.Time
}

var _ usecase.ClientMetadataFetcher = (*Fetcher)(nil)

// New は Fetcher を作る。
func New() *Fetcher {
	return newFetcher(isBlocked, nil)
}

// newFetcher は行き先の判定と TLS の設定を差し替えて作る。テストが
// httptest のサーバー（ループバック）に繋ぐのに使う。
func newFetcher(blocked func(netip.Addr) bool, transport *http.Transport) *Fetcher {
	dialer := &net.Dialer{
		Timeout: fetchTimeout,
		// **繋ぐ直前のアドレスで判定する。** 名前を引いて判定してから繋ぐと、
		// 2 回目の名前解決で別のアドレスを返されて（DNS リバインディング）
		// 判定をすり抜ける。Control は解決済みのアドレスで呼ばれる。
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("%w: %s", errBlockedAddress, address)
			}
			if blocked(ap.Addr()) {
				return fmt.Errorf("%w: %s", errBlockedAddress, ap.Addr())
			}
			return nil
		},
	}

	if transport == nil {
		transport = &http.Transport{}
	}
	transport.DialContext = dialer.DialContext
	// **プロキシは使わない。** プロキシを通すと繋ぐ先はプロキシになり、上の
	// 判定は相手のアドレスを見られなくなる。プロキシが要る環境では、CIMD で
	// 名乗るクライアントは使えない（動的登録は使える）。
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = fetchTimeout

	return &Fetcher{
		client: &http.Client{
			Transport: transport,
			Timeout:   fetchTimeout,
			// **リダイレクトは追わない。** 追うと、https の公開アドレスで受けて
			// 内側の http へ送り直す形で、上の判定の外へ出られる。
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		now:   time.Now,
		cache: map[string]cacheEntry{},
	}
}

// isBlocked は繋がないアドレスかを返す。
//
// 公開されたユニキャストだけを通す。ループバック・プライベート・リンクローカル
// （クラウドのメタデータの 169.254.169.254 を含む）・CGNAT・未指定・マルチ
// キャストは繋がない。IPv4 射影の IPv6 は IPv4 として見る。
func isBlocked(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsUnspecified() ||
		a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsInterfaceLocalMulticast() ||
		a.IsMulticast() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// blockedPrefixes は netip の判定に無い、繋がない範囲。
var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),     // 「この網」
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF プロトコル割り当て
	netip.MustParsePrefix("198.18.0.0/15"), // ベンチマーク
	netip.MustParsePrefix("240.0.0.0/4"),   // 予約
	netip.MustParsePrefix("64:ff9b::/96"),  // NAT64（内側の IPv4 を指しうる）
	netip.MustParsePrefix("2002::/16"),     // 6to4（内側の IPv4 を指しうる）
}

// document は文書のうち etoki が読む項目。
type document struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// Fetch は client_id の URL から文書を取る。中身の検証は usecase が行う。
func (f *Fetcher) Fetch(ctx context.Context, clientID string) (usecase.ClientMetadata, error) {
	u, err := url.Parse(clientID)
	if err != nil || u.Scheme != "https" {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: client_id must be an https URL")
	}

	if meta, ok := f.cached(clientID); ok {
		return meta, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: fetch %s: %w", clientID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: fetch %s: status %d", clientID, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDocumentBytes+1))
	if err != nil {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: read %s: %w", clientID, err)
	}
	if len(body) > maxDocumentBytes {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: %s is larger than %d bytes",
			clientID, maxDocumentBytes)
	}

	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return usecase.ClientMetadata{}, fmt.Errorf("cimd: decode %s: %w", clientID, err)
	}
	meta := usecase.ClientMetadata{
		ClientID:                doc.ClientID,
		ClientName:              doc.ClientName,
		RedirectURIs:            doc.RedirectURIs,
		TokenEndpointAuthMethod: doc.TokenEndpointAuthMethod,
		GrantTypes:              doc.GrantTypes,
		ResponseTypes:           doc.ResponseTypes,
	}

	if age := cacheAge(resp.Header.Get("Cache-Control")); age > 0 {
		f.store(clientID, meta, age)
	}
	return meta, nil
}

func (f *Fetcher) cached(clientID string) (usecase.ClientMetadata, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.cache[clientID]
	if !ok || !f.now().Before(e.expires) {
		return usecase.ClientMetadata{}, false
	}
	return e.meta, true
}

func (f *Fetcher) store(clientID string, meta usecase.ClientMetadata, age time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now()
	if len(f.cache) >= maxCacheEntries {
		for k, e := range f.cache {
			if !now.Before(e.expires) {
				delete(f.cache, k)
			}
		}
		// 期限内のものだけで埋まっていたら、まとめて捨てる。取り直すだけで
		// 正しさは変わらない。
		if len(f.cache) >= maxCacheEntries {
			clear(f.cache)
		}
	}
	f.cache[clientID] = cacheEntry{meta: meta, expires: now.Add(age)}
}

// cacheAge は Cache-Control から持ってよい長さを返す。0 は「持たない」。
//
// **指示が無ければ持たない。** 推測で持つと、相手が直した戻り先が反映されない
// 理由が etoki の側に生まれる。
func cacheAge(header string) time.Duration {
	var age time.Duration
	for _, d := range strings.Split(header, ",") {
		d = strings.TrimSpace(strings.ToLower(d))
		switch {
		case d == "no-store" || d == "no-cache":
			return 0
		case strings.HasPrefix(d, "max-age="):
			n, err := strconv.Atoi(strings.TrimPrefix(d, "max-age="))
			if err != nil || n <= 0 {
				return 0
			}
			age = time.Duration(n) * time.Second
		}
	}
	return min(age, maxCacheAge)
}
