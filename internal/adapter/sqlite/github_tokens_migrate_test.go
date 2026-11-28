package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
)

// ---------------------------------------------------------------------------
// 利用者ごとの GitHub トークンを捨てる移行（ADR 0106、#497）
// ---------------------------------------------------------------------------

// dropGitHubTokensMigration は github_tokens を落とすマイグレーション。
// 確かめるテストは、直前（これを含まない）の状態から始める。
const dropGitHubTokensMigration = "0019_drop_github_tokens.sql"

// 保存してあったトークンは表ごと消え、ログインの記録（利用者とセッション）は
// 残る。トークンを残すと、使わなくなった生きた GitHub のトークンが DB に
// 居続ける。利用者まで消すと、ボードの持ち主やメンバーが指す先が無くなる。
func TestMigrate_DropsGitHubTokensAndKeepsLogins(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	migrateUpTo(t, db, dropGitHubTokensMigration)
	for _, stmt := range []string{
		`INSERT INTO users (id, provider, subject, login, display_name, created_at, updated_at)
		 VALUES ('user-1', 'github', '42', 'octocat', 'Octo Cat',
		         '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO sessions (token_hash, user_id, created_at, expires_at)
		 VALUES ('hash-1', 'user-1', '2026-01-01T00:00:00Z', '2026-02-01T00:00:00Z')`,
		`INSERT INTO github_tokens
		   (user_id, access_token, refresh_token, expires_at, refresh_expires_at, updated_at)
		 VALUES ('user-1', 'sealed-access', 'sealed-refresh', '', '', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var tables int
	if err := db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'github_tokens'`,
	).Scan(&tables); err != nil {
		t.Fatalf("count github_tokens: %v", err)
	}
	if tables != 0 {
		t.Error("github_tokens が残っている")
	}

	for table, want := range map[string]int{"users": 1, "sessions": 1} {
		var got int
		if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM `+table).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Errorf("%s の行 = %d, want %d", table, got, want)
		}
	}
}
