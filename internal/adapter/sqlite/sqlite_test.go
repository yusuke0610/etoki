package sqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// baseTime は時刻依存でテストがぶれないよう固定した基準時刻。
var baseTime = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

// newDB は一時ディレクトリに DB を作り、マイグレーションを適用して返す。
func newDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return db
}

// seedBoard はテスト用のボードを 1 枚作る。sync_runs は boards を外部キーで
// 参照するため、run を保存するテストでは先にこれが必要になる。
func seedBoard(t *testing.T, db *sql.DB, id string) {
	t.Helper()

	err := sqlite.NewBoardRepository(db).Create(t.Context(), port.Board{
		ID:        id,
		Name:      "テストボード",
		Scene:     `{"elements":[]}`,
		CreatedAt: baseTime,
		UpdatedAt: baseTime,
	}, "")
	if err != nil {
		t.Fatalf("seed board: %v", err)
	}
}

func item(localID, itemID string, kind port.ItemKind, parent *string) port.SyncItem {
	return port.SyncItem{
		ItemID:        itemID,
		Kind:          kind,
		Title:         "title-" + localID,
		Body:          "body-" + localID,
		LocalID:       localID,
		ParentLocalID: parent,
		Action:        port.ActionCreated,
		Confirmed:     true,
		CreatedAt:     baseTime,
	}
}

// unconfirmed は届いたか分からない書き込み 1 件（ADR 0056）。
//
// 作成では item ID が分からないので空文字。更新では相手の ID は分かっている
// ので、呼び出し側が渡す。
func unconfirmed(localID, itemID string, action port.SyncAction) port.SyncItem {
	it := item(localID, itemID, port.KindIssue, nil)
	it.Action = action
	it.Confirmed = false
	return it
}

func ptr[T any](v T) *T { return &v }

// Open が返す DB がキャンセル済み context でも壊れないことの確認。
func TestOpen_RejectsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "etoki.db"))
	if err == nil {
		t.Error("Open: want error for canceled context, got nil")
	}
}
