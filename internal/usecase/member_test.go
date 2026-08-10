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

// fakeUsers は login と ID で利用者を引くだけのディレクトリ。
type fakeUsers struct {
	users []port.User
}

func (f *fakeUsers) FindUserByLogin(_ context.Context, login string) (*port.User, error) {
	for _, u := range f.users {
		if u.Login == login {
			return &u, nil
		}
	}
	return nil, nil
}

func (f *fakeUsers) FindUsers(_ context.Context, ids []string) ([]port.User, error) {
	var out []port.User
	for _, id := range ids {
		for _, u := range f.users {
			if u.ID == id {
				out = append(out, u)
			}
		}
	}
	return out, nil
}

// memberSetup は「user-a が owner のボード」を用意する。
//
// fakeBoards の owner は「ボードを見られる操作者」で、ctx には利用者が
// 載っていないので空文字にしておく。ロールだけを差し替えて使う。
func memberSetup(t *testing.T, role port.BoardRole) (*usecase.BoardMemberService, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{
		board: newBoard(interpretScene),
		role:  role,
		members: []port.BoardMember{
			{BoardID: "board-1", UserID: "user-a", Role: port.RoleOwner, CreatedAt: baseTime},
		},
	}
	users := &fakeUsers{users: []port.User{
		{ID: "user-a", Login: "alice", DisplayName: "Alice"},
		{ID: "user-b", Login: "bob", DisplayName: "Bob"},
	}}

	svc := usecase.NewBoardMemberService(boards, users, usecase.NewBoardLocks(),
		usecase.WithMemberClock(func() time.Time { return baseTime }))
	return svc, boards
}

var baseTime = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// 招待にリポジトリのアクセス権は要らない。ブレストに呼ぶ相手と GitHub に
// 書ける相手は同じではない（ADR 0017）。GitHub を一切見ないことは、
// GitHub クライアントを渡していないこと自体が示している。
func TestInvite_AddsMember(t *testing.T) {
	t.Parallel()

	svc, boards := memberSetup(t, port.RoleOwner)

	m, err := svc.Invite(t.Context(), "board-1", "bob", port.RoleEditor)
	if err != nil {
		t.Fatalf("Invite() = %v", err)
	}
	if m.Membership.UserID != "user-b" || m.Membership.Role != port.RoleEditor {
		t.Errorf("Invite() = %+v", m.Membership)
	}
	if m.Login != "bob" || m.DisplayName != "Bob" {
		t.Errorf("表示情報が付いていない: %+v", m)
	}
	if len(boards.members) != 2 {
		t.Errorf("メンバーが増えていない: %+v", boards.members)
	}
}

// 一度もログインしていない相手には招待を積まない。login は改名で変わるので、
// 空いた login を取った別人に権限が渡る。
func TestInvite_RejectsUnknownLogin(t *testing.T) {
	t.Parallel()

	svc, boards := memberSetup(t, port.RoleOwner)

	_, err := svc.Invite(t.Context(), "board-1", "carol", port.RoleEditor)
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Invite() = %v, want ErrInvalidInput", err)
	}
	if len(boards.members) != 1 {
		t.Errorf("招待を積んでいる: %+v", boards.members)
	}
}

func TestInvite_RejectsUnknownRole(t *testing.T) {
	t.Parallel()

	svc, _ := memberSetup(t, port.RoleOwner)

	if _, err := svc.Invite(t.Context(), "board-1", "bob", "admin"); !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Invite() = %v, want ErrInvalidInput", err)
	}
}

// 2 回目は黙ってロールを書き換えない。「招待した」と「ロールを変えた」を
// 呼び出し側が区別できなくなる。
func TestInvite_RejectsDuplicate(t *testing.T) {
	t.Parallel()

	svc, _ := memberSetup(t, port.RoleOwner)

	if _, err := svc.Invite(t.Context(), "board-1", "bob", port.RoleEditor); err != nil {
		t.Fatalf("1 回目の Invite() = %v", err)
	}

	// ロールを変えて招待し直しても通さない。
	_, err := svc.Invite(t.Context(), "board-1", "bob", port.RoleViewer)
	if !errors.Is(err, usecase.ErrAlreadyMember) {
		t.Fatalf("2 回目の Invite() = %v, want ErrAlreadyMember", err)
	}
}

// 招待・解除・ロール変更は owner だけ。
func TestMemberWrites_RequireOwner(t *testing.T) {
	t.Parallel()

	for _, role := range []port.BoardRole{port.RoleEditor, port.RoleViewer} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			svc, boards := memberSetup(t, role)

			if _, err := svc.Invite(t.Context(), "board-1", "bob", port.RoleEditor); !errors.Is(err, usecase.ErrForbidden) {
				t.Errorf("Invite() = %v, want ErrForbidden", err)
			}
			if _, err := svc.SetRole(t.Context(), "board-1", "user-a", port.RoleViewer); !errors.Is(err, usecase.ErrForbidden) {
				t.Errorf("SetRole() = %v, want ErrForbidden", err)
			}
			if err := svc.Remove(t.Context(), "board-1", "user-a"); !errors.Is(err, usecase.ErrForbidden) {
				t.Errorf("Remove() = %v, want ErrForbidden", err)
			}
			if boards.writes != 0 {
				t.Errorf("弾いたのに書き込んでいる: writes = %d", boards.writes)
			}
		})
	}
}

// メンバー一覧は member なら誰でも見られる。誰と共有しているかを owner だけが
// 知っている状態にすると、招待された側は自分が何に呼ばれたのか分からない。
func TestList_AllowsViewer(t *testing.T) {
	t.Parallel()

	svc, _ := memberSetup(t, port.RoleViewer)

	got, err := svc.List(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(got) != 1 || got[0].Login != "alice" {
		t.Errorf("List() = %+v", got)
	}
}

// 利用者を消してもボードは残る（ADR 0016）。指し先の無いメンバーで一覧が
// 開けなくなってはいけない。
func TestList_KeepsMembersWithoutUser(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{
		board: newBoard(interpretScene),
		members: []port.BoardMember{
			{BoardID: "board-1", UserID: "deleted", Role: port.RoleOwner, CreatedAt: baseTime},
		},
	}
	svc := usecase.NewBoardMemberService(boards, &fakeUsers{}, usecase.NewBoardLocks())

	got, err := svc.List(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("List() = %d 件, want 1", len(got))
	}
	if got[0].Login != "" || got[0].Membership.UserID != "deleted" {
		t.Errorf("List() = %+v", got[0])
	}
}

// 最後の owner は外せない。通すと、誰も招待できず作成先も変えられない
// ボードが残る。
func TestRemove_RejectsLastOwner(t *testing.T) {
	t.Parallel()

	svc, boards := memberSetup(t, port.RoleOwner)

	if err := svc.Remove(t.Context(), "board-1", "user-a"); !errors.Is(err, usecase.ErrLastOwner) {
		t.Fatalf("Remove() = %v, want ErrLastOwner", err)
	}
	if len(boards.members) != 1 {
		t.Errorf("最後の owner を外している: %+v", boards.members)
	}
}

func TestSetRole_RejectsDemotingLastOwner(t *testing.T) {
	t.Parallel()

	svc, _ := memberSetup(t, port.RoleOwner)

	_, err := svc.SetRole(t.Context(), "board-1", "user-a", port.RoleEditor)
	if !errors.Is(err, usecase.ErrLastOwner) {
		t.Fatalf("SetRole() = %v, want ErrLastOwner", err)
	}
}

// owner が 2 人いれば片方は抜けられる。判定は「自分自身か」ではなく
// 「他に owner がいるか」。
func TestRemove_AllowsOwnerWhenAnotherExists(t *testing.T) {
	t.Parallel()

	svc, boards := memberSetup(t, port.RoleOwner)
	boards.members = append(boards.members, port.BoardMember{
		BoardID: "board-1", UserID: "user-b", Role: port.RoleOwner, CreatedAt: baseTime,
	})

	if err := svc.Remove(t.Context(), "board-1", "user-a"); err != nil {
		t.Fatalf("Remove() = %v", err)
	}
	if len(boards.members) != 1 || boards.members[0].UserID != "user-b" {
		t.Errorf("Remove() 後のメンバー = %+v", boards.members)
	}
}

// 非メンバーには 403 ではなく「無い」を返す。
func TestMembers_HideBoardFromNonMembers(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene), owner: "someone-else"}
	svc := usecase.NewBoardMemberService(boards, &fakeUsers{}, usecase.NewBoardLocks())

	if _, err := svc.List(t.Context(), "board-1"); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Errorf("List() = %v, want ErrBoardNotFound", err)
	}
	if _, err := svc.Invite(t.Context(), "board-1", "bob", port.RoleEditor); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Errorf("Invite() = %v, want ErrBoardNotFound", err)
	}
}

// blockingMembers は最初の ListMembers で止まるボードリポジトリ。
//
// 「他に owner がいるか」は数えてから書くまでに間がある。その間を実際に開けて
// みせないと、排他が効いているかを確かめたことにならない（race_test.go と同じ
// 方針）。
type blockingMembers struct {
	mu      sync.Mutex
	board   *port.Board
	members []port.BoardMember

	once    sync.Once
	started chan struct{}
	resume  chan struct{}
}

func (b *blockingMembers) Find(_ context.Context, _, id string) (*port.BoardAccess, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.board.ID != id {
		return nil, nil
	}
	return &port.BoardAccess{Board: *b.board, Role: port.RoleOwner}, nil
}

func (b *blockingMembers) ListMembers(_ context.Context, _ string) ([]port.BoardMember, error) {
	// 1 本目だけ、数え終えたところで止める。
	b.once.Do(func() {
		close(b.started)
		<-b.resume
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]port.BoardMember(nil), b.members...), nil
}

func (b *blockingMembers) RemoveMember(_ context.Context, _, userID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, m := range b.members {
		if m.UserID == userID {
			b.members = append(b.members[:i], b.members[i+1:]...)
			return nil
		}
	}
	return port.ErrNotFound
}

func (b *blockingMembers) Create(context.Context, port.Board, string) error { return nil }

func (b *blockingMembers) UpdateScene(context.Context, string, string, string, time.Time) error {
	return nil
}

func (b *blockingMembers) UpdateTarget(
	context.Context, string, string, port.BoardTarget, time.Time,
) error {
	return nil
}

func (b *blockingMembers) List(context.Context, string) ([]port.BoardAccess, error) {
	return nil, nil
}

func (b *blockingMembers) AddMember(context.Context, port.BoardMember) error { return nil }

func (b *blockingMembers) UpdateMemberRole(
	context.Context, string, string, port.BoardRole,
) error {
	return nil
}

func (b *blockingMembers) CountUnowned(context.Context) (int, error) { return 0, nil }

func (b *blockingMembers) ClaimUnowned(context.Context, string) (int64, error) { return 0, nil }

// owner が 2 人いるボードから同時に 2 人とも抜けようとしても、owner は残る。
//
// 排他が無いと、2 本とも「もう 1 人いる」と判断して両方が抜け、誰も招待できず
// 作成先も変えられないボードが残る（ADR 0017 / BoardLocks）。
func TestRemove_ConcurrentOwnersKeepOne(t *testing.T) {
	t.Parallel()

	boards := &blockingMembers{
		board: newBoard(interpretScene),
		members: []port.BoardMember{
			{BoardID: "board-1", UserID: "user-a", Role: port.RoleOwner, CreatedAt: baseTime},
			{BoardID: "board-1", UserID: "user-b", Role: port.RoleOwner, CreatedAt: baseTime},
		},
		started: make(chan struct{}),
		resume:  make(chan struct{}),
	}
	svc := usecase.NewBoardMemberService(boards, &fakeUsers{}, usecase.NewBoardLocks())

	// 途中で t.Fatal しても 1 本目を進ませる。止めたまま抜けると goroutine が残る。
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(boards.resume) }) }
	defer resume()

	first := make(chan error, 1)
	go func() { first <- svc.Remove(t.Context(), "board-1", "user-a") }()

	// 1 本目が owner を数え終えた = ここから書き込みまでが窓。
	<-boards.started

	second := make(chan error, 1)
	go func() { second <- svc.Remove(t.Context(), "board-1", "user-b") }()

	// 排他が効いていれば 2 本目はここで待たされ、まだ数えてすらいない。
	select {
	case err := <-second:
		t.Fatalf("1 本目の途中で 2 本目が進んだ: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	resume()

	if err := <-first; err != nil {
		t.Fatalf("1 本目の Remove() = %v", err)
	}
	// 待たされた先では owner が 1 人しかいない。「最後の owner は外せない」が
	// 判定の取りこぼしではなく順序として出る。
	if err := <-second; !errors.Is(err, usecase.ErrLastOwner) {
		t.Fatalf("2 本目の Remove() = %v, want ErrLastOwner", err)
	}

	remaining, err := boards.ListMembers(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(remaining) != 1 || remaining[0].UserID != "user-b" {
		t.Fatalf("残ったメンバー = %+v, want user-b だけ", remaining)
	}
}
