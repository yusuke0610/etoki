package usecase_test

import (
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
