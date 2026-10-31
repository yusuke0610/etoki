package sqlite_test

import (
	"database/sql"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

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
		Outcome:      port.OutcomeComplete,
		Items: []port.SyncItem{
			withDatabaseID(item("e1", "PVTI_epic", port.KindEpic, nil), 101),
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
	// 識別子も作成時にしか取れない（ADR 0057）。0（知らない）と実値の両方が
	// 往復すること。
	if epic.ItemDatabaseID != 101 {
		t.Errorf("epic.ItemDatabaseID = %d, want 101", epic.ItemDatabaseID)
	}

	issue := got.Items[1]
	if issue.ItemDatabaseID != 0 {
		t.Errorf("issue.ItemDatabaseID = %d, want 0", issue.ItemDatabaseID)
	}
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
		Outcome:      port.OutcomeComplete,
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

// 途中で失敗した run は、結末と理由を持って読み直せる（ADR 0043）。ここが
// 切れると、履歴からは「作れた件数が少ない run」と区別が付かなくなる（#110）。
func TestSaveRun_KeepsOutcomeAndError(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID:      "board-1",
		AnnotationID: "annot-1",
		ContentHash:  "hash-1",
		CreatedAt:    baseTime,
		Outcome:      port.OutcomeIncomplete,
		Error:        "github graphql: rate limited",
		Items:        []port.SyncItem{item("e1", "PVTI_epic", port.KindEpic, nil)},
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
	if got.Outcome != port.OutcomeIncomplete {
		t.Errorf("Outcome = %q, want %q", got.Outcome, port.OutcomeIncomplete)
	}
	if got.Error != "github graphql: rate limited" {
		t.Errorf("Error = %q, want %q", got.Error, "github graphql: rate limited")
	}

	// 履歴の口でも同じものが読める。判定に使う最新 run と、1 回ずつの記録は
	// 別の経路なので、両方で確かめる（ADR 0026）。
	runs, err := repo.ListRunsByAnnotation(t.Context(), "board-1", "annot-1", 10)
	if err != nil {
		t.Fatalf("ListRunsByAnnotation: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs の件数 = %d, want 1", len(runs))
	}
	if runs[0].Outcome != port.OutcomeIncomplete || runs[0].Error == "" {
		t.Errorf("runs[0] = %+v, want incomplete と理由", runs[0])
	}
}

// 結末を書き忘れた run は保存させない。ゼロ値を通すと「記録していなかった頃の
// run」に化けて、あとから見分けられなくなる（ADR 0043）。
func TestSaveRun_RejectsMissingOutcome(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	run := port.SyncRun{
		BoardID:      "board-1",
		AnnotationID: "annot-1",
		ContentHash:  "hash-1",
		CreatedAt:    baseTime,
		Items:        []port.SyncItem{item("e1", "PVTI_epic", port.KindEpic, nil)},
	}

	if _, err := repo.SaveRun(t.Context(), run); err == nil {
		t.Fatal("Outcome 無しの SaveRun が通った")
	}

	// 完走した run に理由が付く、失敗した run の理由が空、のどちらも通さない。
	// 通すと、読む側が「理由の無い失敗」の意味を決められない。
	run.Outcome = port.OutcomeComplete
	run.Error = "boom"
	if _, err := repo.SaveRun(t.Context(), run); err == nil {
		t.Error("complete なのに理由つきの SaveRun が通った")
	}

	run.Outcome = port.OutcomeIncomplete
	run.Error = ""
	if _, err := repo.SaveRun(t.Context(), run); err == nil {
		t.Error("incomplete なのに理由の無い SaveRun が通った")
	}
}

// 結末を記録していなかった頃の run は「不明」のまま読む。**成功に倒さない。**
// 当時も途中失敗は起きていたので、complete と同じに見せると、履歴が嘘をつく
// （ADR 0043）。
func TestFindLatestRun_RunWithoutOutcome(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	// 移行前の INSERT を再現する。outcome を書かずに入れられることそのものが、
	// 既存の行が読めることの条件。
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO sync_runs (board_id, annotation_element_id, content_hash, created_at)
		 VALUES ('board-1', 'annot-1', 'hash-1', ?)`,
		baseTime.UTC().Format(time.RFC3339Nano),
	); err != nil {
		t.Fatalf("insert legacy sync_run: %v", err)
	}

	got, err := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if got == nil {
		t.Fatal("FindLatestRun returned nil")
	}
	if got.Outcome != port.OutcomeUnknown {
		t.Errorf("Outcome = %q, want %q（記録が無いので不明）", got.Outcome, port.OutcomeUnknown)
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want 空", got.Error)
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
		Outcome:      port.OutcomeComplete,
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
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
		ContentHash: "hash-2", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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

// 畳み込みで item_database_id だけは最新の行ではなくグループの MAX を採る
// （ADR 0057）。title / body と同じく MAX(id) の行から読む実装に戻すと落ちる。
//
//   - A: 列を足す前の run（0）を、後の run が実値で更新した → 実値が出る
//   - B: 実値を持つ run のあと、後の run が値を取り損ねた（0）→ 前の実値が残る
func TestListItemsByAnnotation_FoldsDatabaseIDByMax(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{
			item("e1", "PVTI_a", port.KindEpic, nil),
			withDatabaseID(item("i1", "PVTI_b", port.KindIssue, ptr("e1")), 202),
		},
	}); err != nil {
		t.Fatalf("run1: %v", err)
	}

	updatedA := withDatabaseID(item("x1", "PVTI_a", port.KindEpic, nil), 101)
	updatedA.Action = port.ActionUpdated
	updatedB := item("x2", "PVTI_b", port.KindIssue, ptr("x1"))
	updatedB.Title = "書き直した B"
	updatedB.Action = port.ActionUpdated

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-2", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{updatedA, updatedB},
	}); err != nil {
		t.Fatalf("run2: %v", err)
	}

	byAnnotation, err := repo.ListItemsByBoard(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListItemsByBoard: %v", err)
	}
	folded := byAnnotation["annot-1"]
	if len(folded) != 2 {
		t.Fatalf("items = %+v, want 2 件", folded)
	}
	if folded[0].ItemDatabaseID != 101 {
		t.Errorf("A = %d, want 101（後の run で埋まる）", folded[0].ItemDatabaseID)
	}
	// B は最新の行（title は書き直した B）から読みつつ、識別子は前の値を保つ。
	if folded[1].Title != "書き直した B" || folded[1].ItemDatabaseID != 202 {
		t.Errorf("B = %q/%d, want 書き直した B/202", folded[1].Title, folded[1].ItemDatabaseID)
	}

	// 注釈 1 件に絞る口も同じ畳み方をする。
	items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListItemsByAnnotation: %v", err)
	}
	if len(items) != 2 || items[0].ItemDatabaseID != 101 || items[1].ItemDatabaseID != 202 {
		t.Errorf("ListItemsByAnnotation = %+v, want 101 と 202", items)
	}
}

// 負の識別子は行として意味を持たないので DB でも作らせない。SaveRun を通らない
// 保守 SQL にも効くことを見る（.claude/rules/validation-boundaries.md）。
func TestSyncItems_RejectsNegativeDatabaseID(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{withDatabaseID(item("e1", "PVTI_a", port.KindEpic, nil), -1)},
	}); err == nil {
		t.Fatal("SaveRun(負の識別子) = nil, want error")
	}

	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sync_items`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	// 弾かれた run の item は 1 件も残らない。
	if n != 0 {
		t.Errorf("sync_items = %d 件, want 0", n)
	}

	// **保守 SQL に効くことは、行がある状態でしか見られない。** 上の SaveRun は
	// 正しく弾けるので sync_items は空で、そこへ UPDATE を当てても 0 行で通る。
	// 通ったことを CHECK が効いた証拠にすると、CHECK を消しても緑のままになる。
	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-2",
		ContentHash: "hash-2", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{withDatabaseID(item("e1", "PVTI_a", port.KindEpic, nil), 1)},
	}); err != nil {
		t.Fatalf("SaveRun(正の識別子): %v", err)
	}

	if _, err := db.ExecContext(t.Context(),
		`UPDATE sync_items SET item_database_id = -1`); err == nil {
		t.Error("UPDATE で負の識別子に落とせた: CHECK が効いていない")
	}

	// 弾いた UPDATE が書いていないこと。
	var got int64
	if err := db.QueryRowContext(t.Context(),
		`SELECT item_database_id FROM sync_items`).Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != 1 {
		t.Errorf("item_database_id = %d, want 1", got)
	}
}

func withDatabaseID(it port.SyncItem, id int64) port.SyncItem {
	it.ItemDatabaseID = id
	return it
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
		{BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "h", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{item("e1", "PVTI_mine", port.KindEpic, nil)}},
		{BoardID: "board-1", AnnotationID: "annot-2", ContentHash: "h", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{item("e1", "PVTI_other_annot", port.KindEpic, nil)}},
		{BoardID: "board-2", AnnotationID: "annot-1", ContentHash: "h", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
		{BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "h1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{
				item("e1", "PVTI_a", port.KindEpic, nil),
				item("i1", "PVTI_b", port.KindIssue, ptr("e1")),
			}},
		// PVTI_a を更新し、PVTI_c を足す。PVTI_b は触らない。
		{BoardID: "board-1", AnnotationID: "annot-1", ContentHash: "h2", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{
				updated,
				item("x2", "PVTI_c", port.KindIssue, ptr("x1")),
			}},
		{BoardID: "board-1", AnnotationID: "annot-2", ContentHash: "h", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{item("e1", "PVTI_d", port.KindEpic, nil)}},
		{BoardID: "board-2", AnnotationID: "annot-1", ContentHash: "h", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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

// ---------------------------------------------------------------------------
// C-3d: 届いたか分からない書き込み（ADR 0056）
// ---------------------------------------------------------------------------

// 確定しているのに item ID が無い行は書かせない。
//
// 通すと畳み込み（ADR 0026）が空文字で 1 つのグループを作り、別々の item が
// 1 件に混ざる。
//
// **固定しているのは行の不変条件で、どちらの守りが効いたかではない。** 守りは
// 2 つあり（SaveRun の検証と CHECK 制約、0013）、どちらか一方を外してもこの
// テストは緑のまま。復元や保守 SQL は SaveRun を通らないので DB 側が要り、
// SaveRun 側は 200 件の中のどれが悪いかを言うために要る。片方だけを固定する
// テストにすると、もう片方を消したときに落ちない側が残る。
func TestSaveRun_RejectsConfirmedItemWithoutItemID(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	broken := item("e1", "", port.KindEpic, nil)

	_, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{broken},
	})
	if err == nil {
		t.Fatal("SaveRun() = nil, want error")
	}
	// どの item かを名乗る。手掛かりが無いと、200 件の中のどれが悪いのか
	// 分からない。
	if !strings.Contains(err.Error(), "e1") {
		t.Errorf("SaveRun() = %v, want local_id を名乗る", err)
	}

	// 弾いたなら run ごと書かれていない。エラーだけ見て通すと、検証の前に
	// 書いてしまう実装でも緑になる。
	got, findErr := repo.FindLatestRun(t.Context(), "board-1", "annot-1")
	if findErr != nil {
		t.Fatalf("FindLatestRun: %v", findErr)
	}
	if got != nil {
		t.Errorf("run = %+v, want 何も書かれていない", got)
	}
}

// 畳み込みは未確定の書き込みを含めない（ADR 0056）。
//
// 含めると、在るかどうかが分からないものが「いま GitHub に在るもの」に紛れ、
// 更新先の照合（ADR 0026）まで通ってしまう。item ID を持たない未確定の作成は
// 空文字で 1 つのグループになるので、2 件が 1 件に見える形でも壊れる。
func TestListItemsByAnnotation_SkipsUnconfirmedWrites(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeIncomplete,
		Error: "boom",
		Items: []port.SyncItem{
			item("e1", "PVTI_a", port.KindEpic, nil),
			// 応答を失った作成が 2 件。どちらも item ID が無い。
			unconfirmed("i1", "", port.ActionCreated),
			unconfirmed("i2", "", port.ActionCreated),
		},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListItemsByAnnotation: %v", err)
	}

	var ids []string
	for _, it := range items {
		ids = append(ids, it.ItemID)
	}
	if want := []string{"PVTI_a"}; !slices.Equal(ids, want) {
		t.Fatalf("item = %v, want %v", ids, want)
	}
	if !items[0].Confirmed {
		t.Error("Confirmed = false, want true（往復で落ちている）")
	}
}

// 届いたか分からない書き込みは、解けない限り残る（ADR 0056）。
//
// **解けるのは、あとで同じ item に確定の記録が付いたときだけ。** そのときは
// いま GitHub に何があるか分かっているので、打ち手の無い警告を残さない。
// item ID を持たない未確定の作成は確定させる手段が無いので残り続ける。
func TestListUnconfirmedItemsByBoard_DropsOnlyResolved(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")
	repo := sqlite.NewMappingRepository(db)

	save := func(boardID, annotationID, hash string, items ...port.SyncItem) {
		t.Helper()
		if _, err := repo.SaveRun(t.Context(), port.SyncRun{
			BoardID: boardID, AnnotationID: annotationID,
			ContentHash: hash, CreatedAt: baseTime,
			Outcome: port.OutcomeIncomplete, Error: "boom",
			Items: items,
		}); err != nil {
			t.Fatalf("SaveRun(%s/%s): %v", boardID, annotationID, err)
		}
	}

	// annot-1: 応答を失った作成 1 件と、届いたか分からない更新 1 件。更新のほうは
	// あとで同じ item に書けているので解ける。
	save("board-1", "annot-1", "hash-1",
		unconfirmed("c1", "", port.ActionCreated),
		unconfirmed("u1", "PVTI_resolved", port.ActionUpdated),
	)
	save("board-1", "annot-1", "hash-2", item("u2", "PVTI_resolved", port.KindIssue, nil))

	// annot-2: 届いたか分からない更新のまま。書き直していないので解けない。
	save("board-1", "annot-2", "hash-3", unconfirmed("u3", "PVTI_open", port.ActionUpdated))

	// 別ボードは混ぜない。
	save("board-2", "annot-1", "hash-4", unconfirmed("c9", "", port.ActionCreated))

	byAnnotation, err := repo.ListUnconfirmedItemsByBoard(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListUnconfirmedItemsByBoard: %v", err)
	}

	annotations := slices.Sorted(maps.Keys(byAnnotation))
	if want := []string{"annot-1", "annot-2"}; !slices.Equal(annotations, want) {
		t.Fatalf("注釈 = %v, want %v", annotations, want)
	}

	// annot-1 に残るのは、確定させる手段の無い作成だけ。
	var local1 []string
	for _, it := range byAnnotation["annot-1"] {
		local1 = append(local1, it.LocalID)
	}
	if want := []string{"c1"}; !slices.Equal(local1, want) {
		t.Errorf("annot-1 = %v, want %v（解けた更新が残っている）", local1, want)
	}

	var local2 []string
	for _, it := range byAnnotation["annot-2"] {
		local2 = append(local2, it.LocalID)
	}
	if want := []string{"u3"}; !slices.Equal(local2, want) {
		t.Errorf("annot-2 = %v, want %v（解けていない更新が消えている）", local2, want)
	}
}

// 同じ item への確定は、あとに来たものだけが不確かさを解く。
//
// **順序を見ない実装（同じ item に確定の記録があれば消す）でも、上のテストは
// 通ってしまう。** 先に書けていて、あとで届いたか分からなくなった、という並びを
// 別に固定しておく。
func TestListUnconfirmedItemsByBoard_EarlierConfirmDoesNotResolve(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{item("e1", "PVTI_a", port.KindEpic, nil)},
	}); err != nil {
		t.Fatalf("run1: %v", err)
	}

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-2", CreatedAt: baseTime,
		Outcome: port.OutcomeIncomplete, Error: "boom",
		Items: []port.SyncItem{unconfirmed("u1", "PVTI_a", port.ActionUpdated)},
	}); err != nil {
		t.Fatalf("run2: %v", err)
	}

	byAnnotation, err := repo.ListUnconfirmedItemsByBoard(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListUnconfirmedItemsByBoard: %v", err)
	}
	if len(byAnnotation["annot-1"]) != 1 {
		t.Fatalf("annot-1 = %+v, want 1 件（前の確定で解けている）", byAnnotation["annot-1"])
	}

	// 畳み込みのほうは、届いたか分からない更新に上書きされず前の中身のまま。
	items, err := repo.ListItemsByAnnotation(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListItemsByAnnotation: %v", err)
	}
	if len(items) != 1 || items[0].Title != "title-e1" {
		t.Errorf("畳み込み = %+v, want 確定した run1 の中身", items)
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
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
		ContentHash: "hash-2", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
			ContentHash: hash, CreatedAt: baseTime, Items: items, Outcome: port.OutcomeComplete,
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
			ContentHash: hash, CreatedAt: baseTime, Items: items, Outcome: port.OutcomeComplete,
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
		{BoardID: "board-1", AnnotationID: "annot-a", ContentHash: "keep", CreatedAt: baseTime, Outcome: port.OutcomeComplete},
		{BoardID: "board-1", AnnotationID: "annot-b", ContentHash: "other-annot", CreatedAt: baseTime, Outcome: port.OutcomeComplete},
		{BoardID: "board-2", AnnotationID: "annot-a", ContentHash: "other-board", CreatedAt: baseTime, Outcome: port.OutcomeComplete},
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
			ContentHash: hash, CreatedAt: baseTime, Outcome: port.OutcomeComplete,
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
			ContentHash: "hash", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{item("e1", "PVTI_same", port.KindEpic, nil)},
		}); err != nil {
			t.Fatalf("SaveRun(%d 回目): %v", i+1, err)
		}
	}
}
