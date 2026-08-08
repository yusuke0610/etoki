package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/migrations"
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
	})
	if err != nil {
		t.Fatalf("seed board: %v", err)
	}
}

func item(localID, itemID string, kind port.ItemKind, parent *string) port.SyncItem {
	return port.SyncItem{
		ItemID:        itemID,
		Kind:          kind,
		Title:         "title-" + localID,
		LocalID:       localID,
		ParentLocalID: parent,
		CreatedAt:     baseTime,
	}
}

func ptr[T any](v T) *T { return &v }

// ---------------------------------------------------------------------------
// C-11: マイグレーション
// ---------------------------------------------------------------------------

func TestMigrate_IsIdempotent(t *testing.T) {
	t.Parallel()

	db := newDB(t) // 1 回目は newDB の中で適用済み

	// 適用済みの件数はマイグレーションを足すたびに変わる。件数そのものを
	// 期待値に書くと、SQL を 1 つ足しただけでこのテストが落ちる。確かめたいのは
	// 「2 回目で増えないこと」なので、前後を突き合わせる。
	before := countMigrations(t, db)

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("2 回目の Migrate: %v", err)
	}

	if after := countMigrations(t, db); after != before {
		t.Errorf("schema_migrations の件数 = %d, want %d", after, before)
	}
}

func countMigrations(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	if err := db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	return n
}

// 0002 以降は既存の boards に列を足す。移行後、それまでのボードは
// 「作成先が未選択」（ADR 0014）かつ「所有者が無い」（ADR 0016）として
// 読めなければならない。どちらも空文字がその状態を表す。
//
// 0001 だけ適用した状態を作ってから Migrate を呼ぶ。newDB は全部適用して
// しまうので、ここでは使えない。
func TestMigrate_ExistingBoardsAreUnselectedAndUnowned(t *testing.T) {
	t.Parallel()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	initial, err := migrations.FS.ReadFile("0001_initial.sql")
	if err != nil {
		t.Fatalf("read 0001: %v", err)
	}
	for _, stmt := range []string{
		string(initial),
		`CREATE TABLE schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations VALUES ('0001_initial.sql', '2026-01-01T00:00:00Z')`,
		`INSERT INTO boards (id, name, scene, created_at, updated_at)
		 VALUES ('legacy', '移行前のボード', '{}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.ExecContext(t.Context(), stmt); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	b, err := sqlite.NewBoardRepository(db).Find(t.Context(), "", "legacy")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if b == nil {
		t.Fatal("移行でボードが消えている")
	}
	if b.Target != (port.BoardTarget{}) {
		t.Errorf("Target = %+v, want ゼロ値（未選択）", b.Target)
	}
	if b.Target.Selected() {
		t.Error("未選択のはずが Selected() が true")
	}
	if b.OwnerUserID != "" {
		t.Errorf("OwnerUserID = %q, want 空文字（所有者なし）", b.OwnerUserID)
	}

	// 所有者が無いので、認証を有効にすると引き受けの対象として数えられる。
	n, err := sqlite.NewBoardRepository(db).CountUnowned(t.Context())
	if err != nil {
		t.Fatalf("CountUnowned: %v", err)
	}
	if n != 1 {
		t.Errorf("CountUnowned = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// C: MappingRepository
// ---------------------------------------------------------------------------

// C-1: 保存したものがそのまま読み戻せる。
func TestSaveRun_RoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	runID, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID:      "board-1",
		AnnotationID: "annot-1",
		ContentHash:  "hash-1",
		CreatedAt:    baseTime,
		Items: []port.SyncItem{
			item("e1", "PVTI_epic", port.KindEpic, nil),
			item("i1", "PVTI_i1", port.KindIssue, ptr("e1")),
		},
	})
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got == nil {
		t.Fatal("FindLatestRun returned nil")
	}

	if got.ID != runID {
		t.Errorf("ID = %d, want %d", got.ID, runID)
	}
	if got.ContentHash != "hash-1" {
		t.Errorf("ContentHash = %q, want %q", got.ContentHash, "hash-1")
	}
	if !got.CreatedAt.Equal(baseTime) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, baseTime)
	}
	if len(got.Items) != 2 {
		t.Fatalf("Items の件数 = %d, want 2", len(got.Items))
	}

	epic := got.Items[0]
	if epic.LocalID != "e1" || epic.Kind != port.KindEpic || epic.ItemID != "PVTI_epic" {
		t.Errorf("epic = %+v", epic)
	}
	if epic.ParentLocalID != nil {
		t.Errorf("epic.ParentLocalID = %v, want nil", *epic.ParentLocalID)
	}

	issue := got.Items[1]
	if issue.ParentLocalID == nil || *issue.ParentLocalID != "e1" {
		t.Errorf("issue.ParentLocalID = %v, want \"e1\"", issue.ParentLocalID)
	}
	if issue.RunID != runID {
		t.Errorf("issue.RunID = %d, want %d", issue.RunID, runID)
	}
}

// C-2: items が 0 件でも保存できる。
func TestSaveRun_NoItems(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID:      "board-1",
		AnnotationID: "annot-1",
		ContentHash:  "hash-1",
		CreatedAt:    baseTime,
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got == nil {
		t.Fatal("FindLatestRun returned nil")
	}
	if len(got.Items) != 0 {
		t.Errorf("Items の件数 = %d, want 0", len(got.Items))
	}
}

// C-3: 再実行すると run が積み上がり、最新が返る。過去の run も残る。
func TestSaveRun_KeepsHistory(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	first, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime,
		Items: []port.SyncItem{
			item("e1", "PVTI_old_e1", port.KindEpic, nil),
			item("i1", "PVTI_old_i1", port.KindIssue, ptr("e1")),
			item("i2", "PVTI_old_i2", port.KindIssue, ptr("e1")),
		},
	})
	if err != nil {
		t.Fatalf("1 回目の SaveRun: %v", err)
	}

	// 時刻はあえて 1 回目と同じにする。最新判定が created_at ではなく
	// id で行われていることを確かめるため。
	second, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-2", CreatedAt: baseTime,
		Items: []port.SyncItem{
			item("e1", "PVTI_new_e1", port.KindEpic, nil),
			item("i3", "PVTI_new_i3", port.KindIssue, ptr("e1")),
		},
	})
	if err != nil {
		t.Fatalf("2 回目の SaveRun: %v", err)
	}
	if second <= first {
		t.Fatalf("2 回目の run ID = %d, 1 回目 = %d（単調増加していない）", second, first)
	}

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got.ID != second {
		t.Errorf("最新 run の ID = %d, want %d", got.ID, second)
	}
	if got.ContentHash != "hash-2" {
		t.Errorf("ContentHash = %q, want %q", got.ContentHash, "hash-2")
	}
	if len(got.Items) != 2 {
		t.Errorf("最新 run の Items = %d 件, want 2", len(got.Items))
	}

	// 1 回目に作った 3 件が残っていること。GitHub 側に残っている draft issue を
	// 追跡できなくならないことが、案 C を採った理由そのもの。
	var n int
	if err := db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sync_items WHERE run_id = ?`, first).Scan(&n); err != nil {
		t.Fatalf("count items of first run: %v", err)
	}
	if n != 3 {
		t.Errorf("1 回目の run の items = %d 件, want 3（履歴が消えている）", n)
	}
}

// C-4: local_id が重複する items を渡すと run ごと保存されない。
func TestSaveRun_RejectsDuplicateLocalID(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	_, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime,
		Items: []port.SyncItem{
			item("e1", "PVTI_a", port.KindEpic, nil),
			item("e1", "PVTI_b", port.KindIssue, nil), // local_id が重複
		},
	})
	if err == nil {
		t.Fatal("SaveRun: want error for duplicate local_id, got nil")
	}

	assertNoRunPersisted(t, db, repo)
}

// C-5: 不正な kind を渡しても run ごと保存されない。
func TestSaveRun_RejectsInvalidKind(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	_, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime,
		Items: []port.SyncItem{
			item("e1", "PVTI_a", port.KindEpic, nil),
			item("p1", "PVTI_b", port.ItemKind("project"), nil), // ADR 0006 で不採用
		},
	})
	if err == nil {
		t.Fatal("SaveRun: want error for invalid kind, got nil")
	}

	assertNoRunPersisted(t, db, repo)
}

// 存在しないボードを参照する run は外部キー制約で弾かれ、何も残らない。
func TestSaveRun_RejectsUnknownBoard(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "no-such-board", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime,
	}); err == nil {
		t.Fatal("SaveRun: want foreign key error, got nil")
	}
}

// assertNoRunPersisted は run も items も 1 件も残っていないことを確かめる。
// 部分的に書き込まれた run が残ると 3 状態判定が「作成済み」と誤答するため、
// 失敗時の後始末が効いているかはここで確認する。
func assertNoRunPersisted(t *testing.T, db *sql.DB, repo *sqlite.MappingRepository) {
	t.Helper()

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got != nil {
		t.Errorf("失敗した run が残っている: %+v", got)
	}

	for _, table := range []string{"sync_runs", "sync_items"} {
		var n int
		if err := db.QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s に %d 件残っている, want 0", table, n)
		}
	}
}

// C-6: 一度も実行されていない注釈は (nil, nil)。
func TestFindLatestRun_NotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewMappingRepository(db)

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got != nil {
		t.Errorf("FindLatestRun = %+v, want nil", got)
	}
}

// C-7, C-8: ボード単位の一覧は注釈ごとに最新 run を 1 件ずつ返し、
// 他のボードのものは混ざらない。
func TestListLatestRunsByBoard(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")
	repo := sqlite.NewMappingRepository(db)

	save := func(boardID, annotationID, hash string, items ...port.SyncItem) {
		t.Helper()
		if _, err := repo.SaveRun(t.Context(), port.SyncRun{
			BoardID: boardID, AnnotationID: annotationID,
			ContentHash: hash, CreatedAt: baseTime, Items: items,
		}); err != nil {
			t.Fatalf("SaveRun(%s/%s): %v", boardID, annotationID, err)
		}
	}

	save("board-1", "annot-a", "old-a", item("e1", "PVTI_1", port.KindEpic, nil))
	save("board-1", "annot-a", "new-a", item("e1", "PVTI_2", port.KindEpic, nil))
	save("board-1", "annot-b", "only-b")
	save("board-2", "annot-a", "other-board", item("e1", "PVTI_3", port.KindEpic, nil))

	got, err := repo.ListLatestRunsByBoard(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListLatestRunsByBoard: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2 (%+v)", len(got), got)
	}

	// annotation_element_id の昇順で返る。
	if got[0].AnnotationID != "annot-a" || got[0].ContentHash != "new-a" {
		t.Errorf("got[0] = %+v, want annot-a / new-a", got[0])
	}
	if len(got[0].Items) != 1 || got[0].Items[0].ItemID != "PVTI_2" {
		t.Errorf("got[0].Items = %+v, want 最新 run の PVTI_2 のみ", got[0].Items)
	}
	if got[1].AnnotationID != "annot-b" || len(got[1].Items) != 0 {
		t.Errorf("got[1] = %+v, want annot-b / items なし", got[1])
	}
}

func TestListLatestRunsByBoard_EmptyBoard(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewMappingRepository(db)

	got, err := repo.ListLatestRunsByBoard(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListLatestRunsByBoard: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0", len(got))
	}
}

// C-9: ボードを削除すると run と items が CASCADE で消える。
// SQLite は既定で外部キーを検査しないため、これは PRAGMA foreign_keys の
// 設定が効いているかの確認でもある。
func TestDeleteBoard_CascadesToRunsAndItems(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime,
		Items: []port.SyncItem{item("e1", "PVTI_1", port.KindEpic, nil)},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	if _, err := db.ExecContext(t.Context(),
		`DELETE FROM boards WHERE id = ?`, "board-1"); err != nil {
		t.Fatalf("delete board: %v", err)
	}

	for _, table := range []string{"sync_runs", "sync_items"} {
		var n int
		if err := db.QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s に %d 件残っている, want 0（CASCADE が効いていない）", table, n)
		}
	}
}

// C-10: 同じ ProjectV2Item ID を別の run に記録できる。
// 再実行時、GitHub 上に残っている同じアイテムを参照しうるため。
func TestSaveRun_AllowsSameItemIDAcrossRuns(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	for i := range 2 {
		if _, err := repo.SaveRun(t.Context(), port.SyncRun{
			BoardID: "board-1", AnnotationID: "annot-1",
			ContentHash: "hash", CreatedAt: baseTime,
			Items: []port.SyncItem{item("e1", "PVTI_same", port.KindEpic, nil)},
		}); err != nil {
			t.Fatalf("SaveRun(%d 回目): %v", i+1, err)
		}
	}
}

// ---------------------------------------------------------------------------
// D: BoardRepository
// ---------------------------------------------------------------------------

// D-1: 保存したものがそのまま読み戻せる。
func TestBoard_RoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	want := port.Board{
		ID:        "board-1",
		Name:      "決済まわりのブレスト",
		Scene:     `{"elements":[{"id":"a"}]}`,
		CreatedAt: baseTime,
		UpdatedAt: baseTime,
	}
	if err := repo.Create(t.Context(), want); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("Find returned nil")
	}

	if got.ID != want.ID || got.Name != want.Name || got.Scene != want.Scene {
		t.Errorf("got = %+v, want %+v", *got, want)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
}

// D-2: シーン更新で updated_at は進み、created_at は変わらない。
func TestBoard_UpdateScene(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	later := baseTime.Add(time.Hour)
	if err := repo.UpdateScene(t.Context(), "", "board-1", `{"elements":["updated"]}`, later); err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Scene != `{"elements":["updated"]}` {
		t.Errorf("Scene = %q", got.Scene)
	}
	if !got.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, later)
	}
	if !got.CreatedAt.Equal(baseTime) {
		t.Errorf("CreatedAt = %v, want %v（更新で変わってはいけない）", got.CreatedAt, baseTime)
	}
}

func TestBoard_UpdateSceneNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	err := repo.UpdateScene(t.Context(), "", "no-such-board", "{}", baseTime)
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("UpdateScene = %v, want port.ErrNotFound", err)
	}
}

// D-3: 存在しない ID は (nil, nil)。
func TestBoard_FindNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)

	got, err := sqlite.NewBoardRepository(db).Find(t.Context(), "", "no-such-board")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != nil {
		t.Errorf("Find = %+v, want nil", got)
	}
}

// D-4: 一覧は更新時刻の降順。
func TestBoard_ListOrder(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	create := func(id string, updatedAt time.Time) {
		t.Helper()
		if err := repo.Create(t.Context(), port.Board{
			ID: id, Name: id, Scene: "{}",
			CreatedAt: baseTime, UpdatedAt: updatedAt,
		}); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}

	create("old", baseTime)
	create("newest", baseTime.Add(2*time.Hour))
	create("middle", baseTime.Add(time.Hour))

	got, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var ids []string
	for _, b := range got {
		ids = append(ids, b.ID)
	}
	want := []string{"newest", "middle", "old"}
	if len(ids) != len(want) {
		t.Fatalf("件数 = %d, want %d", len(ids), len(want))
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("順序 = %v, want %v", ids, want)
		}
	}
}

func TestBoard_ListEmpty(t *testing.T) {
	t.Parallel()

	got, err := sqlite.NewBoardRepository(newDB(t)).List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0", len(got))
	}
}

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
