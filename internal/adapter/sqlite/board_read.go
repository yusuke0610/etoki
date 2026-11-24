package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// Find は ID でボードを操作者のロールつきで引く。
func (r *BoardRepository) Find(
	ctx context.Context, actor, id string,
) (*port.BoardAccess, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+boardColumns+` `+memberJoin+` WHERE b.id = ?`, actor, id)

	a, err := scanBoard(row)
	if errors.Is(err, sql.ErrNoRows) {
		// 「無い」と「メンバーでない」を区別しない。区別すると、ID を総当たり
		// して他人のボードの存在を確かめられる（ADR 0016 / 0017）。
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find board %s: %w", id, err)
	}

	return &a, nil
}

// FindWithFiles は Find に加えて、そのボードの画像をすべて返す。
//
// **シーンと画像は同じトランザクションで読む**（ADR 0074）。WAL では
// トランザクションの中の読み取りは同じ時点を見るので、間に保存が入っても
// シーンが指す画像が欠けない。
func (r *BoardRepository) FindWithFiles(
	ctx context.Context, actor, id string,
) (*port.BoardAccess, []port.BoardFile, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin find board %s: %w", id, err)
	}
	// 読むだけなので、閉じ方は Rollback で足りる。
	defer func() { _ = tx.Rollback() }()

	a, err := scanBoard(tx.QueryRowContext(ctx,
		`SELECT `+boardColumns+` `+memberJoin+` WHERE b.id = ?`, actor, id))
	if errors.Is(err, sql.ErrNoRows) {
		// Find と同じく「無い」と「メンバーでない」を区別しない。
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("find board %s: %w", id, err)
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT id, data FROM board_files WHERE board_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, nil, fmt.Errorf("list files of board %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	files := []port.BoardFile{}
	for rows.Next() {
		var f port.BoardFile
		if err := rows.Scan(&f.ID, &f.Data); err != nil {
			return nil, nil, fmt.Errorf("scan file: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate files: %w", err)
	}

	return &a, files, nil
}

// FileSizes はボードが持っている画像の ID ごとのバイト数を返す。
//
// **data の列は読まない。** bytes は data より前の列に置いてあるので、画像の
// 中身が載っているページに触らずに済む（マイグレーション 0015）。
func (r *BoardRepository) FileSizes(
	ctx context.Context, boardID string,
) (map[string]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, bytes FROM board_files WHERE board_id = ?`, boardID)
	if err != nil {
		return nil, fmt.Errorf("list file sizes of board %s: %w", boardID, err)
	}
	defer func() { _ = rows.Close() }()

	sizes := map[string]int64{}
	for rows.Next() {
		var (
			id    string
			bytes int64
		)
		if err := rows.Scan(&id, &bytes); err != nil {
			return nil, fmt.Errorf("scan file size: %w", err)
		}
		sizes[id] = bytes
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file sizes: %w", err)
	}
	return sizes, nil
}

// List は操作者がメンバーであるボードを更新時刻の降順で返す。
func (r *BoardRepository) List(ctx context.Context, actor string) ([]port.BoardAccess, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+summaryColumns+` `+memberJoin+` ORDER BY b.updated_at DESC, b.id`, actor)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var boards []port.BoardAccess
	for rows.Next() {
		a, err := scanSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan board: %w", err)
		}
		boards = append(boards, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate boards: %w", err)
	}

	return boards, nil
}

// scanSummary は summaryColumns の並びを読む。
//
// extra は summaryColumns の後ろに足した列の受け皿。呼び出し側が SELECT に
// 足したぶんだけ渡す。列と受け皿の数が合わなければ Scan が落ちるので、
// 片方だけ足したまま素通りすることはない。
func scanSummary(s rowScanner, extra ...any) (port.BoardAccess, error) {
	var (
		a                    port.BoardAccess
		role                 string
		createdAt, updatedAt string
	)
	dest := append([]any{&a.Board.ID, &a.Board.Name,
		&a.Board.Target.RepositoryOwner, &a.Board.Target.RepositoryName,
		&a.Board.Target.ProjectID,
		&a.Board.Target.ProjectNumber, &a.Board.Target.ProjectTitle,
		&a.Board.Target.ProjectURL,
		&createdAt, &updatedAt, &role}, extra...)
	if err := s.Scan(dest...); err != nil {
		return port.BoardAccess{}, err
	}

	a.Role = port.BoardRole(role)

	var err error
	if a.Board.CreatedAt, err = parseTime(createdAt); err != nil {
		return port.BoardAccess{}, err
	}
	if a.Board.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return port.BoardAccess{}, err
	}

	return a, nil
}

// scanBoard は boardColumns の並びを読む。シーンと背景を含む。
func scanBoard(s rowScanner) (port.BoardAccess, error) {
	// port.BoardAccess は scanSummary が作って返すので、その中の Scene を
	// 直接 Scan の受け皿にはできない。いったん受けてから詰める。
	var scene, premise string

	a, err := scanSummary(s, &scene, &premise)
	if err != nil {
		return port.BoardAccess{}, err
	}
	a.Board.Scene = scene
	a.Board.Premise = premise

	return a, nil
}
