package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// BoardRepository は port.BoardRepository の SQLite 実装。
type BoardRepository struct {
	db *sql.DB
}

// NewBoardRepository は BoardRepository を作る。
func NewBoardRepository(db *sql.DB) *BoardRepository {
	return &BoardRepository{db: db}
}

var _ port.BoardRepository = (*BoardRepository)(nil)

// boardColumns は SELECT する列。Find と List で同じ並びを使う。
//
// scanBoard が並び順に依存するので、片方だけ足すと取り違える。
const boardColumns = `id, name, scene, owner_user_id,
	repository_owner, repository_name, project_id,
	created_at, updated_at`

// Create は新しいボードを保存する。
func (r *BoardRepository) Create(ctx context.Context, b port.Board) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO boards (id, name, scene, owner_user_id,
		                     repository_owner, repository_name, project_id,
		                     created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Name, b.Scene, b.OwnerUserID,
		b.Target.RepositoryOwner, b.Target.RepositoryName, b.Target.ProjectID,
		formatTime(b.CreatedAt), formatTime(b.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert board %s: %w", b.ID, err)
	}
	return nil
}

// UpdateScene はシーンと更新時刻だけを更新する。
func (r *BoardRepository) UpdateScene(
	ctx context.Context, owner, id, scene string, updatedAt time.Time,
) error {
	// 所有者を WHERE に入れる。ここは Find を通らずに直接 UPDATE する経路なので、
	// 絞り忘れると他人のボードを書き換えられる（ADR 0016）。
	return r.update(ctx, id,
		`UPDATE boards SET scene = ?, updated_at = ? WHERE id = ? AND owner_user_id = ?`,
		scene, formatTime(updatedAt), id, owner)
}

// UpdateTarget は作成先と更新時刻だけを更新する。
func (r *BoardRepository) UpdateTarget(
	ctx context.Context, owner, id string, t port.BoardTarget, updatedAt time.Time,
) error {
	return r.update(ctx, id,
		`UPDATE boards
		 SET repository_owner = ?, repository_name = ?, project_id = ?, updated_at = ?
		 WHERE id = ? AND owner_user_id = ?`,
		t.RepositoryOwner, t.RepositoryName, t.ProjectID, formatTime(updatedAt), id, owner)
}

// CountUnowned は所有者の無いボードの数を返す。
func (r *BoardRepository) CountUnowned(ctx context.Context) (int, error) {
	var n int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM boards WHERE owner_user_id = ''`).Scan(&n); err != nil {
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

	res, err := r.db.ExecContext(ctx,
		`UPDATE boards SET owner_user_id = ? WHERE owner_user_id = ''`, owner)
	if err != nil {
		return 0, fmt.Errorf("claim boards: %w", err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected for claim: %w", err)
	}
	return n, nil
}

// update は 1 行更新を実行し、対象が無ければ ErrNotFound にする。
//
// UPDATE は対象が無くてもエラーにならない。存在しない ID への更新が黙って
// 成功すると、呼び出し側は保存できたと思い込む。
func (r *BoardRepository) update(ctx context.Context, id, query string, args ...any) error {
	res, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update board %s: %w", id, err)
	}

	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected for board %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("update board %s: %w", id, port.ErrNotFound)
	}

	return nil
}

// Find は ID でボードを引く。存在しなければ (nil, nil) を返す。
func (r *BoardRepository) Find(ctx context.Context, owner, id string) (*port.Board, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+boardColumns+` FROM boards WHERE id = ? AND owner_user_id = ?`, id, owner)

	b, err := scanBoard(row)
	if errors.Is(err, sql.ErrNoRows) {
		// 「無い」と「他人のもの」を区別しない。区別すると、ID を総当たりして
		// 他人のボードの存在を確かめられる。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find board %s: %w", id, err)
	}

	return &b, nil
}

// List は所有者のボードを更新時刻の降順で返す。
func (r *BoardRepository) List(ctx context.Context, owner string) ([]port.Board, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+boardColumns+` FROM boards
		 WHERE owner_user_id = ? ORDER BY updated_at DESC, id`, owner)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var boards []port.Board
	for rows.Next() {
		b, err := scanBoard(rows)
		if err != nil {
			return nil, fmt.Errorf("scan board: %w", err)
		}
		boards = append(boards, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate boards: %w", err)
	}

	return boards, nil
}

// rowScanner は *sql.Row と *sql.Rows の共通部分。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanBoard(s rowScanner) (port.Board, error) {
	var (
		b                    port.Board
		createdAt, updatedAt string
	)
	if err := s.Scan(&b.ID, &b.Name, &b.Scene, &b.OwnerUserID,
		&b.Target.RepositoryOwner, &b.Target.RepositoryName, &b.Target.ProjectID,
		&createdAt, &updatedAt); err != nil {
		return port.Board{}, err
	}

	var err error
	if b.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.Board{}, err
	}
	if b.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return port.Board{}, err
	}

	return b, nil
}
