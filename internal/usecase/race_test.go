package usecase_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 作成先の固定（ADR 0014）は「run が 1 件でもあるか」で判断する。だが作成は
// 作成先を読んでから run を記録するまでに GitHub への書き込みを挟むので、その間は
// まだ run が無い。ここで作成先を変えられると、作成先 A に作った draft issue が
// 作成先 B を指すボードの run として残り、固定が防ごうとしていた事故そのものが
// 起きる。
//
// 排他は BoardService と CreationService で共有する。共有していないと、この
// テストは「作成中に作成先が変わった」で落ちる。
func TestSetTarget_WaitsForInFlightCreation(t *testing.T) {
	t.Parallel()

	locks := usecase.NewBoardLocks()
	boards := &racingBoards{board: *newBoard(interpretScene), updated: make(chan struct{})}
	mappings := &racingMappings{}

	gh := &blockingGitHub{
		fakeGitHub: &fakeGitHub{fields: projectFields()},
		started:    make(chan struct{}),
		resume:     make(chan struct{}),
	}
	// 途中で t.Fatal しても作成側を進ませる。止めたまま抜けると goroutine が残る。
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(gh.resume) }) }
	defer resume()

	creations := usecase.NewCreationService(boards, mappings, gh, locks,
		usecase.WithCreationClock(func() time.Time { return createdAt }))
	boardSvc := usecase.NewBoardService(boards, mappings, locks)

	hash := currentContentHash(t)
	createDone := make(chan error, 1)
	go func() {
		_, err := creations.Create(t.Context(), "board-1", "annot-1", hash, interpretation())
		createDone <- err
	}()

	// 最初の draft issue に着手した = 作成先はもう読み終えている。
	<-gh.started

	setDone := make(chan error, 1)
	go func() { setDone <- boardSvc.SetTarget(t.Context(), "board-1", newTarget()) }()

	select {
	case <-boards.updated:
		t.Fatal("作成中に作成先が変わった")
	case <-time.After(100 * time.Millisecond):
	}

	resume()

	if err := <-createDone; err != nil {
		t.Fatalf("Create() = %v", err)
	}
	// 待たされた先では run が残っている。「作成が始まっていたなら変えられない」が
	// 判定の取りこぼしではなく順序として出る。
	if err := <-setDone; !errors.Is(err, usecase.ErrTargetLocked) {
		t.Fatalf("SetTarget() = %v, want ErrTargetLocked", err)
	}
}

// blockingGitHub は最初の draft issue 作成で止まる GitHubClient。
type blockingGitHub struct {
	*fakeGitHub
	once    sync.Once
	started chan struct{}
	resume  chan struct{}
}

func (g *blockingGitHub) CreateDraftIssue(
	ctx context.Context, projectID string, item port.DraftIssue,
) (string, error) {
	g.once.Do(func() {
		close(g.started)
		<-g.resume
	})
	return g.fakeGitHub.CreateDraftIssue(ctx, projectID, item)
}

// racingBoards は複数の goroutine から触れる BoardRepository。
//
// 既存の fakeBoards は書き込み回数を数えるだけで排他を持たない。並行に読み書き
// するのはこのテストだけなので、ここに専用のものを置く。
type racingBoards struct {
	mu    sync.Mutex
	board port.Board
	// updated は UpdateTarget が呼ばれたら閉じる。作成中に作成先が変わって
	// しまったことを、待たずに検知するため。
	updated chan struct{}
}

// Find は所有者も突き合わせる。実装と同じ形にしておかないと、絞り忘れを
// フェイクが吸収してしまう（ADR 0016）。
func (r *racingBoards) Find(_ context.Context, actor, id string) (*port.BoardAccess, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.board.ID != id || actor != "" {
		return nil, nil
	}
	return &port.BoardAccess{Board: r.board, Role: port.RoleOwner}, nil
}

func (r *racingBoards) UpdateTarget(
	_ context.Context, _, _ string, t port.BoardTarget, _ time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.board.Target = t
	close(r.updated)
	return nil
}

func (r *racingBoards) UpdateTargetDisplay(
	_ context.Context, _, _ string, d port.BoardTargetDisplay, _ time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.board.Target.ProjectNumber = d.ProjectNumber
	r.board.Target.ProjectTitle = d.ProjectTitle
	r.board.Target.ProjectURL = d.ProjectURL
	return nil
}

func (r *racingBoards) Create(context.Context, port.Board, string) error { return nil }

func (r *racingBoards) UpdateName(context.Context, string, string, string) error { return nil }

func (r *racingBoards) Delete(context.Context, string, string) error { return nil }

func (r *racingBoards) UpdateScene(
	context.Context, string, string, string, time.Time, time.Time,
) error {
	return nil
}

func (r *racingBoards) List(context.Context, string) ([]port.BoardAccess, error) { return nil, nil }

func (r *racingBoards) ListMembers(context.Context, string) ([]port.BoardMember, error) {
	return nil, nil
}

func (r *racingBoards) AddMember(context.Context, port.BoardMember) error { return nil }

func (r *racingBoards) UpdateMemberRole(
	context.Context, string, string, port.BoardRole,
) error {
	return nil
}

func (r *racingBoards) RemoveMember(context.Context, string, string) error { return nil }

func (r *racingBoards) CountUnowned(context.Context) (int, error) { return 0, nil }

func (r *racingBoards) ClaimUnowned(context.Context, string) (int64, error) { return 0, nil }

// racingMappings は複数の goroutine から触れる MappingRepository。
type racingMappings struct {
	mu   sync.Mutex
	runs []port.SyncRun
}

// ListItemsByAnnotation はこの試験では使わない。直列化を見るための
// フェイクなので、畳み込みの中身は fakeMappings 側で確かめる。
func (r *racingMappings) ListItemsByBoard(
	context.Context, string,
) (map[string][]port.SyncItem, error) {
	return nil, nil
}

func (r *racingMappings) ListRunsByAnnotation(
	context.Context, string, string, int,
) ([]port.SyncRun, error) {
	return nil, nil
}

func (r *racingMappings) ListItemsByAnnotation(
	context.Context, string, string,
) ([]port.SyncItem, error) {
	return nil, nil
}

func (r *racingMappings) SaveRun(_ context.Context, run port.SyncRun) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.runs = append(r.runs, run)
	return int64(len(r.runs)), nil
}

func (r *racingMappings) FindLatestRun(context.Context, string, string) (*port.SyncRun, error) {
	return nil, nil
}

func (r *racingMappings) ListLatestRunsByBoard(
	_ context.Context, boardID string,
) ([]port.SyncRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var runs []port.SyncRun
	for _, run := range r.runs {
		if run.BoardID == boardID {
			runs = append(runs, run)
		}
	}
	return runs, nil
}
