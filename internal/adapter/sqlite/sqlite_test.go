package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
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
	if b.Board.Target != (port.BoardTarget{}) {
		t.Errorf("Target = %+v, want ゼロ値（未選択）", b.Board.Target)
	}
	if b.Board.Target.Selected() {
		t.Error("未選択のはずが Selected() が true")
	}
	// 0005 は所有者をメンバー行に移す。空文字の所有者は「認証なしの所有者」
	// 1 人としてそのまま owner になる（ADR 0016 / 0017）。
	if b.Role != port.RoleOwner {
		t.Errorf("Role = %q, want %q", b.Role, port.RoleOwner)
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
	// body は GitHub から取り直せない。往復で落ちると二度と分からなくなる
	// （ADR 0023）。
	if epic.Title != "title-e1" || epic.Body != "body-e1" {
		t.Errorf("epic.Title = %q, epic.Body = %q", epic.Title, epic.Body)
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

// body を記録していなかった頃に作られた行も読める。0007 が足した既定値の
// 空文字が効いていることを固定する。空文字は「読むものが無い」として扱う。
func TestFindLatestRun_ItemWithoutBody(t *testing.T) {
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

	// 移行前の INSERT を再現する。body を書かずに入れられることそのものが、
	// 既存の行が読めることの条件。
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO sync_items (run_id, item_id, kind, title, local_id, created_at)
		 VALUES ((SELECT MAX(id) FROM sync_runs), 'PVTI_old', 'epic', '古い item', 'e1', ?)`,
		baseTime.UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert legacy sync_item: %v", err)
	}

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got == nil || len(got.Items) != 1 {
		t.Fatalf("Items = %+v, want 1 件", got)
	}
	if got.Items[0].Body != "" {
		t.Errorf("Body = %q, want 空文字", got.Items[0].Body)
	}
}

// C-3b: 履歴を item_id で畳むと「いま GitHub に在るもの」が出る（ADR 0026）。
//
// **最新 run の items ではない。** 最新 run だけを見ると、更新の run のあとに
// 「前回作ったが今回は触らなかった」item が消える。GitHub 側には残っているのに
// etoki が見せなくなるのは、状態を見せるという方針に反する（中核思想 3）。
func TestListItemsByAnnotation_FoldsHistory(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	// run1: A と B を作る。
	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime,
		Items: []port.SyncItem{
			item("e1", "PVTI_a", port.KindEpic, nil),
			item("i1", "PVTI_b", port.KindIssue, ptr("e1")),
		},
	}); err != nil {
		t.Fatalf("run1: %v", err)
	}

	// run2: A を更新し、C を新しく作る。B は触らない。
	updated := item("x1", "PVTI_a", port.KindEpic, nil)
	updated.Title = "書き直したタイトル"
	updated.Body = "書き直した本文"
	updated.Action = port.ActionUpdated

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-2", CreatedAt: baseTime,
		Items: []port.SyncItem{
			updated,
			item("x2", "PVTI_c", port.KindIssue, ptr("x1")),
		},
	}); err != nil {
		t.Fatalf("run2: %v", err)
	}

	items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListItemsByAnnotation: %v", err)
	}

	// **並びは最初に作られた順。** 更新しても末尾へ動かさない。中身を書き換えた
	// だけで並びが変わると、同じものを見ていることを確かめ直すことになる。
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ItemID)
	}
	if want := []string{"PVTI_a", "PVTI_b", "PVTI_c"}; !slices.Equal(ids, want) {
		t.Fatalf("item = %v, want %v", ids, want)
	}

	// A は更新後の中身になる。
	if items[0].Title != "書き直したタイトル" || items[0].Body != "書き直した本文" {
		t.Errorf("A = %q/%q, want 更新後の中身", items[0].Title, items[0].Body)
	}
	if items[0].Action != port.ActionUpdated {
		t.Errorf("A の action = %q, want %q", items[0].Action, port.ActionUpdated)
	}

	// B は触っていないので run1 のまま残る。ここが消えるのがいちばん困る。
	if items[1].Title != "title-i1" {
		t.Errorf("B = %q, want run1 のまま", items[1].Title)
	}
	if items[1].Action != port.ActionCreated {
		t.Errorf("B の action = %q, want %q", items[1].Action, port.ActionCreated)
	}
}

// 一度も実行していない注釈では空。nil ではなく空スライスを返す。
func TestListItemsByAnnotation_Empty(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListItemsByAnnotation: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items = %v, want 空", items)
	}
}

// 畳み込みは注釈ごと。別の注釈や別のボードの item を混ぜない。
func TestListItemsByAnnotation_ScopedToAnnotation(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")
	repo := sqlite.NewMappingRepository(db)

	for _, run := range []port.SyncRun{
		{BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "h", CreatedAt: baseTime,
			Items: []port.SyncItem{item("e1", "PVTI_mine", port.KindEpic, nil)}},
		{BoardID: "board-1", AnnotationID: "annot-2", ContentHash: "h", CreatedAt: baseTime,
			Items: []port.SyncItem{item("e1", "PVTI_other_annot", port.KindEpic, nil)}},
		{BoardID: "board-2", AnnotationID: "annot-1", ContentHash: "h", CreatedAt: baseTime,
			Items: []port.SyncItem{item("e1", "PVTI_other_board", port.KindEpic, nil)}},
	} {
		if _, err := repo.SaveRun(t.Context(), run); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListItemsByAnnotation: %v", err)
	}
	if len(items) != 1 || items[0].ItemID != "PVTI_mine" {
		t.Errorf("items = %+v, want PVTI_mine だけ", items)
	}
}

// ボード全体の畳み込みと注釈ごとの畳み込みは、同じ注釈について同じものを返す。
//
// **回帰止め。切れると何が起きるか。** 注釈パネルは ListItemsByAnnotation、
// ボードを開いたときの一覧は ListItemsByBoard を通る。畳み込みの規則
// （ADR 0026）が 2 通りになると「注釈を開くと出るがボード一覧には出ない」が
// 起きる。**どちらの画面も同じものを見せているつもりなので、食い違いに
// 気づくのは開発者ではなく利用者になる。**
//
// 更新と取り残しを両方含めるのは、畳み込みが効いていない状態でも件数だけは
// 一致してしまうため。
func TestListItemsByBoard_AgreesWithListItemsByAnnotation(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")
	repo := sqlite.NewMappingRepository(db)

	updated := item("x1", "PVTI_a", port.KindEpic, nil)
	updated.Title = "書き直したタイトル"
	updated.Action = port.ActionUpdated

	for _, run := range []port.SyncRun{
		{BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "h1", CreatedAt: baseTime,
			Items: []port.SyncItem{
				item("e1", "PVTI_a", port.KindEpic, nil),
				item("i1", "PVTI_b", port.KindIssue, ptr("e1")),
			}},
		// PVTI_a を更新し、PVTI_c を足す。PVTI_b は触らない。
		{BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "h2", CreatedAt: baseTime,
			Items: []port.SyncItem{
				updated,
				item("x2", "PVTI_c", port.KindIssue, ptr("x1")),
			}},
		{BoardID: "board-1", AnnotationID: "annot-2", ContentHash: "h", CreatedAt: baseTime,
			Items: []port.SyncItem{item("e1", "PVTI_d", port.KindEpic, nil)}},
		{BoardID: "board-2", AnnotationID: "annot-1", ContentHash: "h", CreatedAt: baseTime,
			Items: []port.SyncItem{item("e1", "PVTI_other_board", port.KindEpic, nil)}},
	} {
		if _, err := repo.SaveRun(t.Context(), run); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	byBoard, err := repo.ListItemsByBoard(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListItemsByBoard: %v", err)
	}

	// 別のボードの注釈を混ぜない。
	annotations := slices.Sorted(maps.Keys(byBoard))
	if want := []string{"annot-1", "annot-2"}; !slices.Equal(annotations, want) {
		t.Fatalf("注釈 = %v, want %v", annotations, want)
	}

	for _, annotationID := range annotations {
		items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", annotationID)
		if err != nil {
			t.Fatalf("ListItemsByAnnotation(%s): %v", annotationID, err)
		}
		if !reflect.DeepEqual(byBoard[annotationID], items) {
			t.Errorf("%s: ボード全体 = %+v, 注釈ごと = %+v",
				annotationID, byBoard[annotationID], items)
		}
	}

	// 畳み込みが効いていることを、片方の結果で確かめる。効いていなければ
	// 上の一致だけは通ってしまう。
	var ids []string
	for _, it := range byBoard["annot-1"] {
		ids = append(ids, it.ItemID)
	}
	if want := []string{"PVTI_a", "PVTI_b", "PVTI_c"}; !slices.Equal(ids, want) {
		t.Errorf("annot-1 の item = %v, want %v", ids, want)
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

// 履歴は畳まずに 1 回ずつ、新しい順で返る（ADR 0007）。
//
// **並びは id の降順。** created_at で並べると、同じ時刻の run（時刻は呼び出し
// 側が与える）で順序が定まらない。ここが切れると、履歴の「最新」が実行順と
// ずれても気づけない。
func TestListRunsByAnnotation_NewestFirst(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	save := func(annotationID, hash string, items ...port.SyncItem) {
		t.Helper()
		if _, err := repo.SaveRun(t.Context(), port.SyncRun{
			BoardID: "board-1", AnnotationID: annotationID,
			// 同じ時刻で積む。並びが時刻に依存していれば、ここで崩れる。
			ContentHash: hash, CreatedAt: baseTime, Items: items,
		}); err != nil {
			t.Fatalf("SaveRun(%s): %v", annotationID, err)
		}
	}

	save("annot-a", "h1", item("e1", "PVTI_1", port.KindEpic, nil))
	save("annot-a", "h2", item("e1", "PVTI_2", port.KindEpic, nil))
	save("annot-b", "other", item("e1", "PVTI_3", port.KindEpic, nil))

	got, err := repo.ListRunsByAnnotation(t.Context(), "board-1", "annot-a", 10)
	if err != nil {
		t.Fatalf("ListRunsByAnnotation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2 (%+v)", len(got), got)
	}
	if got[0].ContentHash != "h2" || got[1].ContentHash != "h1" {
		t.Errorf("並び = %q, %q, want h2, h1（新しい順）",
			got[0].ContentHash, got[1].ContentHash)
	}
	// 畳まない。1 回ずつの記録なので、その run で触った item だけが載る。
	if len(got[0].Items) != 1 || got[0].Items[0].ItemID != "PVTI_2" {
		t.Errorf("got[0].Items = %+v, want PVTI_2 のみ", got[0].Items)
	}
	if got[0].ID <= got[1].ID {
		t.Errorf("ID = %d, %d, want 降順", got[0].ID, got[1].ID)
	}
}

// 別の注釈の run を混ぜない。絞りを外すと、ボード内の全 run が 1 つの注釈の
// 履歴として並ぶ。
func TestListRunsByAnnotation_ScopedToAnnotationAndBoard(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")
	repo := sqlite.NewMappingRepository(db)

	for _, run := range []port.SyncRun{
		{BoardID: "board-1", AnnotationID: "annot-a", ContentHash: "keep", CreatedAt: baseTime},
		{BoardID: "board-1", AnnotationID: "annot-b", ContentHash: "other-annot", CreatedAt: baseTime},
		{BoardID: "board-2", AnnotationID: "annot-a", ContentHash: "other-board", CreatedAt: baseTime},
	} {
		if _, err := repo.SaveRun(t.Context(), run); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	got, err := repo.ListRunsByAnnotation(t.Context(), "board-1", "annot-a", 10)
	if err != nil {
		t.Fatalf("ListRunsByAnnotation: %v", err)
	}
	if len(got) != 1 || got[0].ContentHash != "keep" {
		t.Fatalf("got = %+v, want board-1/annot-a の 1 件だけ", got)
	}
}

// 上限は「新しいほうから」効く。古いほうから切ると、直前に何をしたかを
// 辿るという目的にいちばん要る run が落ちる。
func TestListRunsByAnnotation_LimitKeepsNewest(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	for _, hash := range []string{"h1", "h2", "h3"} {
		if _, err := repo.SaveRun(t.Context(), port.SyncRun{
			BoardID: "board-1", AnnotationID: "annot-a",
			ContentHash: hash, CreatedAt: baseTime,
		}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	got, err := repo.ListRunsByAnnotation(t.Context(), "board-1", "annot-a", 2)
	if err != nil {
		t.Fatalf("ListRunsByAnnotation: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2", len(got))
	}
	if got[0].ContentHash != "h3" || got[1].ContentHash != "h2" {
		t.Errorf("並び = %q, %q, want h3, h2", got[0].ContentHash, got[1].ContentHash)
	}
}

func TestListRunsByAnnotation_Empty(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")

	got, err := sqlite.NewMappingRepository(db).
		ListRunsByAnnotation(t.Context(), "board-1", "annot-a", 10)
	if err != nil {
		t.Fatalf("ListRunsByAnnotation: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, want 空", got)
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

// C-9: ボードを削除すると run と items とメンバーが CASCADE で消える。
// SQLite は既定で外部キーを検査しないため、これは PRAGMA foreign_keys の
// 設定が効いているかの確認でもある。
func TestDeleteBoard_CascadesToRunsItemsAndMembers(t *testing.T) {
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

	for _, table := range []string{"sync_runs", "sync_items", "board_members"} {
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
	if err := repo.Create(t.Context(), want, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("Find returned nil")
	}

	if got.Board.ID != want.ID || got.Board.Name != want.Name || got.Board.Scene != want.Scene {
		t.Errorf("got = %+v, want %+v", got.Board, want)
	}
	if !got.Board.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.Board.CreatedAt, want.CreatedAt)
	}
}

// D-1a: 作成先の表示名も一緒に読み戻せる（ADR 0019）。
//
// project_id は不透明な node ID なので、一覧を Project 名でまとめるには
// 選んだ時点の番号と名前が要る。URL も同じ扱いで、GitHub へ辿る導線に要る
// （ADR 0025）。Create と UpdateTarget の両方で残ること。
func TestBoard_TargetDisplayNameRoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	created := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_1",
		ProjectNumber:   3,
		ProjectTitle:    "ロードマップ",
		ProjectURL:      "https://github.com/orgs/acme/projects/3",
	}
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "b", Scene: "{}", Target: created,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target != created {
		t.Errorf("Target = %+v, want %+v", got.Board.Target, created)
	}

	updated := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "api",
		ProjectID:       "PVT_2",
		ProjectNumber:   7,
		ProjectTitle:    "技術的負債",
		ProjectURL:      "https://github.com/users/acme/projects/7",
	}
	if err := repo.UpdateTarget(t.Context(), "", "board-1", updated, baseTime); err != nil {
		t.Fatalf("UpdateTarget: %v", err)
	}

	got, err = repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target != updated {
		t.Errorf("Target = %+v, want %+v", got.Board.Target, updated)
	}
}

// D-1a2: 表示名だけを取り直せる。作成先そのものは動かない（ADR 0037）。
//
// 固定後に通る唯一の経路なので、ここが作成先まで書けるようになると
// ADR 0014 の固定が意味を失う。
func TestBoard_UpdateTargetDisplayKeepsTarget(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	target := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_1",
		ProjectNumber:   3,
		ProjectTitle:    "ロードマップ",
		ProjectURL:      "https://github.com/orgs/acme/projects/3",
	}
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "b", Scene: "{}", Target: target,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	display := port.BoardTargetDisplay{
		ProjectNumber: 4,
		ProjectTitle:  "改名後のロードマップ",
		ProjectURL:    "https://github.com/orgs/acme/projects/4",
	}
	if err := repo.UpdateTargetDisplay(t.Context(), "", "board-1", display, baseTime); err != nil {
		t.Fatalf("UpdateTargetDisplay: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	want := target
	want.ProjectNumber = display.ProjectNumber
	want.ProjectTitle = display.ProjectTitle
	want.ProjectURL = display.ProjectURL
	if got.Board.Target != want {
		t.Errorf("Target = %+v, want %+v", got.Board.Target, want)
	}
}

// D-1b: 表示名を送らずに設定した作成先は「名前を知らない」として残る。
//
// 表示名は任意（ADR 0019）。0 と空文字で保存され、それでも作成先としては
// 選ばれている。ここが崩れると、画面を通さずに設定したボードが未選択に見える。
func TestBoard_TargetWithoutDisplayNameIsStillSelected(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	target := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_1",
	}
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "b", Scene: "{}", Target: target,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target.ProjectNumber != 0 || got.Board.Target.ProjectTitle != "" ||
		got.Board.Target.ProjectURL != "" {
		t.Errorf("表示用の値 = %d/%q/%q, want 0 と空文字",
			got.Board.Target.ProjectNumber, got.Board.Target.ProjectTitle,
			got.Board.Target.ProjectURL)
	}
	if !got.Board.Target.Selected() {
		t.Error("表示名が無いだけで未選択になっている")
	}
}

// D-2: シーン更新で updated_at は進み、created_at は変わらない。
func TestBoard_UpdateScene(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	later := baseTime.Add(time.Hour)
	if err := repo.UpdateScene(
		t.Context(), "", "board-1", `{"elements":["updated"]}`, baseTime, later,
	); err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Scene != `{"elements":["updated"]}` {
		t.Errorf("Scene = %q", got.Board.Scene)
	}
	if !got.Board.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", got.Board.UpdatedAt, later)
	}
	if !got.Board.CreatedAt.Equal(baseTime) {
		t.Errorf("CreatedAt = %v, want %v（更新で変わってはいけない）", got.Board.CreatedAt, baseTime)
	}
}

func TestBoard_UpdateName(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	if err := repo.UpdateName(t.Context(), "", "board-1", "新しい名前"); err != nil {
		t.Fatalf("UpdateName: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Name != "新しい名前" {
		t.Errorf("Name = %q, want 新しい名前", got.Board.Name)
	}
	// **改名で版を進めてはいけない**（ADR 0020）。updated_at はシーンの版で、
	// 保存の照合基準になっている。進めると、そのボードを開いている別の
	// メンバーの次の保存が、誰もシーンを触っていないのに 409 で断られる。
	// ここが切れると、SET に updated_at を足した実装が素通りする。
	if !got.Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（改名で進めてはいけない）",
			got.Board.UpdatedAt, baseTime)
	}
	// 名前だけを書く。SET にシーンが混ざると、改名でブレストが消える。
	if got.Board.Scene != `{"elements":[]}` {
		t.Errorf("Scene = %q（改名で書き換えてはいけない）", got.Board.Scene)
	}
}

// メンバーでないボードは改名できない。絞りを外すと、ID を知っているだけで
// 他人のボードの名前を書き換えられる（ADR 0016）。
func TestBoard_UpdateNameRequiresMembership(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	err := repo.UpdateName(t.Context(), "someone-else", "board-1", "乗っ取り")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateName = %v, want port.ErrNotFound", err)
	}

	got, findErr := repo.Find(t.Context(), "", "board-1")
	if findErr != nil {
		t.Fatalf("Find: %v", findErr)
	}
	if got.Board.Name != "テストボード" {
		t.Errorf("Name = %q, want テストボード（書き換わってはいけない）", got.Board.Name)
	}
}

func TestBoard_UpdateNameNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	err := repo.UpdateName(t.Context(), "", "no-such-board", "名前")
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("UpdateName = %v, want port.ErrNotFound", err)
	}
}

func TestBoard_UpdateSceneNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	err := repo.UpdateScene(t.Context(), "", "no-such-board", "{}", baseTime, baseTime)
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("UpdateScene = %v, want port.ErrNotFound", err)
	}
}

// 基準が古ければ 1 文の中で弾く。ボードは共有できるので、2 人が同じボードを
// 開いて別々に描く状況は例外ではない（ADR 0020）。
func TestBoard_UpdateSceneRejectsStaleBase(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	// 先に保存した側。ここで版が baseTime から later に進む。
	later := baseTime.Add(time.Hour)
	if err := repo.UpdateScene(
		t.Context(), "", "board-1", `{"elements":["first"]}`, baseTime, later,
	); err != nil {
		t.Fatalf("UpdateScene（先に保存した側）: %v", err)
	}

	// 後から保存する側は、開いたときの版のまま送ってくる。
	err := repo.UpdateScene(
		t.Context(), "", "board-1", `{"elements":["second"]}`, baseTime, later.Add(time.Minute))
	if !errors.Is(err, port.ErrConflict) {
		t.Fatalf("UpdateScene = %v, want port.ErrConflict", err)
	}

	// 「無い」と混ぜない。無いものは待っても現れないが、食い違いは相手の
	// 変更を取り込めば進める。
	if errors.Is(err, port.ErrNotFound) {
		t.Error("版の食い違いを ErrNotFound で返している")
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Scene != `{"elements":["first"]}` {
		t.Errorf("Scene = %q, want 先に保存した側のまま", got.Board.Scene)
	}
	if !got.Board.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v（弾いたのに進んでいる）", got.Board.UpdatedAt, later)
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
		}, ""); err != nil {
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
	for _, a := range got {
		ids = append(ids, a.Board.ID)
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

// 一覧はシーンを読まない。読むと、返さないもののために全ボードぶんの
// シーン JSON をメモリへ載せることになる（画像は base64 で入りうる）。
//
// ここが落ちるのは List が boardColumns 側に戻ったとき。Find は逆に
// シーンを返し続ける必要があるので、両方を 1 つのテストで固定する。
func TestBoard_ListOmitsScene(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	const scene = `{"elements":[{"id":"a"}]}`
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "決済まわりのブレスト", Scene: scene,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].Board.Scene != "" {
		t.Errorf("List が Scene を返している: %q", got[0].Board.Scene)
	}
	// シーン以外は一覧に要る。落とすと表示名も作成先も出せなくなる。
	if got[0].Board.Name != "決済まわりのブレスト" {
		t.Errorf("Name = %q", got[0].Board.Name)
	}
	if !got[0].Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v", got[0].Board.UpdatedAt, baseTime)
	}

	found, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.Board.Scene != scene {
		t.Errorf("Find の Scene = %q, want %q", found.Board.Scene, scene)
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
