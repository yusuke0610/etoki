package sqlite

import (
	"context"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// CountUnowned は所有者の無いボードの数を返す。
func (r *BoardRepository) CountUnowned(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM board_members WHERE user_id = '' AND role = ?`,
		string(port.RoleOwner)).Scan(&n); err != nil {
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

	// role の条件は CountUnowned と揃える。片方だけが招待された行まで拾うと、
	// 数えた件数と引き受けた件数が食い違い、owner のつもりで viewer の行を
	// 受け取ることになる。
	res, err := r.db.ExecContext(ctx,
		`UPDATE board_members SET user_id = ? WHERE user_id = '' AND role = ?`,
		owner, string(port.RoleOwner))
	if err != nil {
		return 0, fmt.Errorf("claim boards: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected for claim: %w", err)
	}
	return n, nil
}

// ListMembers はボードのメンバーを返す。
//
// 並びは古い順。owner が先頭に来るので、一覧の先頭が作った人になる。
func (r *BoardRepository) ListMembers(
	ctx context.Context, boardID string,
) ([]port.BoardMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT board_id, user_id, role, created_at FROM board_members
		 WHERE board_id = ? ORDER BY created_at, user_id`, boardID)
	if err != nil {
		return nil, fmt.Errorf("list members of board %s: %w", boardID, err)
	}
	defer func() { _ = rows.Close() }()

	var members []port.BoardMember
	for rows.Next() {
		var (
			m         port.BoardMember
			role      string
			createdAt string
		)
		if err := rows.Scan(&m.BoardID, &m.UserID, &role, &createdAt); err != nil {
			return nil, fmt.Errorf("scan member: %w", err)
		}

		m.Role = port.BoardRole(role)
		if m.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate members: %w", err)
	}

	return members, nil
}

// AddMember はメンバーを 1 人足す。
func (r *BoardRepository) AddMember(ctx context.Context, m port.BoardMember) error {
	// 既存なら何もしない。黙って上書きすると「招待した」と「ロールを変えた」を
	// 呼び出し側が区別できなくなる。件数で見分けて ErrAlreadyExists を返す。
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO board_members (board_id, user_id, role, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (board_id, user_id) DO NOTHING`,
		m.BoardID, m.UserID, string(m.Role), formatTime(m.CreatedAt))
	if err != nil {
		return fmt.Errorf("add member to board %s: %w", m.BoardID, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for add member: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("add member to board %s: %w", m.BoardID, port.ErrAlreadyExists)
	}

	return nil
}

// UpdateMemberRole はメンバーのロールを変える。
func (r *BoardRepository) UpdateMemberRole(
	ctx context.Context, boardID, userID string, role port.BoardRole,
) error {
	return r.exec(ctx, "member "+userID+" of board "+boardID,
		`UPDATE board_members SET role = ? WHERE board_id = ? AND user_id = ?`,
		string(role), boardID, userID)
}

// RemoveMember はメンバーを外す。
func (r *BoardRepository) RemoveMember(ctx context.Context, boardID, userID string) error {
	return r.exec(ctx, "member "+userID+" of board "+boardID,
		`DELETE FROM board_members WHERE board_id = ? AND user_id = ?`,
		boardID, userID)
}
