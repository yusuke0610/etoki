package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// AccountRepository は port.AccountRepository の SQLite 実装（ADR 0101）。
//
// SessionRepository と別の型にしてあるのは、資格情報を開く鍵（secret.Box）が
// 要らないため。消すだけなら封を開けない。鍵なしで作れるので、配線は
// OAuthGrantRepository と同じく、認証を設定したかどうかに関わらず渡せる
// （Auth が nil なら etoki.New が使わない）。
type AccountRepository struct {
	db *sql.DB
}

// NewAccountRepository は AccountRepository を作る。
func NewAccountRepository(db *sql.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

var _ port.AccountRepository = (*AccountRepository)(nil)

// DeleteUser は利用者の行を消す。存在しなくても誤りとしない。
//
// **セッション・資格情報・MCP の許可は、スキーマの `ON DELETE CASCADE` が一緒に
// 消す**（マイグレーション 0003 / 0016）。消える範囲を Go に書き下さない
// （ボードの削除と同じ）。`board_members` には外部キーが無い（ADR 0016）ので、
// ここでは消えない。片付けるのはユースケース層。
func (r *AccountRepository) DeleteUser(ctx context.Context, userID string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("delete user %s: %w", userID, err)
	}
	return nil
}
