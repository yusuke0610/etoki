package usecase

import (
	"context"
	"fmt"
	"net/url"

	"github.com/yusuke0610/etoki/port"
)

// このファイルは認可の要求の検証と、同意の画面からの許可・拒否を置く。型と設定は
// oauth.go（ADR 0076）。

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
