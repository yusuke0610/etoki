package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// originGuard はブラウザ由来の cross-site リクエストを弾く。
//
// etoki は認証を持たず、127.0.0.1 にバインドすることだけを防御としている
// （ADR 0004）。しかしループバックにバインドしていても、ブラウザ経由なら
// 外部サイトから叩ける。塞ぐのは 2 つ。
//
//   - CSRF: 攻撃者のページから fetch される。Content-Type を text/plain に
//     すればプリフライトが起きず、Gin の ShouldBindJSON は Content-Type を
//     見ずに本文を JSON として読むため、CORS ヘッダを返さないだけでは
//     止まらない。Origin を見て弾く。
//   - DNS リバインディング: 攻撃者のドメインを 127.0.0.1 に向け直すと、
//     ページと etoki が同一オリジンになり CORS の制約が一切かからない。
//     Origin は自分自身になるので止まらない。Host を見て弾く。
type originGuard struct {
	// origins は追加で許すオリジン。正規化済み。
	origins map[string]struct{}
	// hosts は追加で許す Host ヘッダ。origins から導く。
	hosts map[string]struct{}
}

func newOriginGuard(allowed []string) originGuard {
	g := originGuard{
		origins: make(map[string]struct{}, len(allowed)),
		hosts:   make(map[string]struct{}, len(allowed)),
	}

	for _, raw := range allowed {
		origin := normalizeOrigin(raw)
		if origin == "" {
			continue
		}
		g.origins[origin] = struct{}{}

		// Host ヘッダには scheme が付かない。同じ設定で両方を許すために、
		// オリジンから host[:port] を取り出しておく。利用者に 2 つの環境変数を
		// 書き分けさせると、片方だけ足して 403 のままになる。
		if u, err := url.Parse(origin); err == nil && u.Host != "" {
			g.hosts[strings.ToLower(u.Host)] = struct{}{}
		}
	}

	return g
}

// normalizeOrigin は比較できる形に揃える。末尾のスラッシュとパスは捨てる。
func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// isLoopbackHost は host[:port] がループバックを指すかを返す。
//
// ポートは見ない。make dev では Vite の dev サーバー(:5173)が Host と Origin を
// そのまま転送するため、リッスンポートに絞ると開発時に落ちる。ポートを問わない
// ことで別のローカルアプリからも叩けるようになるが、利用者の端末で任意の
// サーバーを立てられる相手には、そもそもこの防御が意味を持たない。
func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}

	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	hostname = strings.ToLower(strings.Trim(hostname, "[]"))

	if hostname == "localhost" {
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func (g originGuard) allowsHost(host string) bool {
	if isLoopbackHost(host) {
		return true
	}
	_, ok := g.hosts[strings.ToLower(host)]
	return ok
}

func (g originGuard) allowsOrigin(origin string) bool {
	// "null" は sandbox された iframe や file:// から来る。ホスト名を持たない
	// ので、ループバック判定にも許可リストにも引っかからない。
	normalized := normalizeOrigin(origin)
	if normalized == "" {
		return false
	}
	if u, err := url.Parse(normalized); err == nil && isLoopbackHost(u.Host) {
		return true
	}
	_, ok := g.origins[normalized]
	return ok
}

// handler は Host と Origin を検証するミドルウェアを返す。
//
// Origin が無いリクエストは通す。ブラウザは cross-origin の POST に必ず
// Origin を付ける一方、curl やスクリプトは付けない。攻撃者のページから
// Origin を省略・偽装することはできないので、この非対称性は安全側に働く。
//
// ただしサブリソース読み込み（<img> など）の GET には Origin が付かない。
// etoki の GET はすべて副作用を持たず、応答も CORS で読めないため許容して
// いる。GET に副作用を持たせるなら、この判断ごと見直すこと（ADR 0013）。
func (g originGuard) handler(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !g.allowsHost(c.Request.Host) {
			g.reject(c, logger, "host is not allowed", slog.String("host", c.Request.Host))
			return
		}

		if origin := c.GetHeader("Origin"); origin != "" && !g.allowsOrigin(origin) {
			g.reject(c, logger, "origin is not allowed", slog.String("origin", origin))
			return
		}

		c.Next()
	}
}

func (g originGuard) reject(c *gin.Context, logger *slog.Logger, msg string, attr slog.Attr) {
	logger.WarnContext(c.Request.Context(), "rejected cross-site request",
		slog.String("path", c.Request.URL.Path),
		slog.String("reason", msg),
		attr,
	)

	// 何を許すかは応答に載せない。攻撃者が許可リストを総当たりで
	// 探れるようにする理由がない。
	c.Abort()
	errorJSON(c, http.StatusForbidden,
		"forbidden: request did not come from a local origin. "+
			"set ETOKI_ALLOWED_ORIGINS if etoki is bound beyond loopback")
}
