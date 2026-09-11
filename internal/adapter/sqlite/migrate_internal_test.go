package sqlite

// checkForeignKeys は公開していないので、ここだけ package sqlite の中から見る。
// 他のテストは package sqlite_test にある。外から確かめるには、壊れた参照を
// 残すマイグレーションを埋め込み FS に足すことになり、本番にも配られてしまう。

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// 壊れた外部キー参照が残っていたら、COMMIT の前に見つけて止める。
//
// 作り直しの区間は外部キーを切っているので、参照が壊れても書き込みの側は
// エラーにならない（ADR 0049）。ここが見落とすと、履歴の一部が親を失ったまま
// commit される。
func TestCheckForeignKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// 参照が壊れた行は、外部キーを切っている間しか作れない。作り直しの区間を
	// そのまま再現する。
	if err := setForeignKeys(ctx, conn, false); err != nil {
		t.Fatalf("setForeignKeys(false): %v", err)
	}
	t.Cleanup(func() { _ = setForeignKeys(ctx, conn, true) })

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	// 親の居ない sync_item。存在しない run_id を指す。
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sync_items (run_id, item_id, kind, title, body, local_id,
		                         action, created_at)
		 VALUES (999, 'node-1', 'issue', 'title', 'body', 'i1', 'created',
		         '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert dangling sync_item: %v", err)
	}

	err = checkForeignKeys(ctx, tx)
	if err == nil {
		t.Fatal("壊れた参照を見逃した")
	}
	// どの表が壊れたかを添えていること。止めるだけでは、直す側が探しに行けない。
	if got := err.Error(); !strings.Contains(got, "sync_items") {
		t.Errorf("エラーに壊れた表の名前が無い: %q", got)
	}
}

// 参照が壊れていなければ通る。禁じる側だけを見ると、常にエラーを返す実装でも
// 緑になり、正常なマイグレーションが全部止まる。
func TestCheckForeignKeys_Clean(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	if err := checkForeignKeys(ctx, tx); err != nil {
		t.Errorf("checkForeignKeys = %v, want nil", err)
	}
}

// 印は先頭行が印そのものであることを要求する。
//
// **前方一致にしない。** 似た行が黙って外部キーを切る経路に入ると、親テーブルを
// 作り直すつもりの無いマイグレーションで子テーブルの守りが外れる。逆に、印を
// 書いたつもりで 2 行目に置いた場合は既定（安全な側）に落ちる。
func TestHasRebuildDirective(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"先頭行が印":       {"-- etoki:rebuild-table\nCREATE TABLE x (a);\n", true},
		"印だけで改行が無い":   {"-- etoki:rebuild-table", true},
		"前後に空白":       {"  -- etoki:rebuild-table  \nCREATE TABLE x (a);\n", true},
		"CRLF":        {"-- etoki:rebuild-table\r\nCREATE TABLE x (a);\n", true},
		"似ているが別物":     {"-- etoki:rebuild-table-later\nCREATE TABLE x (a);\n", false},
		"2 行目に書いてある":  {"-- 説明\n-- etoki:rebuild-table\n", false},
		"印が無い":        {"ALTER TABLE x ADD COLUMN a TEXT;\n", false},
		"行の途中に混ざっている": {"CREATE TABLE x (a); -- etoki:rebuild-table\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := hasRebuildDirective(tc.body); got != tc.want {
				t.Errorf("hasRebuildDirective = %v, want %v", got, tc.want)
			}
		})
	}
}
