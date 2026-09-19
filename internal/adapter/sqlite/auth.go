package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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
//
// **同じ login を持つ他の行からは login を外す**（ADR 0053）。login は
// ログインしたときにしか書き換わらないので、改名で空いた login を別人が取ると
// 以前の持ち主の行が同じ login を名乗り続ける。いまその login でログインした
// のはこの利用者なので、引き当てはここに寄せる。
func (r *SessionRepository) UpsertUser(ctx context.Context, u port.User) (port.User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return port.User{}, fmt.Errorf("begin upsert user: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// updated_at は触らない。「最後にログインした時刻」で、招待の確認に出す。
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET login = ''
		 WHERE provider = ? AND subject <> ? AND login <> '' AND login = ? COLLATE NOCASE`,
		u.Provider, u.Subject, u.Login,
	); err != nil {
		return port.User{}, fmt.Errorf("release login %q: %w", u.Login, err)
	}

	// login と display_name は変わりうるので毎回書く。id と created_at は
	// 既存のものを保つ。ここで id を振り直すと、ボードの所有者（PR-C）を
	// 見失う。
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, provider, subject, login, display_name, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (provider, subject) DO UPDATE SET
		   login = excluded.login,
		   display_name = excluded.display_name,
		   updated_at = excluded.updated_at`,
		r.newID(), u.Provider, u.Subject, u.Login, u.DisplayName,
		formatTime(u.CreatedAt), formatTime(u.UpdatedAt),
	); err != nil {
		return port.User{}, fmt.Errorf("upsert user %s/%s: %w", u.Provider, u.Subject, err)
	}

	row := tx.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE provider = ? AND subject = ?`,
		u.Provider, u.Subject)

	got, err := scanUser(row)
	if err != nil {
		return port.User{}, fmt.Errorf("read back user %s/%s: %w", u.Provider, u.Subject, err)
	}

	if err := tx.Commit(); err != nil {
		return port.User{}, fmt.Errorf("commit upsert user %s/%s: %w", u.Provider, u.Subject, err)
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

// FindUsers は ID をまとめて引く。見つからなかった ID は結果に現れない。
func (r *SessionRepository) FindUsers(ctx context.Context, ids []string) ([]port.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// IN 句のプレースホルダは件数ぶん組み立てる。ids を文字列連結すると
	// SQL インジェクションになる。
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("find users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []port.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}

	return users, nil
}

// FindUserByLogin は login で利用者を引く。存在しなければ (nil, nil) を返す。
//
// 大文字小文字は区別しない（GitHub の login と同じ）。同じ login を持つ行は
// 1 つに保ってある（`UpsertUser` と一意索引、ADR 0053）。
func (r *SessionRepository) FindUserByLogin(
	ctx context.Context, provider, login string,
) (*port.User, error) {
	// 外した login は空文字で持っている。空の入力でそのどれかを引かない。
	if login == "" {
		return nil, nil
	}

	row := r.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users
		 WHERE provider = ? AND login <> '' AND login = ? COLLATE NOCASE`, provider, login)

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
func (r *SessionRepository) SaveState(ctx context.Context, st port.OAuthState) error {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_states WHERE expires_at <= ?`, formatTime(st.CreatedAt)); err != nil {
		return fmt.Errorf("prune oauth states: %w", err)
	}

	_, err := r.db.ExecContext(ctx,
		`INSERT INTO oauth_states (state, return_to, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		st.State, st.ReturnTo, formatTime(st.CreatedAt), formatTime(st.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("insert oauth state: %w", err)
	}
	return nil
}

// ConsumeState は state を照合して削除し、保存してあった内容を返す。
//
// 消した行をそのまま受け取る（RETURNING）。SELECT してから DELETE すると、
// 同じ state で 2 本同時に来たときに両方通る。照合・削除・読み出しを 1 文に
// 置くことで、返した戻り先が「その 1 本だけが消した行のもの」であることが
// 文の側で決まる。
func (r *SessionRepository) ConsumeState(
	ctx context.Context, state string, now time.Time,
) (*port.OAuthState, error) {
	if _, err := r.db.ExecContext(ctx,
		`DELETE FROM oauth_states WHERE expires_at <= ?`, formatTime(now)); err != nil {
		return nil, fmt.Errorf("prune oauth states: %w", err)
	}

	// **読まれるものだけ返す。** 時刻まで RETURNING して詰めることもできるが、
	// 誰も読まないうえ、その解析の失敗が通った state を 500 に変える。
	var returnTo string
	err := r.db.QueryRowContext(ctx,
		`DELETE FROM oauth_states WHERE state = ? AND expires_at > ?
		 RETURNING return_to`,
		state, formatTime(now),
	).Scan(&returnTo)
	if errors.Is(err, sql.ErrNoRows) {
		// 未知・使用済み・期限切れ。どれも「通らなかった」で同じ扱い。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consume oauth state: %w", err)
	}

	return &port.OAuthState{State: state, ReturnTo: returnTo}, nil
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
