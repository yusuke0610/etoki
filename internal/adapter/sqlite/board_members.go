package sqlite

import (
	"context"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// ownerRole は board_members.role に書く持ち主の値。
//
// ロールは外した（ADR 0107）が、列と CHECK は残っている（表は #496 で畳む）。
// ボードを作った本人の行には、これまでどおりこの値を書く。**値で読み分けるのは
// 所有者の無いボードを数える・引き受けるとき（CountUnowned / ClaimUnowned）
// だけ。** ボードに触れるかは、値によらずメンバーかどうかで決める。共有で
// 招いた人の行（editor / viewer）が残っていても、その人はログインできない。
const ownerRole = "owner"

// CountUnowned は所有者の無いボードの数を返す。
func (r *BoardRepository) CountUnowned(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM board_members WHERE user_id = '' AND role = ?`,
		ownerRole).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unowned boards: %w", err)
	}
	return n, nil
}

// ClaimUnowned は所有者の無いボードをすべて owner のものにする。
func (r *BoardRepository) ClaimUnowned(ctx context.Context, owner string) (int64, error) {
	if owner == "" {
		// 空文字は「所有者が無い」そのもの。引き受けたことにならない。
		return 0, fmt.Errorf("claim boards: %w: owner is required", port.ErrNotFound)
	}

	// role の条件は CountUnowned と揃える。片方だけが共有で招いた行まで拾うと、
	// 数えた件数と引き受けた件数が食い違う。
	res, err := r.db.ExecContext(ctx,
		`UPDATE board_members SET user_id = ? WHERE user_id = '' AND role = ?`,
		owner, ownerRole)
	if err != nil {
		return 0, fmt.Errorf("claim boards: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected for claim: %w", err)
	}
	return n, nil
}
