package usecase

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// このファイルはトークンの発行・更新（RFC 6749 / 7636 / 8707）と、`/mcp` に届いた
// アクセストークンの検証を置く。型と設定は oauth.go（ADR 0076）。

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
