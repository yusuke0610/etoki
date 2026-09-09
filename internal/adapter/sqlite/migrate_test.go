package sqlite_test

import (
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/migrations"
	"github.com/yusuke0610/etoki/port"
)

// 一度もマイグレーションしていない DB は未適用として検出される。
//
// SQLite は接続時にファイルを作るため、空の DB でもサーバーは起動できて
// しまう。そのまま動かすと全 API が 500 を返し続けるので、起動時に弾く。
func TestEnsureMigrated_FreshDatabase(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	err = sqlite.EnsureMigrated(t.Context(), db)
	if !errors.Is(err, sqlite.ErrNotMigrated) {
		t.Errorf("EnsureMigrated = %v, want ErrNotMigrated", err)
	}
}

// 適用済みなら通る。
func TestEnsureMigrated_AfterMigrate(t *testing.T) {
	t.Parallel()

	db := newDB(t) // newDB は Migrate 済み

	if err := sqlite.EnsureMigrated(t.Context(), db); err != nil {
		t.Errorf("EnsureMigrated = %v, want nil", err)
	}
}

// schema_migrations はあるが記録が消えている場合も未適用として扱う。
// 手で DB をいじった後などに、中途半端な状態で起動しないようにする。
func TestEnsureMigrated_MissingRecord(t *testing.T) {
	t.Parallel()

	db := newDB(t)

	if _, err := db.ExecContext(t.Context(), `DELETE FROM schema_migrations`); err != nil {
		t.Fatalf("delete schema_migrations: %v", err)
	}

	err := sqlite.EnsureMigrated(t.Context(), db)
	if !errors.Is(err, sqlite.ErrNotMigrated) {
		t.Errorf("EnsureMigrated = %v, want ErrNotMigrated", err)
	}
}

// ---------------------------------------------------------------------------
// 外部キーを切って適用するマイグレーション（ADR 0046）
// ---------------------------------------------------------------------------

// rebuildMigration は外部キーを切って適用する最初のマイグレーション。
// この経路を確かめるテストは、直前（これを含まない）の状態から始める。
const rebuildMigration = "0011_sync_run_outcome_check.sql"

// migrateUpTo は name より前のマイグレーションだけを適用し、適用済みとして
// 記録する。newDB は全部適用してしまうので、「適用の仕方そのもの」を
// 確かめたいときの手前の状態はこれで作る。
func migrateUpTo(t *testing.T, db *sql.DB, name string) {
	t.Helper()

	if _, err := db.ExecContext(t.Context(),
		`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
	); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}

	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	slices.Sort(names)

	for _, n := range names {
		if n >= name {
			break
		}
		body, err := migrations.FS.ReadFile(n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		if _, err := db.ExecContext(t.Context(), string(body)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
		if _, err := db.ExecContext(t.Context(),
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, '2026-01-01T00:00:00Z')`,
			n,
		); err != nil {
			t.Fatalf("record %s: %v", n, err)
		}
	}
}

// countRows は 1 つの数を数えるだけの問い合わせを実行する。
func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()

	var n int
	if err := db.QueryRowContext(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// 親テーブルを作り直すマイグレーションが、子テーブルの行を 1 つも消さない。
//
// **これがこの仕組みの存在理由。** sync_runs は sync_items から
// ON DELETE CASCADE 付きで参照されるので、foreign_keys=ON のまま DROP TABLE
// すると暗黙の DELETE が CASCADE を発火させ、実行履歴（ADR 0007）と作成済み
// draft issue の追跡情報が黙って消える。applyMigration の分岐を外して常に
// トランザクション経路へ戻すと、このテストは sync_items が 0 件になって落ちる。
func TestMigrate_RebuildKeepsChildRows(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrateUpTo(t, db, rebuildMigration)
	seedBoard(t, db, "board-1")

	// 途中で失敗した run を 1 つ入れる。列の写し忘れは、既定値を持たない
	// outcome / error でいちばん出やすい。
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO sync_runs (id, board_id, annotation_element_id, content_hash,
		                        created_at, outcome, error)
		 VALUES (7, 'board-1', 'annot-1', 'hash-1', '2026-01-01T00:00:00Z',
		         'incomplete', 'boom')`,
	); err != nil {
		t.Fatalf("seed sync_run: %v", err)
	}
	for _, localID := range []string{"e1", "i1"} {
		if _, err := db.ExecContext(t.Context(),
			`INSERT INTO sync_items (run_id, item_id, kind, title, body, local_id,
			                         action, created_at)
			 VALUES (7, 'node-'||?, 'issue', 'title', 'body', ?, 'created',
			         '2026-01-01T00:00:00Z')`,
			localID, localID,
		); err != nil {
			t.Fatalf("seed sync_item %s: %v", localID, err)
		}
	}

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM sync_items`); n != 2 {
		t.Errorf("sync_items = %d 件, want 2（作り直しで子テーブルが消えている）", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM sync_runs`); n != 1 {
		t.Errorf("sync_runs = %d 件, want 1", n)
	}

	// 移し替えた列がそのまま残っていること。id も引き継ぐ（子テーブルの
	// run_id が指す先であり、最新の run を決める鍵でもある）。
	var id int64
	var outcome, failure string
	if err := db.QueryRowContext(t.Context(),
		`SELECT id, outcome, error FROM sync_runs`).Scan(&id, &outcome, &failure); err != nil {
		t.Fatalf("select sync_runs: %v", err)
	}
	if id != 7 || outcome != "incomplete" || failure != "boom" {
		t.Errorf("run = (id=%d, outcome=%q, error=%q), want (7, \"incomplete\", \"boom\")",
			id, outcome, failure)
	}

	// 作り直したテーブルにインデックスが戻っていること。DROP で一緒に消えるので、
	// 張り直しを書き忘れると最新 run の引き当てが全走査になる。
	if n := countRows(t, db,
		`SELECT COUNT(*) FROM sqlite_master
		  WHERE type = 'index' AND name = 'idx_sync_runs_latest'`); n != 1 {
		t.Error("idx_sync_runs_latest が張り直されていない")
	}
}

// AUTOINCREMENT の水位を引き継ぐ。DROP TABLE は sqlite_sequence の行も消すので、
// 引き継がないと、削除済みボードの run が使った id を新しい run が再利用する。
// 「最新の run は created_at ではなく id で決める」（0001）が依っているのは
// id が単調増加すること。
func TestMigrate_RebuildKeepsAutoincrementWatermark(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrateUpTo(t, db, rebuildMigration)
	seedBoard(t, db, "board-keep")
	seedBoard(t, db, "board-gone")

	// 残るボードの run より後ろの id を、消えるボードの run が使う。ボードごと
	// 消すと、水位（200）だけが残って行は無くなる。
	for _, seed := range []struct {
		id      int64
		boardID string
	}{{100, "board-keep"}, {200, "board-gone"}} {
		if _, err := db.ExecContext(t.Context(),
			`INSERT INTO sync_runs (id, board_id, annotation_element_id, content_hash,
			                        created_at, outcome)
			 VALUES (?, ?, 'annot-1', 'h', '2026-01-01T00:00:00Z', 'complete')`,
			seed.id, seed.boardID,
		); err != nil {
			t.Fatalf("seed sync_run %d: %v", seed.id, err)
		}
	}
	if _, err := db.ExecContext(t.Context(),
		`DELETE FROM boards WHERE id = 'board-gone'`); err != nil {
		t.Fatalf("delete board-gone: %v", err)
	}

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	id, err := sqlite.NewMappingRepository(db).SaveRun(t.Context(), port.SyncRun{
		BoardID:      "board-keep",
		AnnotationID: "annot-1",
		ContentHash:  "hash-next",
		CreatedAt:    baseTime,
		Outcome:      port.OutcomeComplete,
	})
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if id <= 200 {
		t.Errorf("新しい run の id = %d, want > 200（消えた run の id を使い回している）", id)
	}
}

// 作り直しのあと、外部キー検査が接続に戻っている。
//
// **接続を 1 本に絞って確かめる。** 切ったままプールへ帰った接続は、以後の
// ON DELETE CASCADE を黙って効かなくする。プールに複数あると、戻し忘れた
// 接続を引かない回ができてしまう。
func TestMigrate_RebuildRestoresForeignKeys(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var on int
	if err := db.QueryRowContext(t.Context(), `PRAGMA foreign_keys`).Scan(&on); err != nil {
		t.Fatalf("PRAGMA foreign_keys: %v", err)
	}
	if on != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1（切ったまま戻していない）", on)
	}

	// 値だけでなく、消える側の挙動でも見る。ボードを消したら run も item も
	// 消えることが、外部キーが効いていることの意味（ADR 0042）。
	seedBoard(t, db, "board-1")
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO sync_runs (id, board_id, annotation_element_id, content_hash,
		                        created_at, outcome)
		 VALUES (1, 'board-1', 'annot-1', 'h', '2026-01-01T00:00:00Z', 'complete')`,
	); err != nil {
		t.Fatalf("seed sync_run: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO sync_items (run_id, item_id, kind, title, body, local_id,
		                         action, created_at)
		 VALUES (1, 'node-1', 'issue', 'title', 'body', 'i1', 'created',
		         '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed sync_item: %v", err)
	}

	if _, err := db.ExecContext(t.Context(),
		`DELETE FROM boards WHERE id = 'board-1'`); err != nil {
		t.Fatalf("delete board: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM sync_runs`); n != 0 {
		t.Errorf("sync_runs = %d 件, want 0（カスケードが効いていない）", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM sync_items`); n != 0 {
		t.Errorf("sync_items = %d 件, want 0（カスケードが効いていない）", n)
	}
}
