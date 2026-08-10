package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// sessionCookie はセッション token を載せる cookie の名前。
const sessionCookie = "etoki_session"

// callbackPath はコールバックのパス。redirect_uri の組み立てにも使う。
const callbackPath = "/api/auth/callback"

// authNotConfigured は認証未設定のときの案内。
const authNotConfigured = "authentication is not configured: " +
	"set ETOKI_GITHUB_APP_CLIENT_ID and ETOKI_GITHUB_APP_CLIENT_SECRET"

// getSession はログイン状態を返す。
//
// 常に 200。未ログインを 401 で伝えると、認証を設定していない構成の起動時にも
// 401 が出て、本当の失効と見分けがつかなくなる（ADR 0015）。
func (h *handlers) getSession(c *gin.Context) {
	out := apitypes.SessionStatus{AuthRequired: h.auth != nil}
	if h.auth == nil {
		c.JSON(http.StatusOK, out)
		return
	}

	// ミドルウェアが解決済みのものを使う。ここで引き直すと 2 回問い合わせる。
	if user, ok := currentUser(c); ok {
		out.Authenticated = true
		out.User = &apitypes.AuthUser{
			Provider:    user.Provider,
			Login:       user.Login,
			DisplayName: user.DisplayName,
		}
	}

	c.JSON(http.StatusOK, out)
}

// startLogin は state を発行し、認可画面の URL を返す。
//
// POST なのは state の発行が書き込みだから。GET にすると外部ページの <img> から
// 叩けてしまう。POST なら Origin 検証が効く（ADR 0013 / 0015）。
func (h *handlers) startLogin(c *gin.Context) {
	if h.auth == nil {
		errorJSON(c, http.StatusServiceUnavailable, authNotConfigured)
		return
	}

	authorizeURL, err := h.auth.Start(c.Request.Context(), h.redirectURI(c))
	if err != nil {
		h.fail(c, err)
		return
	}

	c.JSON(http.StatusOK, apitypes.LoginResponse{AuthorizeURL: authorizeURL})
}

// completeLogin は認可の結果を受け取ってセッションを張る。
//
// **etoki で唯一、副作用を持つ GET。** 認証基盤からのトップレベル遷移なので
// GET 以外にできない。守るのは Origin ではなく state で、サーバー発行・単回
// 使用・期限つきなので攻撃者は有効な値を用意できない（ADR 0015）。
func (h *handlers) completeLogin(c *gin.Context) {
	if h.auth == nil {
		errorJSON(c, http.StatusServiceUnavailable, authNotConfigured)
		return
	}

	code, state := c.Query("code"), c.Query("state")
	if code == "" || state == "" {
		// 認可からの正しい遷移なら必ず両方付く。無いのは誰かが直接叩いた場合。
		h.badRequest(c, errors.New("code and state are required"))
		return
	}

	token, _, err := h.auth.Complete(c.Request.Context(), code, state, h.redirectURI(c))
	if err != nil {
		h.failAuth(c, err)
		return
	}

	h.setSessionCookie(c, token)

	// 画面に戻す。認可の code と state を URL に残したままにしない。
	c.Redirect(http.StatusFound, "/")
}

// logout はセッションを破棄する。
func (h *handlers) logout(c *gin.Context) {
	if h.auth == nil {
		c.Status(http.StatusNoContent)
		return
	}

	token, _ := c.Cookie(sessionCookie)
	if err := h.auth.Logout(c.Request.Context(), token); err != nil {
		h.fail(c, err)
		return
	}

	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

// redirectURI は認可から戻ってくる先を組み立てる。
//
// 設定があればそれを使う。無ければリクエストの Host から組む。make dev では
// ブラウザが :5173 にいて Vite が Host を書き換えずに転送するので、これで
// 正しい値になる。
func (h *handlers) redirectURI(c *gin.Context) string {
	if h.publicURL != "" {
		return strings.TrimRight(h.publicURL, "/") + callbackPath
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: c.Request.Host, Path: callbackPath}).String()
}

// secureCookie は cookie に Secure を付けるかどうかを返す。
//
// `c.Request.TLS` だけを見ると、TLS を終端するリバースプロキシの背後で常に
// nil になる。https で配信しているのにセッション cookie が平文で載る、という
// 一番まずい取りこぼしがそこで起きる。公開 URL のスキームも見て補う。
//
// 既定の構成（http://127.0.0.1）はどちらも偽なので、Secure は付かない。
// ここで常に付けると、一部のブラウザが cookie を保存せず、原因の分からない
// ログイン失敗になる。
func (h *handlers) secureCookie(c *gin.Context) bool {
	return c.Request.TLS != nil || strings.HasPrefix(h.publicURL, "https://")
}

// setSessionCookie はセッション token を cookie に載せる。
func (h *handlers) setSessionCookie(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(usecase.SessionTTL.Seconds()),
		HttpOnly: true,
		// Strict にすると GitHub からの戻りで送られず、初回だけ未ログインに
		// 見える。Lax はトップレベル GET では送られるので、これでよい。
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookie(c),
	})
}

func (h *handlers) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   h.secureCookie(c),
	})
}

// failAuth はログインの失敗を HTTP ステータスに写す。
func (h *handlers) failAuth(c *gin.Context, err error) {
	switch {
	case errors.Is(err, usecase.ErrInvalidInput), errors.Is(err, port.ErrNotAuthenticated):
		// state が古い、または code を使い切った。攻撃かもしれないので記録する
		// が、正規の利用者が認可画面を開いたまま放置しただけでも起きる。
		h.logger.WarnContext(c.Request.Context(), "login callback rejected",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))

		// 画面に戻す。ブラウザのトップレベル遷移に JSON を返すと、利用者は
		// 生のエラー本文を見ることになる。戻せばログイン画面が出るので、
		// やり直しの導線もそのまま繋がる。
		c.Redirect(http.StatusFound, "/")

	default:
		h.fail(c, err)
	}
}

// userContextKey は解決済みの利用者を gin.Context に置く鍵。
const userContextKey = "etoki_user"

// currentUser はミドルウェアが解決した利用者を返す。
func currentUser(c *gin.Context) (port.User, bool) {
	value, ok := c.Get(userContextKey)
	if !ok {
		return port.User{}, false
	}
	user, ok := value.(port.User)
	return user, ok
}

// resolveSession はセッションを解決して、利用者を context に載せる。
//
// **ここでは弾かない。** 未ログインでも通し、認証が要るハンドラだけが
// requireAuth で止める。/api/auth/session は未ログインでも 200 を返す必要が
// あり、ここで弾くとそれが書けない。
func resolveSession(auth *usecase.AuthService, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth == nil {
			c.Next()
			return
		}

		token, err := c.Cookie(sessionCookie)
		if err != nil || token == "" {
			c.Next()
			return
		}

		user, err := auth.Resolve(c.Request.Context(), token)
		if err != nil {
			// 解決に失敗しただけで全部を止めない。未ログインとして進める。
			// DB が壊れているなら、後続のハンドラが 500 として返す。
			logger.ErrorContext(c.Request.Context(), "resolve session failed",
				slog.String("path", c.Request.URL.Path), slog.Any("error", err))
			c.Next()
			return
		}
		if user == nil {
			c.Next()
			return
		}

		c.Set(userContextKey, *user)

		// 下流（port.GitHubTokenSource）は context から利用者を引く。
		// gin.Context ではなく Request の context に載せる必要がある。
		ctx := port.ContextWithUserID(c.Request.Context(), user.ID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// requireAuth は未ログインを 401 で止める。
//
// 認証を設定していなければ素通しする。設定していない構成の挙動を変えない
// のが PR の前提（ADR 0015）。
func requireAuth(auth *usecase.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if auth == nil {
			c.Next()
			return
		}

		if _, ok := currentUser(c); !ok {
			c.Abort()
			errorJSON(c, http.StatusUnauthorized, "login required")
			return
		}

		c.Next()
	}
}
