package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
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

// rebuildDirective は、外部キーで参照される親テーブルを作り直すマイグレーション
// であることを示す印。**ファイルの先頭行に単独で書く。**
//
// SQLite で CHECK 制約を変えるには「新しい表を作って INSERT ... SELECT し、
// 旧表を DROP して RENAME する」手順が要る。だが foreign_keys=ON のまま親表を
// DROP すると SQLite は暗黙の DELETE を発行し、子表の ON DELETE CASCADE が
// 発火して履歴ごと消える。切るには PRAGMA をトランザクションの**外**で
// 実行しなければならず（トランザクション内の PRAGMA foreign_keys は no-op）、
// 既定の適用経路では書けない。
//
// 印を Go 側のファイル名一覧ではなく SQL の側に置くのは、なぜ特別扱いなのかを
// マイグレーション本体と同じ場所に残すため（ADR 0049）。
const rebuildDirective = "-- etoki:rebuild-table"

// applyMigration は 1 ファイルを適用し、適用済みとして記録する。
//
// 印のあるファイルだけ外部キーを切る経路に回す。印が無いものはこれまでどおり
// 1 つのトランザクションで適用する。**既定は安全な側**にしてある。
func applyMigration(ctx context.Context, db *sql.DB, name string) error {
	body, err := migrations.FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	if hasRebuildDirective(string(body)) {
		return applyRebuild(ctx, db, name, string(body))
	}

	return applyInTx(ctx, db, name, string(body))
}

// hasRebuildDirective は先頭行が印そのものかを見る。
//
// 前方一致では見ない。前方一致にすると "-- etoki:rebuild-table-later" のような
// 別物の行が黙って外部キーを切る経路に入る。
func hasRebuildDirective(body string) bool {
	first, _, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(first) == rebuildDirective
}

// applyInTx は既定の適用経路。SQL の実行と記録を同一トランザクションに入れ、
// 片方だけが残らないようにする。
func applyInTx(ctx context.Context, db *sql.DB, name, body string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}

	if err := recordMigration(ctx, tx, name); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}

// applyRebuild は外部キーを切った状態で 1 ファイルを適用する（ADR 0049）。
//
// SQLite が案内するテーブル再作成の手順に従い、切る／戻すの 2 つだけを
// トランザクションの外に出す。作り直しそのものと記録は、これまでと同じく
// 1 つのトランザクションの中で行う。
func applyRebuild(ctx context.Context, db *sql.DB, name, body string) error {
	// **接続を 1 本に固定する。** PRAGMA は接続ごとに効き、database/sql は
	// プールから接続を配る。db.ExecContext で切っても、続くトランザクションが
	// 別の接続に載れば外部キーは ON のままで、親表の DROP が子表を消す。
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("pin connection for migration %s: %w", name, err)
	}
	defer func() { _ = conn.Close() }()

	if err := setForeignKeys(ctx, conn, false); err != nil {
		return fmt.Errorf("disable foreign keys for migration %s: %w", name, err)
	}

	applyErr := rebuildInTx(ctx, conn, name, body)

	// **成功しても失敗しても戻す。** 切ったままプールへ帰した接続は、以後の
	// ON DELETE CASCADE を黙って効かなくする。戻せなかったことも握り潰さない。
	restoreErr := setForeignKeys(ctx, conn, true)

	if applyErr != nil {
		return applyErr
	}
	if restoreErr != nil {
		return fmt.Errorf("restore foreign keys after migration %s: %w", name, restoreErr)
	}

	return nil
}

// rebuildInTx は作り直しの SQL と記録を 1 トランザクションで適用する。
func rebuildInTx(ctx context.Context, conn *sql.Conn, name, body string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}

	if err := recordMigration(ctx, tx, name); err != nil {
		return err
	}

	// **COMMIT の前に見る。** 外部キーを切っている区間なので、参照が壊れても
	// 書き込みの側はエラーにならない。ここで見つけて、何も書かずに止める。
	if err := checkForeignKeys(ctx, tx); err != nil {
		return fmt.Errorf("migration %s: %w", name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}

// recordMigration は適用済みとして記録する。呼び出し側のトランザクションに
// 入れることで、SQL だけが通って記録が残らない状態を作らない。
func recordMigration(ctx context.Context, tx *sql.Tx, name string) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		name, formatTime(time.Now()),
	); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}

	return nil
}

// setForeignKeys は接続 1 本の外部キー検査を切り替え、**切り替わったことを
// 読み返して確かめる。**
//
// PRAGMA foreign_keys はトランザクションの中では no-op になり、そのときエラーも
// 返さない。黙って ON のまま親表を DROP すると子表の行が消えるので、
// 「効かなかった」を必ずエラーにする。
func setForeignKeys(ctx context.Context, conn *sql.Conn, on bool) error {
	stmt, want := `PRAGMA foreign_keys=OFF`, 0
	if on {
		stmt, want = `PRAGMA foreign_keys=ON`, 1
	}

	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("exec %s: %w", stmt, err)
	}

	var got int
	if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&got); err != nil {
		return fmt.Errorf("read back foreign_keys: %w", err)
	}
	if got != want {
		return fmt.Errorf("foreign_keys = %d, want %d", got, want)
	}

	return nil
}

// checkForeignKeys は壊れた外部キー参照が残っていないことを確かめる。
//
// PRAGMA foreign_key_check は違反を行として返す（エラーにはしない）ので、
// 1 行でも返ったかどうかで判断する。
func checkForeignKeys(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return fmt.Errorf("foreign_key_check: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if rows.Next() {
		// 列は table / rowid / parent / fkid。どこが壊れたかを添えるだけなので、
		// 読めなかったとしても「1 行返った＝違反がある」という判断は変えない。
		var table, parent string
		var rowid sql.NullInt64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return fmt.Errorf("foreign key violation (details unreadable): %w", err)
		}
		return fmt.Errorf("foreign key violation in %s referencing %s", table, parent)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate foreign_key_check: %w", err)
	}

	return nil
}
