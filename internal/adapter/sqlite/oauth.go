package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// OAuthGrantRepository は port.OAuthGrantRepository の SQLite 実装（ADR 0076）。
//
// 秘密は持たない。コードとトークンはハッシュだけが渡ってくる。
type OAuthGrantRepository struct {
	db *sql.DB
}

// NewOAuthGrantRepository は OAuthGrantRepository を作る。
func NewOAuthGrantRepository(db *sql.DB) *OAuthGrantRepository {
	return &OAuthGrantRepository{db: db}
}

var _ port.OAuthGrantRepository = (*OAuthGrantRepository)(nil)

// fixedTimeLayout は小数 9 桁で固定した RFC 3339。
//
// **期限の判定を文字列の大小で行うので、桁数を揃える。** formatTime の
// RFC3339Nano は小数部の末尾の 0 を落とすので、".5Z" が ".45Z" より前に
// 並ぶ。読むのは parseTime のままでよい（RFC3339Nano の解析は桁数を問わない）。
const fixedTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func formatFixedTime(t time.Time) string {
	return t.UTC().Format(fixedTimeLayout)
}

// CreateClient は登録したクライアントを保存し、許可を得ないまま古くなった
// 登録を消す。
func (r *OAuthGrantRepository) CreateClient(
	ctx context.Context, c port.OAuthClient, unusedBefore time.Time,
) error {
	uris, err := json.Marshal(c.RedirectURIs)
	if err != nil {
		return fmt.Errorf("encode redirect uris: %w", err)
	}

	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_clients WHERE granted_at = '' AND created_at < ?`,
		formatFixedTime(unusedBefore)); err != nil {
		return fmt.Errorf("prune oauth clients: %w", err)
	}

	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`,
		c.ID, c.Name, string(uris), formatFixedTime(c.CreatedAt),
	); err != nil {
		return fmt.Errorf("insert oauth client: %w", err)
	}
	return nil
}

// FindClient は ID でクライアントを引く。
func (r *OAuthGrantRepository) FindClient(ctx context.Context, id string) (*port.OAuthClient, error) {
	var (
		c         port.OAuthClient
		uris      string
		createdAt string
	)
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = ?`, id,
	).Scan(&c.ID, &c.Name, &uris, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find oauth client: %w", err)
	}

	if err := json.Unmarshal([]byte(uris), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("decode redirect uris of %s: %w", id, err)
	}
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// SaveCode は認可コードを保存し、期限切れのコードを消す。
func (r *OAuthGrantRepository) SaveCode(ctx context.Context, c port.OAuthCode) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_codes WHERE expires_at <= ?`, formatFixedTime(c.CreatedAt)); err != nil {
		return fmt.Errorf("prune oauth codes: %w", err)
	}

	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_codes
		   (code_hash, user_id, client_id, client_name, scope, resource, redirect_uri,
		    code_challenge, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CodeHash, c.UserID, c.ClientID, c.ClientName, c.Scope, c.Resource, c.RedirectURI,
		c.CodeChallenge, formatFixedTime(c.CreatedAt), formatFixedTime(c.ExpiresAt),
	); err != nil {
		return fmt.Errorf("insert oauth code: %w", err)
	}
	return nil
}

const codeColumns = `code_hash, user_id, client_id, client_name, scope, resource, redirect_uri,
	code_challenge, created_at, expires_at, grant_id`

func scanCode(s rowScanner) (port.OAuthCode, error) {
	var (
		c                    port.OAuthCode
		createdAt, expiresAt string
	)
	if err := s.Scan(&c.CodeHash, &c.UserID, &c.ClientID, &c.ClientName, &c.Scope, &c.Resource,
		&c.RedirectURI, &c.CodeChallenge, &createdAt, &expiresAt, &c.GrantID); err != nil {
		return port.OAuthCode{}, err
	}

	var err error
	if c.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.OAuthCode{}, err
	}
	if c.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return port.OAuthCode{}, err
	}
	return c, nil
}

// ConsumeCode は期限内のコードを使用済みにして返す。
//
// **使用済みにする照合は UPDATE 1 文に置く**（ConsumeState と同じ理由）。
// 先に読んでから書くと、同じコードで 2 本同時に来たときに両方が 1 回目になる。
func (r *OAuthGrantRepository) ConsumeCode(
	ctx context.Context, codeHash, grantID string, now time.Time,
) (*port.OAuthCode, error) {
	at := formatFixedTime(now)

	c, err := scanCode(r.db.QueryRowContext(ctx,
		`UPDATE oauth_codes SET used_at = ?, grant_id = ?
		 WHERE code_hash = ? AND expires_at > ? AND used_at = ''
		 RETURNING `+codeColumns,
		at, grantID, codeHash, at,
	))
	if err == nil {
		return &c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("consume oauth code: %w", err)
	}

	// 1 回目になれなかった。使用済みなら使い回し、無ければ未知か期限切れ。
	c, err = scanCode(r.db.QueryRowContext(ctx,
		`SELECT `+codeColumns+` FROM oauth_codes
		 WHERE code_hash = ? AND expires_at > ? AND used_at <> ''`,
		codeHash, at,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find replayed oauth code: %w", err)
	}
	c.Replayed = true
	return &c, nil
}

// CreateGrant は許可と最初のトークンを 1 つのトランザクションで保存する。
func (r *OAuthGrantRepository) CreateGrant(
	ctx context.Context, g port.OAuthGrant, tokens []port.OAuthToken,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create grant: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := pruneTokens(ctx, tx, g.CreatedAt); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO oauth_grants
		   (id, user_id, client_id, client_name, scope, resource, created_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.UserID, g.ClientID, g.ClientName, g.Scope, g.Resource,
		formatFixedTime(g.CreatedAt), formatFixedTime(g.LastUsedAt),
	); err != nil {
		return fmt.Errorf("insert oauth grant: %w", err)
	}

	// 動的登録のクライアントなら「一度は許可を得た」を残し、掃除から外す。
	// Client ID Metadata Document のクライアントは表に居ないので何も起きない。
	if _, err := tx.ExecContext(ctx,
		`UPDATE oauth_clients SET granted_at = ? WHERE id = ? AND granted_at = ''`,
		formatFixedTime(g.CreatedAt), g.ClientID,
	); err != nil {
		return fmt.Errorf("mark oauth client granted: %w", err)
	}

	if err := insertTokens(ctx, tx, g.ID, tokens); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create grant: %w", err)
	}
	return nil
}

// pruneTokens は期限切れのトークンと、トークンが 1 本も残っていない許可を消す。
//
// 許可は作るときに必ずトークンを伴うので、トークンが無い許可は期限が切れた
// ものだけになる。
func pruneTokens(ctx context.Context, tx *sql.Tx, now time.Time) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM oauth_tokens WHERE expires_at <= ?`, formatFixedTime(now)); err != nil {
		return fmt.Errorf("prune oauth tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM oauth_grants
		 WHERE NOT EXISTS (SELECT 1 FROM oauth_tokens t WHERE t.grant_id = oauth_grants.id)`,
	); err != nil {
		return fmt.Errorf("prune oauth grants: %w", err)
	}
	return nil
}

func insertTokens(ctx context.Context, tx *sql.Tx, grantID string, tokens []port.OAuthToken) error {
	for _, t := range tokens {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO oauth_tokens (token_hash, grant_id, kind, created_at, expires_at)
			 VALUES (?, ?, ?, ?, ?)`,
			t.TokenHash, grantID, string(t.Kind),
			formatFixedTime(t.CreatedAt), formatFixedTime(t.ExpiresAt),
		); err != nil {
			return fmt.Errorf("insert oauth token: %w", err)
		}
	}
	return nil
}

const grantColumns = `g.id, g.user_id, g.client_id, g.client_name, g.scope, g.resource,
	g.created_at, g.last_used_at`

func scanGrant(s rowScanner, extra ...any) (port.OAuthGrant, error) {
	var (
		g                     port.OAuthGrant
		createdAt, lastUsedAt string
	)
	dest := append([]any{&g.ID, &g.UserID, &g.ClientID, &g.ClientName, &g.Scope, &g.Resource,
		&createdAt, &lastUsedAt}, extra...)
	if err := s.Scan(dest...); err != nil {
		return port.OAuthGrant{}, err
	}

	var err error
	if g.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.OAuthGrant{}, err
	}
	if g.LastUsedAt, err = parseTime(lastUsedAt); err != nil {
		return port.OAuthGrant{}, err
	}
	return g, nil
}

// FindToken は期限内で未使用のトークンを許可ごと引く。
func (r *OAuthGrantRepository) FindToken(
	ctx context.Context, tokenHash string, now time.Time,
) (*port.OAuthTokenGrant, error) {
	var (
		out                  port.OAuthTokenGrant
		kind                 string
		createdAt, expiresAt string
	)
	g, err := scanGrant(r.db.QueryRowContext(ctx,
		`SELECT `+grantColumns+`, t.token_hash, t.kind, t.created_at, t.expires_at
		 FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id
		 WHERE t.token_hash = ? AND t.expires_at > ? AND t.used_at = ''`,
		tokenHash, formatFixedTime(now),
	), &out.Token.TokenHash, &kind, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find oauth token: %w", err)
	}

	out.Grant = g
	out.Token.Kind = port.OAuthTokenKind(kind)
	if out.Token.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if out.Token.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}
	return &out, nil
}

// UseRefreshToken は refresh token を使用済みにして許可を返す。
//
// 照合は ConsumeCode と同じく UPDATE 1 文に置く。
func (r *OAuthGrantRepository) UseRefreshToken(
	ctx context.Context, tokenHash string, now time.Time,
) (*port.OAuthRefresh, error) {
	at := formatFixedTime(now)

	var grantID string
	err := r.db.QueryRowContext(ctx,
		`UPDATE oauth_tokens SET used_at = ?
		 WHERE token_hash = ? AND kind = 'refresh' AND expires_at > ? AND used_at = ''
		 RETURNING grant_id`,
		at, tokenHash, at,
	).Scan(&grantID)
	replayed := false
	if errors.Is(err, sql.ErrNoRows) {
		err = r.db.QueryRowContext(ctx,
			`SELECT grant_id FROM oauth_tokens
			 WHERE token_hash = ? AND kind = 'refresh' AND expires_at > ? AND used_at <> ''`,
			tokenHash, at,
		).Scan(&grantID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		replayed = true
	}
	if err != nil {
		return nil, fmt.Errorf("use oauth refresh token: %w", err)
	}

	g, err := r.FindGrant(ctx, grantID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		// トークンは許可に ON DELETE CASCADE でぶら下がるので、ここには来ない。
		return nil, nil
	}
	return &port.OAuthRefresh{Grant: *g, Replayed: replayed}, nil
}

// AddTokens は許可にトークンを足し、最後に使った時刻を進める。
func (r *OAuthGrantRepository) AddTokens(
	ctx context.Context, grantID string, tokens []port.OAuthToken, now time.Time,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin add tokens: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// **許可の有無を UPDATE の結果で見る。** 先に読む形にすると、読んでから
	// 足すまでのあいだの取り消しを見落とし、取り消した許可にトークンが増える。
	res, err := tx.ExecContext(ctx,
		`UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, formatFixedTime(now), grantID)
	if err != nil {
		return fmt.Errorf("touch oauth grant: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("touch oauth grant: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: oauth grant %s", port.ErrNotFound, grantID)
	}

	if err := insertTokens(ctx, tx, grantID, tokens); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit add tokens: %w", err)
	}
	return nil
}

// FindGrant は ID で許可を引く。
func (r *OAuthGrantRepository) FindGrant(ctx context.Context, id string) (*port.OAuthGrant, error) {
	g, err := scanGrant(r.db.QueryRowContext(ctx,
		`SELECT `+grantColumns+` FROM oauth_grants g WHERE g.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find oauth grant: %w", err)
	}
	return &g, nil
}

// ListGrants は利用者の許可を新しい順に返す。
func (r *OAuthGrantRepository) ListGrants(ctx context.Context, userID string) ([]port.OAuthGrant, error) {
	return r.listGrants(ctx,
		`SELECT `+grantColumns+` FROM oauth_grants g WHERE g.user_id = ?
		 ORDER BY g.created_at DESC, g.id DESC`, userID)
}

// ListAllGrants は全員の許可を新しい順に返す。
func (r *OAuthGrantRepository) ListAllGrants(ctx context.Context) ([]port.OAuthGrant, error) {
	return r.listGrants(ctx,
		`SELECT `+grantColumns+` FROM oauth_grants g ORDER BY g.created_at DESC, g.id DESC`)
}

func (r *OAuthGrantRepository) listGrants(
	ctx context.Context, query string, args ...any,
) ([]port.OAuthGrant, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list oauth grants: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []port.OAuthGrant
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan oauth grant: %w", err)
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list oauth grants: %w", err)
	}
	return out, nil
}

// DeleteGrant は許可を消す。トークンは ON DELETE CASCADE で消える。
func (r *OAuthGrantRepository) DeleteGrant(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM oauth_grants WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete oauth grant: %w", err)
	}
	return nil
}
