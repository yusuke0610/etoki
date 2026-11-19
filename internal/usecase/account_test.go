package usecase_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 利用者を消す（ADR 0101）。**守りたいのは 3 つ。** その人だけが owner のボードは
// 消し、ほかの人のボードは消さないこと。利用者の行を消すのはボードとメンバーの
// 行を片付けたあとであること（先に消すと持ち主の無いボードが残る）。途中で
// 失敗しても、もう一度消せば続きから消えること。

// boardSet は複数のボードとメンバーを持つボードリポジトリ。
//
// **メンバーで絞る。** 実装（sqlite）と同じく、List と Find は操作者がメンバーで
// あるボードだけを返す。全部返すフェイクだと、他人のボードを消す実装でも緑に
// なる。使わない口は埋め込んだ nil に落ちる（呼べば panic で分かる）。
type boardSet struct {
	port.BoardRepository

	boards  map[string]port.Board
	order   []string
	members map[string][]port.BoardMember
	// ops は書き込みの順番。利用者の行を最後に消すことを見る。
	ops *[]string
	// removeErr は RemoveMember が返す誤り。途中で失敗したあとの続きを見る。
	removeErr error
	// listed は List が返したことを知らせる。排他を待たせているあいだにメンバーを
	// 動かすテストが、List を読み終えてから書くために使う。
	listed chan struct{}
	// onListMembers は ListMembers のたびに呼ばれる。数えた時点で排他を握って
	// いるかを見るテストが使う。
	onListMembers func(boardID string)
}

func newBoardSet(ops *[]string) *boardSet {
	return &boardSet{
		boards:  map[string]port.Board{},
		members: map[string][]port.BoardMember{},
		ops:     ops,
	}
}

// add はボードを足す。members は (利用者, ロール) の組。
func (s *boardSet) add(id string, members ...port.BoardMember) {
	s.boards[id] = port.Board{ID: id, Name: "ボード " + id, UpdatedAt: baseTime}
	s.order = append(s.order, id)
	for i := range members {
		members[i].BoardID = id
	}
	s.members[id] = members
}

func (s *boardSet) roleOf(boardID, user string) (port.BoardRole, bool) {
	for _, m := range s.members[boardID] {
		if m.UserID == user {
			return m.Role, true
		}
	}
	return "", false
}

func (s *boardSet) List(_ context.Context, actor string) ([]port.BoardAccess, error) {
	var out []port.BoardAccess
	for _, id := range s.order {
		b, ok := s.boards[id]
		if !ok {
			continue
		}
		if role, ok := s.roleOf(id, actor); ok {
			out = append(out, port.BoardAccess{Board: b, Role: role})
		}
	}
	if s.listed != nil {
		s.listed <- struct{}{}
	}
	return out, nil
}

func (s *boardSet) Find(_ context.Context, actor, id string) (*port.BoardAccess, error) {
	b, ok := s.boards[id]
	if !ok {
		return nil, nil
	}
	role, ok := s.roleOf(id, actor)
	if !ok {
		return nil, nil
	}
	return &port.BoardAccess{Board: b, Role: role}, nil
}

func (s *boardSet) ListMembers(_ context.Context, boardID string) ([]port.BoardMember, error) {
	if s.onListMembers != nil {
		s.onListMembers(boardID)
	}
	return slices.Clone(s.members[boardID]), nil
}

func (s *boardSet) RemoveMember(_ context.Context, boardID, userID string) error {
	if s.removeErr != nil {
		return s.removeErr
	}
	before := len(s.members[boardID])
	s.members[boardID] = slices.DeleteFunc(s.members[boardID], func(m port.BoardMember) bool {
		return m.UserID == userID
	})
	if len(s.members[boardID]) == before {
		return port.ErrNotFound
	}
	*s.ops = append(*s.ops, "leave "+boardID)
	return nil
}

// Delete は実装と同じく、操作者がメンバーでないボードは消さない。
func (s *boardSet) Delete(_ context.Context, actor, id string) error {
	if _, ok := s.roleOf(id, actor); !ok {
		return port.ErrNotFound
	}
	delete(s.boards, id)
	delete(s.members, id)
	*s.ops = append(*s.ops, "delete board "+id)
	return nil
}

// accountStore は利用者の行。消された利用者を覚える。
type accountStore struct {
	deleted []string
	ops     *[]string
}

func (a *accountStore) DeleteUser(_ context.Context, userID string) error {
	a.deleted = append(a.deleted, userID)
	*a.ops = append(*a.ops, "delete user "+userID)
	return nil
}

func owner(user string) port.BoardMember { return port.BoardMember{UserID: user, Role: port.RoleOwner} }
func editor(user string) port.BoardMember {
	return port.BoardMember{UserID: user, Role: port.RoleEditor}
}

// accountFixture は u1 から見て扱いの違う 4 枚を持つ。
//
//   - solo: u1 だけ（u1 だけが owner）→ 消える
//   - shared: u1 が owner、u2 が editor（u1 だけが owner）→ 消える
//   - coOwned: u1 と u3 が owner → u1 が外れるだけ
//   - joined: u4 が owner、u1 が editor → u1 が外れるだけ
func accountFixture() (*boardSet, *fakeMappings, *accountStore, *usecase.AccountService, *[]string) {
	ops := &[]string{}
	boards := newBoardSet(ops)
	boards.add("solo", owner("u1"))
	boards.add("shared", owner("u1"), editor("u2"))
	boards.add("coOwned", owner("u1"), owner("u3"))
	boards.add("joined", owner("u4"), editor("u1"))

	mappings := &fakeMappings{}
	accounts := &accountStore{ops: ops}
	boardSvc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())
	svc := usecase.NewAccountService(boards, boardSvc, accounts)
	return boards, mappings, accounts, svc, ops
}

func TestAccountDeletion_ListsSoleOwnedBoardsAndCountsTheRest(t *testing.T) {
	t.Parallel()

	_, mappings, _, svc, _ := accountFixture()
	// 共有しているボードから作った記録。削除の確認と同じ数え方で出る（ADR 0042）。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: "shared", AnnotationID: "a1", Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{
			{ItemID: "PVTI_1", Confirmed: true},
			{ItemID: "PVTI_2", Confirmed: true},
		},
	}); err != nil {
		t.Fatalf("SaveRun() = %v", err)
	}
	// 他人のボードの記録は数えない。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: "joined", AnnotationID: "a2", Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{{ItemID: "PVTI_9", Confirmed: true}},
	}); err != nil {
		t.Fatalf("SaveRun() = %v", err)
	}

	got, err := svc.Deletion(asUser(t.Context(), "u1"))
	if err != nil {
		t.Fatalf("Deletion() = %v", err)
	}

	want := []usecase.DeletedBoard{
		{ID: "solo", Name: "ボード solo", OtherMemberCount: 0},
		{
			ID: "shared", Name: "ボード shared", OtherMemberCount: 1,
			Losing: usecase.BoardDeletion{RecordedItemCount: 2},
		},
	}
	if !slices.Equal(got.Boards, want) {
		t.Errorf("Boards = %+v, want %+v", got.Boards, want)
	}
	if got.LeftBoardCount != 2 {
		t.Errorf("LeftBoardCount = %d, want 2（coOwned と joined）", got.LeftBoardCount)
	}
}

func TestAccountDelete_DeletesSoleOwnedBoardsLeavesTheRestThenTheUser(t *testing.T) {
	t.Parallel()

	boards, _, accounts, svc, ops := accountFixture()

	if err := svc.Delete(asUser(t.Context(), "u1")); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	// その人だけが owner のボードは、共有していても消える。
	for _, id := range []string{"solo", "shared"} {
		if _, ok := boards.boards[id]; ok {
			t.Errorf("%s が残っている", id)
		}
	}
	// ほかにも owner がいる・他人のボードは残り、u1 だけが外れる。
	for id, want := range map[string][]string{"coOwned": {"u3"}, "joined": {"u4"}} {
		if _, ok := boards.boards[id]; !ok {
			t.Errorf("%s が消えている", id)
		}
		var got []string
		for _, m := range boards.members[id] {
			got = append(got, m.UserID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s のメンバー = %v, want %v", id, got, want)
		}
	}
	if !slices.Equal(accounts.deleted, []string{"u1"}) {
		t.Errorf("消した利用者 = %v, want [u1]", accounts.deleted)
	}
	// **利用者の行は最後。** 先に消すと、持ち主の無いボードが残る。
	if last := (*ops)[len(*ops)-1]; last != "delete user u1" {
		t.Errorf("最後の書き込み = %q, want delete user u1（順番 %v）", last, *ops)
	}
}

// 途中で失敗したら、消えたものは戻さず、利用者の行は消さない。もう一度消せば
// 続きから消える。
func TestAccountDelete_ContinuesAfterAPartialFailure(t *testing.T) {
	t.Parallel()

	boards, _, accounts, svc, _ := accountFixture()
	boards.removeErr = errors.New("boom")

	if err := svc.Delete(asUser(t.Context(), "u1")); err == nil {
		t.Fatal("Delete() = nil, want error")
	}
	if len(accounts.deleted) != 0 {
		t.Fatalf("途中で失敗したのに利用者を消した: %v", accounts.deleted)
	}

	boards.removeErr = nil
	if err := svc.Delete(asUser(t.Context(), "u1")); err != nil {
		t.Fatalf("2 回目の Delete() = %v", err)
	}
	if got, err := boards.List(t.Context(), "u1"); err != nil || len(got) != 0 {
		t.Errorf("u1 のボード = %+v, %v, want なし", got, err)
	}
	if !slices.Equal(accounts.deleted, []string{"u1"}) {
		t.Errorf("消した利用者 = %v, want [u1]", accounts.deleted)
	}
}

// 空文字は「認証なしの所有者」1 人（ADR 0016）。消すとその構成の全ボードが消える。
func TestAccountDelete_RefusesTheUnauthenticatedOwner(t *testing.T) {
	t.Parallel()

	ops := &[]string{}
	boards := newBoardSet(ops)
	boards.add("local", owner(""))
	accounts := &accountStore{ops: ops}
	svc := usecase.NewAccountService(
		boards, usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks()), accounts)

	if _, err := svc.Deletion(asUser(t.Context(), "")); !errors.Is(err, usecase.ErrInvalidInput) {
		t.Errorf("Deletion() = %v, want ErrInvalidInput", err)
	}
	if err := svc.Delete(asUser(t.Context(), "")); !errors.Is(err, usecase.ErrInvalidInput) {
		t.Errorf("Delete() = %v, want ErrInvalidInput", err)
	}
	if len(*ops) != 0 {
		t.Errorf("断ったのに書き込んだ: %v", *ops)
	}
}

// ボードはボードの削除と同じロックの中で消す（ADR 0042）。作成の途中で消すと、
// 取り消せない作成が記録の無いまま残る。
func TestAccountDelete_WaitsForTheBoardLock(t *testing.T) {
	t.Parallel()

	ops := &[]string{}
	boards := newBoardSet(ops)
	boards.add("solo", owner("u1"))
	locks := usecase.NewBoardLocks()
	accounts := &accountStore{ops: ops}
	svc := usecase.NewAccountService(
		boards, usecase.NewBoardService(boards, &fakeMappings{}, locks), accounts)

	// 作成が走っている（ロックを握っている）ことにする。
	release, err := locks.Acquire(t.Context(), "solo")
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	defer release()

	ctx, cancel := context.WithTimeout(asUser(t.Context(), "u1"), 50*time.Millisecond)
	defer cancel()
	if err := svc.Delete(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Delete() = %v, want DeadlineExceeded", err)
	}
	if _, ok := boards.boards["solo"]; !ok {
		t.Error("ロックを待たずにボードを消した")
	}
	if len(accounts.deleted) != 0 {
		t.Errorf("ボードを消せないのに利用者を消した: %v", accounts.deleted)
	}
}

// owner が 2 人のボードで、排他を待つあいだにもう 1 人の owner が抜けたら、
// 外すのではなくボードごと消す。排他の外で数えた owner の数のまま外すと、owner の
// いないボードが残る（`BoardMemberService.Remove` が排他の中で数えるのと同じ守り）。
func TestAccountDelete_RecountsOwnersInsideTheLock(t *testing.T) {
	t.Parallel()

	ops := &[]string{}
	boards := newBoardSet(ops)
	boards.add("coOwned", owner("u1"), owner("u3"), editor("u2"))
	boards.listed = make(chan struct{}, 1)
	locks := usecase.NewBoardLocks()
	accounts := &accountStore{ops: ops}
	svc := usecase.NewAccountService(
		boards, usecase.NewBoardService(boards, &fakeMappings{}, locks), accounts)

	// u3 が抜ける操作が排他を握っている。
	release, err := locks.Acquire(t.Context(), "coOwned")
	if err != nil {
		t.Fatalf("Acquire() = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- svc.Delete(asUser(t.Context(), "u1")) }()

	// 一覧を読み終えた（owner が 2 人に見えた）あとで、u3 が抜ける。
	<-boards.listed
	boards.members["coOwned"] = []port.BoardMember{
		{BoardID: "coOwned", UserID: "u1", Role: port.RoleOwner},
		{BoardID: "coOwned", UserID: "u2", Role: port.RoleEditor},
	}
	release()

	if err := <-done; err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if _, ok := boards.boards["coOwned"]; ok {
		t.Errorf("u1 だけが owner になったボードが残っている（メンバー %v、順番 %v）",
			boards.members["coOwned"], *ops)
	}
	if !slices.Equal(*ops, []string{"delete board coOwned", "delete user u1"}) {
		t.Errorf("書き込み = %v, want [delete board coOwned, delete user u1]", *ops)
	}
}

// 一覧を引いてから抜けるまでのあいだに、ほかの経路でボードが消えていたり、外されて
// いたりしても誤りにしない。消す手間が省けただけで、ここで止めると利用者を消せなく
// なる。サーバーと排他を共有しない `etoki delete-user` や、ほかの owner の操作で起きる。
func TestAccountDelete_SkipsBoardsThatAreAlreadyGone(t *testing.T) {
	t.Parallel()

	ops := &[]string{}
	boards := newBoardSet(ops)
	boards.add("solo", owner("u1"))
	boards.add("joined", owner("u4"), editor("u1"))
	accounts := &accountStore{ops: ops}
	svc := usecase.NewAccountService(
		boards, usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks()), accounts)

	boards.onListMembers = func(boardID string) {
		switch boardID {
		case "solo":
			// 数えた直後に、排他を共有しないほかのプロセスが消した。メンバーは
			// 数えた時点のまま返る。
			delete(boards.boards, "solo")
		case "joined":
			// owner が先に外していた。
			boards.members["joined"] = []port.BoardMember{
				{BoardID: "joined", UserID: "u4", Role: port.RoleOwner},
			}
		}
	}

	if err := svc.Delete(asUser(t.Context(), "u1")); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if !slices.Equal(accounts.deleted, []string{"u1"}) {
		t.Errorf("消した利用者 = %v, want [u1]", accounts.deleted)
	}
	if !slices.Equal(*ops, []string{"delete user u1"}) {
		t.Errorf("書き込み = %v, want [delete user u1]（どちらも消す手間が省けた）", *ops)
	}
}

// 消すときに owner を数えるのは、そのボードの排他を握ってから。
// `TestAccountDelete_RecountsOwnersInsideTheLock` は「数え直した結果に従う」ことを
// 見て、こちらは「数えた時点で排他の中にいる」ことを見る。外で数える実装は、
// あちらでは届く順しだいで通りうる。
func TestAccountDelete_CountsOwnersWhileHoldingTheLock(t *testing.T) {
	t.Parallel()

	ops := &[]string{}
	boards := newBoardSet(ops)
	boards.add("coOwned", owner("u1"), owner("u3"))
	boards.add("solo", owner("u1"))
	locks := usecase.NewBoardLocks()
	accounts := &accountStore{ops: ops}
	svc := usecase.NewAccountService(
		boards, usecase.NewBoardService(boards, &fakeMappings{}, locks), accounts)

	// 握っていれば、ここから取ろうとしても取れない。
	boards.onListMembers = func(boardID string) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if release, err := locks.Acquire(ctx, boardID); err == nil {
			release()
			t.Errorf("%s の owner を排他の外で数えた", boardID)
		}
	}

	if err := svc.Delete(asUser(t.Context(), "u1")); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if !slices.Equal(*ops, []string{"leave coOwned", "delete board solo", "delete user u1"}) {
		t.Errorf("書き込み = %v", *ops)
	}
}
