package usecase_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 履歴は新しい順で返る（ADR 0007）。畳み込み（いま GitHub に在るもの）とは
// 別の口で、1 回ずつの記録をそのまま返す。
func TestListRuns_NewestFirst(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{}
	saveRuns(t, mappings, "annot-1", 3)

	got, err := usecase.NewAnnotationService(boards, mappings).
		ListRuns(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListRuns() = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("件数 = %d, want 3", len(got))
	}
	if got[0].ContentHash != "h3" || got[2].ContentHash != "h1" {
		t.Errorf("並び = %q … %q, want h3 … h1（新しい順）",
			got[0].ContentHash, got[2].ContentHash)
	}
}

// 上限は新しいほうから効く。ここが切れると、上限を渡し忘れた（全件返す）実装や、
// 古いほうを残す実装が素通りする。
func TestListRuns_CapsAtMaxRunHistory(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{}
	total := usecase.MaxRunHistory + 2
	saveRuns(t, mappings, "annot-1", total)

	got, err := usecase.NewAnnotationService(boards, mappings).
		ListRuns(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListRuns() = %v", err)
	}
	if len(got) != usecase.MaxRunHistory {
		t.Fatalf("件数 = %d, want %d", len(got), usecase.MaxRunHistory)
	}
	if got[0].ContentHash != "h"+strconv.Itoa(total) {
		t.Errorf("先頭 = %q, want 最新の h%d", got[0].ContentHash, total)
	}
}

// **シーンから注釈が消えていても履歴は読める。** run は GitHub 側に在るものの
// 追跡表なので（ADR 0007）、frame を消した時点で辿れなくなると、いちばん辿り
// たい場面（何を作ったか分からなくなったとき）で辿れない。
//
// ここが切れるのは、ListStates と同じようにシーンを見て注釈の実在を確かめる
// 実装にしたとき。
func TestListRuns_KeepsHistoryAfterAnnotationIsRemoved(t *testing.T) {
	t.Parallel()

	// 注釈を 1 つも持たないシーン。
	boards := &fakeBoards{board: newBoard(emptyScene)}
	mappings := &fakeMappings{}
	saveRuns(t, mappings, "annot-gone", 1)

	got, err := usecase.NewAnnotationService(boards, mappings).
		ListRuns(t.Context(), "board-1", "annot-gone")
	if err != nil {
		t.Fatalf("ListRuns() = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1（frame を消しても記録は残る）", len(got))
	}
}

func TestListRuns_Empty(t *testing.T) {
	t.Parallel()

	got, err := usecase.NewAnnotationService(
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{},
	).ListRuns(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("ListRuns() = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %+v, want 空", got)
	}
}

// **frame を消して保存しても、GitHub に作ったものへは辿れる**（#111）。
//
// `ListStates` は畳み込みをボード全体で引いておきながら、シーンに残っている
// 注釈のぶんだけ配って残りを捨てていた。GitHub には draft issue が残っている
// のに、etoki の画面からは存在しないことになる（ADR 0009 が避けたかった形）。
//
// ここが切れるのは、畳み込みの残りを捨てる実装に戻したとき。
func TestListStates_ReturnsDetachedAnnotations(t *testing.T) {
	t.Parallel()

	// シーンに在るのは annot-1 だけ。annot-gone の frame は消されている。
	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{}
	saveRunWithItems(t, mappings, "annot-1", "PVTI_live", "生きているほう")
	saveRunWithItems(t, mappings, "annot-gone", "PVTI_gone", "消した囲みで作ったほう")

	states, detached, err := usecase.NewAnnotationService(boards, mappings).
		ListStates(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListStates() = %v", err)
	}

	// シーンに在るほうは今までどおり。混ぜて返していないことまで見る。
	if len(states) != 1 || states[0].Annotation.ID != "annot-1" {
		t.Fatalf("states = %+v, want annot-1 の 1 件", states)
	}

	if len(detached) != 1 {
		t.Fatalf("detached = %d 件, want 1", len(detached))
	}
	if detached[0].ID != "annot-gone" {
		t.Errorf("detached[0].ID = %q, want annot-gone", detached[0].ID)
	}
	// **何の囲みだったかは items からしか読めない。** 名前はシーンから消えて
	// いるので取れない。ここが空だと、辿る先の無い ID が並ぶだけになる。
	if len(detached[0].Items) != 1 {
		t.Fatalf("items = %d 件, want 1", len(detached[0].Items))
	}
	if got := detached[0].Items[0].Title; got != "消した囲みで作ったほう" {
		t.Errorf("items[0].Title = %q", got)
	}
	if detached[0].LatestRun == nil {
		t.Error("LatestRun が無い。いつ実行したのかも見分ける材料になる")
	}
}

// 消しただけで 1 件も作っていない注釈は出さない。辿る先が無い。
func TestListStates_IgnoresDetachedWithoutItems(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{}
	// items を持たない run。1 件も作れずに終わった run も記録には残る（ADR 0009）。
	saveRuns(t, mappings, "annot-gone", 1)

	_, detached, err := usecase.NewAnnotationService(boards, mappings).
		ListStates(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListStates() = %v", err)
	}
	if len(detached) != 0 {
		t.Errorf("detached = %+v, want 空", detached)
	}
}

// 並びが実行ごとに入れ替わらないこと。畳み込みは map で返るので、揃えないと
// 開き直すたびに順が変わる。
func TestListStates_DetachedIsSortedByID(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	mappings := &fakeMappings{}
	for _, id := range []string{"annot-c", "annot-a", "annot-b"} {
		saveRunWithItems(t, mappings, id, "PVTI_"+id, id)
	}

	_, detached, err := usecase.NewAnnotationService(boards, mappings).
		ListStates(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListStates() = %v", err)
	}

	got := make([]string, len(detached))
	for i, d := range detached {
		got[i] = d.ID
	}
	if want := []string{"annot-a", "annot-b", "annot-c"}; !slices.Equal(got, want) {
		t.Errorf("並び = %v, want %v", got, want)
	}
}

// saveRunWithItems は draft issue を 1 件作った run を積む。
func saveRunWithItems(
	t *testing.T, mappings *fakeMappings, annotationID, itemID, title string,
) {
	t.Helper()

	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID:      "board-1",
		AnnotationID: annotationID,
		ContentHash:  "h1",
		CreatedAt:    baseTime,
		Items: []port.SyncItem{{
			ItemID:    itemID,
			Kind:      port.KindIssue,
			Title:     title,
			LocalID:   "i1",
			Action:    port.ActionCreated,
			CreatedAt: baseTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
}

// saveRuns は同じ注釈に n 件の run を古い順で積む。
func saveRuns(t *testing.T, mappings *fakeMappings, annotationID string, n int) {
	t.Helper()

	for i := 1; i <= n; i++ {
		if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
			BoardID:      "board-1",
			AnnotationID: annotationID,
			ContentHash:  "h" + strconv.Itoa(i),
			CreatedAt:    baseTime,
		}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}
}
