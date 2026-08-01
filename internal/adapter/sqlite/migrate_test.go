package sqlite_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
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
