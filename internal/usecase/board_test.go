package usecase_test

import (
	"errors"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 作成先はボードごとに持ち、最初の draft issue を作ると固定される（ADR 0014）。

func newTarget() port.BoardTarget {
	return port.BoardTarget{RepositoryOwner: "acme", RepositoryName: "web", ProjectID: "PVT_2"}
}

// run が 1 件も無いうちは選び直せる。ブレストを始める前なら GitHub 側に
// 何も無いので、変えて困るものが無い。
func TestSetTarget_SucceedsBeforeAnyRun(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

	if err := svc.SetTarget(t.Context(), "board-1", newTarget()); err != nil {
		t.Fatalf("SetTarget() = %v", err)
	}
	if boards.writes != 1 {
		t.Errorf("writes = %d, want 1", boards.writes)
	}
}

// run が 1 件でもあれば固定。sync_runs は GitHub 側に残っている item の
// 追跡表であり、作成先が変わると記録が指す先を見失う。
func TestSetTarget_RejectsAfterFirstRun(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	err := svc.SetTarget(t.Context(), "board-1", newTarget())
	if !errors.Is(err, usecase.ErrTargetLocked) {
		t.Fatalf("SetTarget() = %v, want ErrTargetLocked", err)
	}
	if boards.writes != 0 {
		t.Errorf("固定済みなのに書き込んでいる: writes = %d", boards.writes)
	}
}

// 同じ値でも通さない。通すと「変更できた」と「たまたま同じだった」を
// 呼び出し側が区別できなくなる。
func TestSetTarget_RejectsSameValueAfterRun(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	boards := &fakeBoards{board: board}
	mappings := &fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	if err := svc.SetTarget(t.Context(), "board-1", board.Target); !errors.Is(err, usecase.ErrTargetLocked) {
		t.Fatalf("SetTarget() = %v, want ErrTargetLocked", err)
	}
}

func TestSetTarget_RejectsIncompleteTarget(t *testing.T) {
	t.Parallel()

	for name, target := range map[string]port.BoardTarget{
		"すべて空":        {},
		"project が無い": {RepositoryOwner: "acme", RepositoryName: "web"},
		"リポジトリ名が無い":   {RepositoryOwner: "acme", ProjectID: "PVT_2"},
		"リポジトリ所有者が無い": {RepositoryName: "web", ProjectID: "PVT_2"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

			if err := svc.SetTarget(t.Context(), "board-1", target); !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("SetTarget() = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestSetTarget_RejectsUnknownBoard(t *testing.T) {
	t.Parallel()

	svc := usecase.NewBoardService(&fakeBoards{}, &fakeMappings{}, usecase.NewBoardLocks())

	if err := svc.SetTarget(t.Context(), "missing", newTarget()); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("SetTarget() = %v, want ErrNotFound", err)
	}
}

func TestTargetLocked(t *testing.T) {
	t.Parallel()

	svc := usecase.NewBoardService(&fakeBoards{}, &fakeMappings{}, usecase.NewBoardLocks())
	locked, err := svc.TargetLocked(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("TargetLocked() = %v", err)
	}
	if locked {
		t.Error("run が無いのに固定されている")
	}

	withRun := usecase.NewBoardService(&fakeBoards{},
		&fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}, usecase.NewBoardLocks())
	if locked, err = withRun.TargetLocked(t.Context(), "board-1"); err != nil {
		t.Fatalf("TargetLocked() = %v", err)
	}
	if !locked {
		t.Error("run があるのに固定されていない")
	}
}

// 作成先が未選択のボードには作れない。置き場所が無いまま GitHub を叩くと、
// 原因が設定不足だと分かりにくい形で失敗する。
func TestCreate_RejectsBoardWithoutTarget(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	board.Target = port.BoardTarget{}

	gh := &fakeGitHub{fields: projectFields()}
	svc := usecase.NewCreationService(&fakeBoards{board: board}, &fakeMappings{}, gh,
		usecase.NewBoardLocks())

	_, err := svc.Create(t.Context(), "board-1", "annot-1", "sha256:whatever", interpretation())
	if !errors.Is(err, usecase.ErrTargetNotSelected) {
		t.Fatalf("Create() = %v, want ErrTargetNotSelected", err)
	}
	if len(gh.calls) != 0 {
		t.Errorf("GitHub を叩いている: %+v", gh.calls)
	}
}

// 作成先はボードから取る。プロセス全体の設定ではない。
func TestCreate_UsesTargetProjectOfBoard(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	board.Target.ProjectID = "PVT_board_specific"

	gh := &fakeGitHub{fields: projectFields()}
	svc := usecase.NewCreationService(&fakeBoards{board: board}, &fakeMappings{}, gh,
		usecase.NewBoardLocks())

	if _, err := svc.Create(
		t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation(),
	); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if gh.projectIDs == nil {
		t.Fatal("GitHub が呼ばれていない")
	}
	for _, id := range gh.projectIDs {
		if id != "PVT_board_specific" {
			t.Fatalf("projectID = %q, want PVT_board_specific", id)
		}
	}
}
