package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/yusuke0610/etoki/migrations"
)

// ErrNotMigrated はマイグレーションが未適用であることを表す。
//
// SQLite は接続時にファイルを作るため、空の DB でもサーバーは起動できて
// しまう。その状態を起動時に検出して落とすために使う。
// メッセージに "etoki: " を付けないのは、cmd 側が既に前置きしているため。
var ErrNotMigrated = errors.New("database is not migrated")

// createSchemaMigrations は適用済みバージョンを記録する表を用意する。
const createSchemaMigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	version    TEXT PRIMARY KEY,
	applied_at TEXT NOT NULL
)`

// Migrate は未適用のマイグレーションをファイル名の昇順で適用する。
//
// 何度呼んでも安全であり、2 回目以降は何もしない。ロールバックは提供しない。
// 前進のみとする判断は docs/adr/0003-sqlite-and-migrations.md を参照。
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createSchemaMigrations); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}

	pending, err := pendingVersions(applied)
	if err != nil {
		return err
	}

	for _, name := range pending {
		if err := applyMigration(ctx, db, name); err != nil {
			return err
		}
	}

	return nil
}

// EnsureMigrated は未適用のマイグレーションが無いことを確かめる。
//
// マイグレーションは明示的な操作にしておきたいので、ここでは適用しない。
// 代わりに何をすべきかが分かるエラーを返す。黙って 500 を返し続けるより、
// 起動時に落ちて指示を出す方が原因にたどり着ける。
func EnsureMigrated(ctx context.Context, db *sql.DB) error {
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		// schema_migrations 自体が無い＝一度も適用していない。
		return fmt.Errorf("%w", ErrNotMigrated)
	}

	pending, err := pendingVersions(applied)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%w (pending: %v)", ErrNotMigrated, pending)
	}

	return nil
}

// pendingVersions は未適用のマイグレーション名を昇順で返す。
func pendingVersions(applied []string) ([]string, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("glob migrations: %w", err)
	}
	slices.Sort(names)

	var pending []string
	for _, name := range names {
		if !slices.Contains(applied, name) {
			pending = append(pending, name)
		}
	}

	return pending, nil
}

// appliedVersions は適用済みのバージョン名を返す。
func appliedVersions(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("select schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}

	return versions, nil
}

// applyMigration は 1 ファイルを適用し、適用済みとして記録する。
// SQL の実行と記録は同一トランザクションに入れ、片方だけが残らないようにする。
func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	body, err := migrations.FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		name, formatTime(time.Now()),
	); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}
