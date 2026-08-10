package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yusuke0610/etoki/internal/secret"
	"github.com/yusuke0610/etoki/port"
)

// SessionRepository は port.SessionRepository の SQLite 実装。
//
// 資格情報は secret.Box で封をしてから書く。平文で置かないのは、共有サーバーに
// なる前提があるため（ADR 0015）。
type SessionRepository struct {
	db    *sql.DB
	box   secret.Box
	newID func() string
}

// NewSessionRepository は SessionRepository を作る。
func NewSessionRepository(db *sql.DB, box secret.Box) *SessionRepository {
	return &SessionRepository{db: db, box: box, newID: uuid.NewString}
}

var _ port.SessionRepository = (*SessionRepository)(nil)

// UpsertUser は provider と subject で利用者を引き当て、無ければ作る。
func (r *SessionRepository) UpsertUser(ctx context.Context, u port.User) (port.User, error) {
	// login と display_name は変わりうるので毎回書く。id と created_at は
	// 既存のものを保つ。ここで id を振り直すと、ボードの所有者（PR-C）を
	// 見失う。
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO users (id, provider, subject, login, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (provider, subject) DO UPDATE SET
		   login = excluded.login,
		   display_name = excluded.display_name,
		   updated_at = excluded.updated_at`,
		r.newID(), u.Provider, u.Subject, u.Login, u.DisplayName,
		formatTime(u.CreatedAt), formatTime(u.UpdatedAt),
	)
	if err != nil {
		return port.User{}, fmt.Errorf("upsert user %s/%s: %w", u.Provider, u.Subject, err)
	}

	row := r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE provider = ? AND subject = ?`,
		u.Provider, u.Subject)

	got, err := scanUser(row)
	if err != nil {
		return port.User{}, fmt.Errorf("read back user %s/%s: %w", u.Provider, u.Subject, err)
	}

	return got, nil
}

// FindUser は ID で利用者を引く。存在しなければ (nil, nil) を返す。
func (r *SessionRepository) FindUser(ctx context.Context, id string) (*port.User, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id)

	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user %s: %w", id, err)
	}

	return &u, nil
}

// FindUserByLogin は login で利用者を引く。存在しなければ (nil, nil) を返す。
func (r *SessionRepository) FindUserByLogin(
	ctx context.Context, provider, login string,
) (*port.User, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE provider = ? AND login = ?`, provider, login)

	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find user %s/%s: %w", provider, login, err)
	}

	return &u, nil
}

const userColumns = `id, provider, subject, login, display_name, created_at, updated_at`

func scanUser(s rowScanner) (port.User, error) {
	var (
		u                    port.User
		createdAt, updatedAt string
	)
	if err := s.Scan(&u.ID, &u.Provider, &u.Subject, &u.Login, &u.DisplayName,
		&createdAt, &updatedAt); err != nil {
		return port.User{}, err
	}

	var err error
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.User{}, err
	}
	if u.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return port.User{}, err
	}

	return u, nil
}

// CreateSession はセッションを保存する。
//
// ついでに期限切れを掃除する。ログインは頻繁ではなく、表が育つのもここなので
// 掃除に適している。読み取り側でやると、GET のたびに DELETE を打つことになる。
func (r *SessionRepository) CreateSession(ctx context.Context, s port.Session) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at <= ?`, formatTime(s.CreatedAt)); err != nil {
		return fmt.Errorf("prune sessions: %w", err)
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		s.TokenHash, s.UserID, formatTime(s.CreatedAt), formatTime(s.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// FindSession は token のハッシュでセッションを引く。
//
// 期限切れは存在しないものとして扱う。
//
// **ここでは消さない。** 解決はほぼ全リクエストで走るので、掃除を混ぜると
// GET のたびに DELETE を打つことになる。掃除は CreateSession に置いてある。
func (r *SessionRepository) FindSession(
	ctx context.Context, tokenHash string, now time.Time,
) (*port.Session, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, created_at, expires_at
		 FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, formatTime(now))

	var (
		s                    port.Session
		createdAt, expiresAt string
	)
	err := row.Scan(&s.TokenHash, &s.UserID, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		// 無い場合と期限切れを区別しない。呼び出し側はどちらも未ログイン。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find session: %w", err)
	}

	if s.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if s.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}

	return &s, nil
}

// DeleteSession はセッションを消す。存在しなくても誤りとしない。
func (r *SessionRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SaveCredentials は利用者の資格情報を封をして保存する。
func (r *SessionRepository) SaveCredentials(
	ctx context.Context, userID string, c port.Credentials, now time.Time,
) error {
	access, err := r.box.Seal(c.AccessToken)
	if err != nil {
		return fmt.Errorf("seal access token: %w", err)
	}
	refresh, err := r.box.Seal(c.RefreshToken)
	if err != nil {
		return fmt.Errorf("seal refresh token: %w", err)
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO github_tokens
		   (user_id, access_token, refresh_token, expires_at, refresh_expires_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET
		   access_token = excluded.access_token,
		   refresh_token = excluded.refresh_token,
		   expires_at = excluded.expires_at,
		   refresh_expires_at = excluded.refresh_expires_at,
		   updated_at = excluded.updated_at`,
		userID, access, refresh,
		formatOptionalTime(c.ExpiresAt), formatOptionalTime(c.RefreshExpiresAt), formatTime(now),
	)
	if err != nil {
		return fmt.Errorf("save credentials for %s: %w", userID, err)
	}

	return nil
}

// FindCredentials は利用者の資格情報を返す。無ければ (nil, nil)。
func (r *SessionRepository) FindCredentials(
	ctx context.Context, userID string,
) (*port.Credentials, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT access_token, refresh_token, expires_at, refresh_expires_at
		 FROM github_tokens WHERE user_id = ?`, userID)

	var access, refresh, expiresAt, refreshExpiresAt string
	err := row.Scan(&access, &refresh, &expiresAt, &refreshExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find credentials for %s: %w", userID, err)
	}

	var c port.Credentials
	if c.AccessToken, err = r.box.Open(access); err != nil {
		// 鍵を変えた、あるいは DB を差し替えた。開けないものは無いのと同じに
		// 扱い、再ログインに落とす。中身を推測して復旧しようとしない。
		return nil, fmt.Errorf("open credentials for %s: %w", userID, err)
	}
	if c.RefreshToken, err = r.box.Open(refresh); err != nil {
		return nil, fmt.Errorf("open credentials for %s: %w", userID, err)
	}
	if c.ExpiresAt, err = parseOptionalTime(expiresAt); err != nil {
		return nil, err
	}
	if c.RefreshExpiresAt, err = parseOptionalTime(refreshExpiresAt); err != nil {
		return nil, err
	}

	return &c, nil
}

// SaveState は発行した state を保存する。
//
// CreateSession と同じく、書き込むついでに期限切れを掃除する。ConsumeState
// だけに任せると、認可画面から戻らなかった分が残り続ける。**ログインを始めて
// やめるのは異常ではない**ので、掃除を戻ってきた場合だけに置くと表が育つ。
func (r *SessionRepository) SaveState(
	ctx context.Context, state string, createdAt, expiresAt time.Time,
) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_states WHERE expires_at <= ?`, formatTime(createdAt)); err != nil {
		return fmt.Errorf("prune oauth states: %w", err)
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_states (state, created_at, expires_at) VALUES (?, ?, ?)`,
		state, formatTime(createdAt), formatTime(expiresAt),
	)
	if err != nil {
		return fmt.Errorf("insert oauth state: %w", err)
	}
	return nil
}

// ConsumeState は state を照合して削除する。
//
// DELETE の影響行数で判定する。SELECT してから DELETE すると、同じ state で
// 2 本同時に来たときに両方通る。
func (r *SessionRepository) ConsumeState(
	ctx context.Context, state string, now time.Time,
) (bool, error) {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_states WHERE expires_at <= ?`, formatTime(now)); err != nil {
		return false, fmt.Errorf("prune oauth states: %w", err)
	}

	res, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_states WHERE state = ? AND expires_at > ?`,
		state, formatTime(now))
	if err != nil {
		return false, fmt.Errorf("consume oauth state: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected for oauth state: %w", err)
	}

	return n == 1, nil
}

// formatOptionalTime はゼロ値を空文字にする。
//
// 失効しない構成では期限が無い。NULL 許容にせず空文字で表すのは、
// 「値が無い」の表し方をボードの作成先（ADR 0014）と揃えるため。
func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTime(t)
}

func parseOptionalTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return parseTime(s)
}
