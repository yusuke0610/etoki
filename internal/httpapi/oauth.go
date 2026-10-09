package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// MCP のクライアントのための認可サーバーの口（ADR 0076）。
//
// `/oauth/*` と `/.well-known/*` は MCP のクライアントが叩く。形は RFC が決めて
// いて etoki の契約ではないので、`api/openapi.yaml` に載せず、誤りも
// `ErrorResponse` ではなく RFC 6749 の `{error, error_description}` で返す。
// 同意の画面が叩く `/api/oauth/*` は画面の契約なので openapi に載せている。
const (
	oauthAuthorizePath = "/oauth/authorize"
	//nolint:gosec // 口のパスであって資格情報の値ではない（G101）
	oauthTokenPath         = "/oauth/token"
	oauthRegisterPath      = "/oauth/register"
	wellKnownResourcePath  = "/.well-known/oauth-protected-resource"
	wellKnownAuthorization = "/.well-known/oauth-authorization-server"

	// consentQueryKey は同意の画面に要求を運ぶクエリの名前。
	//
	// 要求のクエリを 1 つの値に包んで運ぶ。そのまま並べると、OAuth の
	// state と画面のクエリの名前がぶつかりうる。
	consentQueryKey = "authorize"
)

// baseURL は自分の URL の起点を返す。ログインの redirect_uri と同じ規則で、
// 設定があればそれを使い、無ければリクエストの Host から組む。
//
// Host は originGuard（ADR 0013）がすでに許した値だけが届く。
func (h *handlers) baseURL(c *gin.Context) string {
	if h.publicURL != "" {
		return strings.TrimRight(h.publicURL, "/")
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return (&url.URL{Scheme: scheme, Host: c.Request.Host}).String()
}

// oauthEndpoints はこのリクエストから見た issuer と `/mcp` の URL を返す。
func (h *handlers) oauthEndpoints(c *gin.Context) usecase.OAuthEndpoints {
	base := h.baseURL(c)
	return usecase.OAuthEndpoints{Issuer: base, Resource: base + mcpPath}
}

// oauthUnavailable は認可サーバーを組み立てていないときの応答。
//
// `/mcp` の 503（ADR 0071）と同じく text/plain にする。叩くのは MCP の
// クライアントで、画面のための code（ADR 0034）を読む相手がいない。
func oauthUnavailable(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.String(http.StatusServiceUnavailable,
		"MCP authorization is not available: authentication is not configured\n")
}

// oauthJSON は RFC の口の応答を書く。トークンを含みうるので保存させない
// （RFC 6749 5.1）。
func oauthJSON(c *gin.Context, status int, body any) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.JSON(status, body)
}

// oauthErrorBody は RFC 6749 5.2 の誤りの本文。
type oauthErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// failOAuth は RFC の口の失敗を応答にする。
func (h *handlers) failOAuth(c *gin.Context, err error) {
	var oe *usecase.OAuthError
	switch {
	case errors.As(err, &oe):
		status := http.StatusBadRequest
		if oe.Code == usecase.OAuthInvalidClient {
			// RFC 6749 5.2。公開クライアントなので WWW-Authenticate は付けない。
			status = http.StatusUnauthorized
		}
		oauthJSON(c, status, oauthErrorBody{Error: oe.Code, ErrorDescription: oe.Description})
	case errors.Is(err, usecase.ErrRateLimited):
		oauthJSON(c, http.StatusTooManyRequests, oauthErrorBody{
			Error: "temporarily_unavailable", ErrorDescription: err.Error(),
		})
	default:
		h.logger.ErrorContext(c.Request.Context(), "unhandled error",
			slog.String("path", c.Request.URL.Path), slog.Any("error", err))
		oauthJSON(c, http.StatusInternalServerError, oauthErrorBody{Error: "server_error"})
	}
}

// getProtectedResourceMetadata は `/mcp` の Protected Resource Metadata
// （RFC 9728）を返す。
//
// SDK の auth.ProtectedResourceMetadataHandler は使わない。中身を固定値で
// 受けるので、Host から URL を組む etoki では構成ごとに作れない。
func (h *handlers) getProtectedResourceMetadata(c *gin.Context) {
	if h.oauth == nil {
		oauthUnavailable(c)
		return
	}
	ep := h.oauthEndpoints(c)
	oauthJSON(c, http.StatusOK, gin.H{
		"resource":                 ep.Resource,
		"authorization_servers":    []string{ep.Issuer},
		"scopes_supported":         []string{usecase.OAuthScopeRead},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "etoki",
	})
}

// getAuthorizationServerMetadata は Authorization Server Metadata
// （RFC 8414）を返す。
func (h *handlers) getAuthorizationServerMetadata(c *gin.Context) {
	if h.oauth == nil {
		oauthUnavailable(c)
		return
	}
	issuer := h.oauthEndpoints(c).Issuer
	oauthJSON(c, http.StatusOK, gin.H{
		"issuer":                                issuer,
		"authorization_endpoint":                issuer + oauthAuthorizePath,
		"token_endpoint":                        issuer + oauthTokenPath,
		"registration_endpoint":                 issuer + oauthRegisterPath,
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{usecase.OAuthScopeRead},
		// 認可の応答に iss を載せる（RFC 9207）。取り違えの攻撃（mix-up）を
		// クライアントが見分けられる。
		"authorization_response_iss_parameter_supported": true,
		"client_id_metadata_document_supported":          h.oauthMetadataDocuments,
	})
}

// authorize は認可の要求を同意の画面へ転送する。
//
// **何も書かない。** 副作用を持つ GET は `/api/auth/callback` だけに留める
// （ADR 0013、`.claude/rules/http-handlers.md`）。要求の検証も画面が
// `/api/oauth/authorization` を叩いたときに行う。ここで検証すると、
// Client ID Metadata Document の取得（外向きの通信）を、ログインしていない
// 誰でも起こせることになる。
//
// 転送先は自オリジンの "/" で、要求は 1 つのクエリに包んで運ぶ。
func (h *handlers) authorize(c *gin.Context) {
	if h.oauth == nil {
		oauthUnavailable(c)
		return
	}
	target := url.URL{Path: "/", RawQuery: url.Values{
		consentQueryKey: {c.Request.URL.RawQuery},
	}.Encode()}
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, target.String())
}

// maxOAuthFormBody は `/oauth/token` と `/oauth/register` の本文の上限。
//
// 載るのは ID・コード・URL だけで、`/api` の既定と同じで足りる。登録の
// 戻り先は usecase が数と長さを絞っている（maxRedirectURIs × maxRedirectURILen
// でも 20 KiB）。
const maxOAuthFormBody = defaultMaxBody

// token はトークンの要求を受ける（RFC 6749 3.2）。本文は form。
func (h *handlers) token(c *gin.Context) {
	if h.oauth == nil {
		oauthUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOAuthFormBody)
	if err := c.Request.ParseForm(); err != nil {
		h.failOAuth(c, &usecase.OAuthError{
			Code: usecase.OAuthInvalidRequest, Description: "request body must be a form",
		})
		return
	}
	f := c.Request.PostForm

	res, err := h.oauth.Token(c.Request.Context(), usecase.TokenRequest{
		GrantType:    f.Get("grant_type"),
		Code:         f.Get("code"),
		RedirectURI:  f.Get("redirect_uri"),
		ClientID:     f.Get("client_id"),
		CodeVerifier: f.Get("code_verifier"),
		RefreshToken: f.Get("refresh_token"),
		Resource:     f.Get("resource"),
		Scope:        f.Get("scope"),
	}, h.oauthEndpoints(c))
	if err != nil {
		h.failOAuth(c, err)
		return
	}

	oauthJSON(c, http.StatusOK, gin.H{
		"access_token":  res.AccessToken,
		"token_type":    "Bearer",
		"expires_in":    res.ExpiresIn,
		"refresh_token": res.RefreshToken,
		"scope":         res.Scope,
	})
}

// registrationRequest は動的登録の本文（RFC 7591 2）のうち etoki が読む項目。
type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// register は動的登録を受ける（RFC 7591）。
func (h *handlers) register(c *gin.Context) {
	if h.oauth == nil {
		oauthUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxOAuthFormBody)
	var req registrationRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		h.failOAuth(c, &usecase.OAuthError{
			Code: usecase.OAuthInvalidClientMetadata, Description: "request body must be JSON",
		})
		return
	}

	client, err := h.oauth.Register(c.Request.Context(), usecase.ClientMetadata{
		ClientName:              req.ClientName,
		RedirectURIs:            req.RedirectURIs,
		TokenEndpointAuthMethod: req.TokenEndpointAuthMethod,
		GrantTypes:              req.GrantTypes,
		ResponseTypes:           req.ResponseTypes,
	})
	if err != nil {
		h.failOAuth(c, err)
		return
	}

	oauthJSON(c, http.StatusCreated, gin.H{
		"client_id":                  client.ID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              client.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
	})
}

// parseAuthorizeRequest は `/oauth/authorize` のクエリを読む。
//
// 同じ名前が 2 つ付いた要求は断る（RFC 6749 3.1）。先頭だけを読むと、画面に
// 見せた値と使う値が食い違いうる。
func parseAuthorizeRequest(raw string) (usecase.AuthorizeRequest, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return usecase.AuthorizeRequest{}, fmt.Errorf("%w: request is not a query string", usecase.ErrInvalidInput)
	}
	for k, vs := range q {
		if len(vs) > 1 {
			return usecase.AuthorizeRequest{}, fmt.Errorf("%w: %s appears more than once",
				usecase.ErrInvalidInput, k)
		}
	}
	return usecase.AuthorizeRequest{
		ResponseType:        q.Get("response_type"),
		ClientID:            q.Get("client_id"),
		RedirectURI:         q.Get("redirect_uri"),
		CodeChallenge:       q.Get("code_challenge"),
		CodeChallengeMethod: q.Get("code_challenge_method"),
		Scope:               q.Get("scope"),
		State:               q.Get("state"),
		Resource:            q.Get("resource"),
	}, nil
}

// mcpConnectionsNotConfigured は許可の口を組み立てていないときの案内。
func mcpConnectionsNotConfigured(c *gin.Context) {
	errorJSON(c, http.StatusServiceUnavailable, apitypes.ErrorCodeMcpConnectionsNotConfigured,
		"mcp connections require authentication: "+
			"set ETOKI_GITHUB_APP_CLIENT_ID and ETOKI_GITHUB_APP_CLIENT_SECRET")
}

// getOAuthAuthorization は同意の画面に出すものを返す。何も書かない。
func (h *handlers) getOAuthAuthorization(c *gin.Context) {
	if h.oauth == nil {
		mcpConnectionsNotConfigured(c)
		return
	}
	req, err := parseAuthorizeRequest(c.Query("request"))
	if err != nil {
		h.fail(c, err)
		return
	}
	view, err := h.oauth.Authorization(c.Request.Context(), req, h.oauthEndpoints(c))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.OAuthAuthorization{
		ClientID:      view.ClientID,
		ClientName:    view.ClientName,
		ClientIDIsURL: view.ClientIDIsURL,
		RedirectURI:   view.RedirectURI,
		Scope:         view.Scope,
	})
}

// decideOAuthAuthorization は同意か拒否を受け、クライアントへ戻す先を返す。
//
// **POST なので Origin の検証が効く**（ADR 0013）。リダイレクトを返さず
// URL を返すのは、ログインの開始（/api/auth/login）と同じ理由。
func (h *handlers) decideOAuthAuthorization(c *gin.Context) {
	if h.oauth == nil {
		mcpConnectionsNotConfigured(c)
		return
	}
	var body apitypes.OAuthDecisionRequest
	if !h.bindJSON(c, &body) {
		return
	}
	req, err := parseAuthorizeRequest(body.Request)
	if err != nil {
		h.fail(c, err)
		return
	}

	ctx := c.Request.Context()
	var target string
	if body.Approve {
		user, _ := currentUser(c)
		target, err = h.oauth.Approve(ctx, user.ID, req, h.oauthEndpoints(c))
	} else {
		target, err = h.oauth.Deny(ctx, req, h.oauthEndpoints(c))
	}
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, apitypes.OAuthDecision{RedirectTo: target})
}

// listOAuthGrants は自分の許可を返す。
func (h *handlers) listOAuthGrants(c *gin.Context) {
	if h.oauth == nil {
		mcpConnectionsNotConfigured(c)
		return
	}
	user, _ := currentUser(c)
	grants, err := h.oauth.Grants(c.Request.Context(), user.ID)
	if err != nil {
		h.fail(c, err)
		return
	}

	out := make([]apitypes.OAuthGrant, 0, len(grants))
	for _, g := range grants {
		out = append(out, toOAuthGrant(g))
	}
	c.JSON(http.StatusOK, out)
}

func toOAuthGrant(g port.OAuthGrant) apitypes.OAuthGrant {
	return apitypes.OAuthGrant{
		ID:         g.ID,
		ClientID:   g.ClientID,
		ClientName: g.ClientName,
		Scope:      g.Scope,
		CreatedAt:  g.CreatedAt,
		LastUsedAt: g.LastUsedAt,
	}
}

// revokeOAuthGrant は自分の許可を取り消す。
func (h *handlers) revokeOAuthGrant(c *gin.Context) {
	if h.oauth == nil {
		mcpConnectionsNotConfigured(c)
		return
	}
	user, _ := currentUser(c)
	if err := h.oauth.Revoke(c.Request.Context(), user.ID, c.Param("grantId")); err != nil {
		h.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// requireBearer は `/mcp` に届いたアクセストークンを検証し、利用者を ctx に
// 載せる。
//
// **SDK の auth.RequireBearerToken は使わない。** WWW-Authenticate に載せる
// resource_metadata の URL を固定値で受けるので、Host から URL を組む etoki
// では構成ごとに作れない。返す形は同じにしてある（401 と resource_metadata、
// scope が足りなければ 403）。
//
// **画面のセッション cookie は見ない。** ここに来る前に resolveSession が
// cookie から利用者を載せていても、Bearer が無ければ 401 にし、あれば
// トークンの利用者で上書きする。cookie で通すと、ブラウザが `/mcp` を
// 叩けることになり、MCP のトークンを要求する意味が無くなる。
func (h *handlers) requireBearer(next http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		ep := h.oauthEndpoints(c)
		metadata := h.baseURL(c) + wellKnownResourcePath + mcpPath

		token, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			unauthorizedBearer(c, metadata, "")
			return
		}
		verified, err := h.oauth.Verify(c.Request.Context(), token, ep)
		if err != nil {
			h.logger.ErrorContext(c.Request.Context(), "verify mcp token",
				slog.String("path", c.Request.URL.Path), slog.Any("error", err))
			c.String(http.StatusInternalServerError, "internal error\n")
			return
		}
		if verified == nil {
			unauthorizedBearer(c, metadata, "invalid_token")
			return
		}
		if !containsScope(verified.Scopes, usecase.OAuthScopeRead) {
			c.Header("WWW-Authenticate", fmt.Sprintf(
				`Bearer error="insufficient_scope", scope=%q, resource_metadata=%q`,
				usecase.OAuthScopeRead, metadata))
			c.String(http.StatusForbidden, "insufficient scope\n")
			return
		}

		ctx := port.ContextWithUserID(c.Request.Context(), verified.UserID)
		next.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
	}
}

// bearerToken は Authorization ヘッダから Bearer のトークンを取り出す。
// スキーム名は大文字小文字を区別しない（RFC 7235 2.1）。
func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

func containsScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

// unauthorizedBearer は 401 と、メタデータの在りかを返す（RFC 9728 5.1）。
// MCP のクライアントはこれを読んで認可の流れを始める。
func unauthorizedBearer(c *gin.Context, metadata, errCode string) {
	challenge := fmt.Sprintf(`Bearer resource_metadata=%q`, metadata)
	if errCode != "" {
		challenge = fmt.Sprintf(`Bearer error=%q, resource_metadata=%q`, errCode, metadata)
	}
	c.Header("WWW-Authenticate", challenge)
	c.String(http.StatusUnauthorized, "unauthorized\n")
	c.Abort()
}
