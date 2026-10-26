package usecase

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yusuke0610/etoki/internal/loopback"
	"github.com/yusuke0610/etoki/port"
)

// このファイルは MCP のクライアントの登録（RFC 7591）と、client_id からの引き当て
// （Client ID Metadata Document を含む）を置く。型と設定は oauth.go（ADR 0076）。

// Register は動的登録（RFC 7591）を受ける。
//
// **検証を通ったものだけを数える。** 形の誤りで断ったリクエストは何も書かない
// ので、窓を食わせる理由が無い。
func (s *OAuthServer) Register(ctx context.Context, meta ClientMetadata) (port.OAuthClient, error) {
	name, uris, err := validateClientMetadata(meta)
	if err != nil {
		return port.OAuthClient{}, err
	}

	now := s.now()
	if err := s.admitRegistration(now); err != nil {
		return port.OAuthClient{}, err
	}

	id, err := s.random()
	if err != nil {
		return port.OAuthClient{}, err
	}
	client := port.OAuthClient{ID: id, Name: name, RedirectURIs: uris, CreatedAt: now}
	if err := s.grants.CreateClient(ctx, client, now.Add(-UnusedClientTTL)); err != nil {
		return port.OAuthClient{}, err
	}
	return client, nil
}

// admitRegistration は窓の中の登録回数を見て、1 回ぶんを数える。
// 考え方は AuthService.admitLoginStart と同じ。
func (s *OAuthServer) admitRegistration(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.registrations = withinWindow(s.registrations, now.Add(-StateTTL))
	if len(s.registrations) >= MaxClientRegistrations {
		return fmt.Errorf("%w: %d client registrations within %s, limit is %d",
			ErrRateLimited, len(s.registrations), StateTTL, MaxClientRegistrations)
	}
	s.registrations = append(s.registrations, now)
	return nil
}

// validateClientMetadata はクライアントのメタデータを検証し、名前と戻り先を返す。
//
// 動的登録と Client ID Metadata Document で同じ規則を当てる。
func validateClientMetadata(meta ClientMetadata) (string, []string, error) {
	// **公開クライアントだけを受ける。** MCP のクライアントは利用者の手元で
	// 動き、秘密を守れない。秘密を持つ前提の登録を受けると、守れない秘密で
	// 守っているつもりになる。
	if m := meta.TokenEndpointAuthMethod; m != "" && m != oauthTokenEndpointAuthNone {
		return "", nil, oauthError(OAuthInvalidClientMetadata,
			"token_endpoint_auth_method must be none, got %q", m)
	}
	for _, g := range meta.GrantTypes {
		if g != oauthGrantAuthorizationCode && g != oauthGrantRefreshToken {
			return "", nil, oauthError(OAuthInvalidClientMetadata, "unsupported grant_type %q", g)
		}
	}
	for _, r := range meta.ResponseTypes {
		if r != oauthResponseTypeCode {
			return "", nil, oauthError(OAuthInvalidClientMetadata, "unsupported response_type %q", r)
		}
	}

	name := strings.TrimSpace(meta.ClientName)
	if utf8.RuneCountInString(name) > maxClientNameRunes {
		return "", nil, oauthError(OAuthInvalidClientMetadata,
			"client_name must be at most %d characters", maxClientNameRunes)
	}
	// **制御文字と向きの制御を断る。** 名前は同意の画面と `etoki grants` の表に
	// そのまま出る。改行や端末の制御列が入ると、表の行を偽装したり端末の表示を
	// 変えたりでき、向きの制御（RLO など）は画面の名前を別の綴りに見せる。名前は
	// クライアントの自称なので、出す側ごとに逃がすより入口で断る。
	if strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r)
	}) >= 0 {
		return "", nil, oauthError(OAuthInvalidClientMetadata,
			"client_name must not contain control characters")
	}

	if len(meta.RedirectURIs) == 0 || len(meta.RedirectURIs) > maxRedirectURIs {
		return "", nil, oauthError(OAuthInvalidRedirectURI,
			"redirect_uris must have 1 to %d entries", maxRedirectURIs)
	}
	for _, u := range meta.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			return "", nil, err
		}
	}
	return name, meta.RedirectURIs, nil
}

// validateRedirectURI は戻り先が受けられる形かを見る。
//
// **ループバックの http か、https だけ。** それ以外の http を許すと、code が
// 平文で他所へ流れる。`javascript:` のようなスキームは論外。フラグメントは
// RFC 6749 3.1.2 が禁じている。
func validateRedirectURI(raw string) error {
	if len(raw) > maxRedirectURILen {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri must be an absolute URL")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri must not have a fragment")
	}
	if u.User != nil {
		return oauthError(OAuthInvalidRedirectURI, "redirect_uri must not have userinfo")
	}
	switch {
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && loopback.Hostname(u.Hostname()):
		return nil
	default:
		return oauthError(OAuthInvalidRedirectURI,
			"redirect_uri must be https or http on a loopback address")
	}
}

// redirectURIMatches は要求の戻り先が登録のどれかと一致するかを返す。
//
// **ループバックの http だけはポートの違いを許す**（RFC 8252 7.3）。ネイティブの
// クライアントは、そのとき空いているポートで待つ。それ以外は文字列の完全一致。
func redirectURIMatches(registered []string, requested string) bool {
	req, err := url.Parse(requested)
	if err != nil {
		return false
	}
	for _, r := range registered {
		if r == requested {
			return true
		}
		reg, err := url.Parse(r)
		if err != nil {
			continue
		}
		if reg.Scheme == "http" && req.Scheme == "http" &&
			loopback.Hostname(reg.Hostname()) && reg.Hostname() == req.Hostname() &&
			reg.EscapedPath() == req.EscapedPath() && reg.RawQuery == req.RawQuery {
			return true
		}
	}
	return false
}

// resolvedClient は client_id を引いた結果。
type resolvedClient struct {
	id           string
	name         string
	redirectURIs []string
	isURL        bool
}

// resolveClient は client_id からクライアントを引く。
//
// https で始まる client_id は Client ID Metadata Document として取りに行く。
// それ以外は動的登録の ID として表を引く。動的登録の ID は乱数なので https で
// 始まることは無い。
func (s *OAuthServer) resolveClient(ctx context.Context, clientID string) (resolvedClient, error) {
	if clientID == "" {
		return resolvedClient{}, fmt.Errorf("%w: client_id is required", ErrInvalidInput)
	}

	if !strings.HasPrefix(clientID, clientMetadataDocumentScheme) {
		c, err := s.grants.FindClient(ctx, clientID)
		if err != nil {
			return resolvedClient{}, err
		}
		if c == nil {
			return resolvedClient{}, fmt.Errorf("%w: unknown client_id", ErrInvalidInput)
		}
		return resolvedClient{id: c.ID, name: c.Name, redirectURIs: c.RedirectURIs}, nil
	}

	if err := validateClientIDURL(clientID); err != nil {
		return resolvedClient{}, err
	}
	if s.fetcher == nil {
		return resolvedClient{}, fmt.Errorf(
			"%w: client id metadata documents are not supported", ErrInvalidInput)
	}
	meta, err := s.fetcher.Fetch(ctx, clientID)
	if err != nil {
		return resolvedClient{}, fmt.Errorf("%w: fetch client metadata: %w", ErrInvalidInput, err)
	}
	// **文書の client_id が URL と一致しなければ断る**（CIMD の定め）。別の
	// URL の文書を置かれると、名乗る名前の出どころが URL のドメインでなくなる。
	if meta.ClientID != clientID {
		return resolvedClient{}, fmt.Errorf(
			"%w: client metadata document has client_id %q", ErrInvalidInput, meta.ClientID)
	}
	name, uris, err := validateClientMetadata(meta)
	if err != nil {
		return resolvedClient{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return resolvedClient{id: clientID, name: name, redirectURIs: uris, isURL: true}, nil
}

// validateClientIDURL は Client ID Metadata Document の URL の形を見る。
//
// 行き先の絞り込み（プライベートなアドレスに繋がない、など）は取りに行く側の
// 仕事で、ここは URL として受けられるかだけを見る。
func validateClientIDURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Opaque != "" {
		return fmt.Errorf("%w: client_id must be an https URL", ErrInvalidInput)
	}
	// パスを必須にするのは CIMD の定め。ホストだけの URL を許すと、同じ
	// ドメインの別のクライアントと見分けられない。
	if u.Path == "" || u.Path == "/" {
		return fmt.Errorf("%w: client_id URL must have a path", ErrInvalidInput)
	}
	if u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return fmt.Errorf("%w: client_id URL must not have a fragment or userinfo", ErrInvalidInput)
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return fmt.Errorf("%w: client_id URL must not have dot segments", ErrInvalidInput)
		}
	}
	return nil
}
