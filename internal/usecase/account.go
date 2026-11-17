package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// AccountService は利用者がその持ち物ごと消える口（ADR 0101）。
//
// **消すのは、その人だけが owner のボードと、その人のメンバーの行と、利用者の
// 行。** ほかにも owner がいるボードは消さずに、メンバーから外れるだけにする。
// 共有していても、その人だけが owner なら消す。誰に移すかを etoki が決めると
// 中核思想 3 に反し、owner が共有したボードを消せるのは今と同じ（ADR 0042）。
// 残したいなら、先に参加者をオーナーにしてもらう。
//
// **消す順番は、ボード → メンバーの行 → 利用者の行。** 利用者の行を先に消すと、
// 持ち主の無いボードが残る（`board_members` には外部キーが無い、ADR 0016）。
// 途中で失敗したら、消えたものは戻さない。もう一度消せば続きから消える。
type AccountService struct {
	boards   port.BoardRepository
	boardSvc *BoardService
	accounts port.AccountRepository
}

// NewAccountService は AccountService を作る。
//
// ボードを消すのは `boardSvc` に任せ、排他も `boardSvc` と同じもの（作成と同じ
// `BoardLocks`）を使う（ADR 0042）。ここで書き直すと、作成の途中で消さない守りが
// 2 箇所になる。
func NewAccountService(
	boards port.BoardRepository, boardSvc *BoardService, accounts port.AccountRepository,
) *AccountService {
	return &AccountService{boards: boards, boardSvc: boardSvc, accounts: accounts}
}

// AccountDeletion は利用者を消すと失われるもの。
//
// **押す前に見せるための口**（ADR 0042 と同じ形、中核思想 3）。引いた時点の
// 中身であって、Delete の前提条件ではない（`BoardService.Deletion` と同じ理由）。
type AccountDeletion struct {
	// Boards は一緒に消えるボード（その人だけが owner）。
	Boards []DeletedBoard
	// LeftBoardCount はメンバーから外れるだけのボードの数。ボードは、ほかの
	// メンバーのもとに残る。
	LeftBoardCount int
}

// DeletedBoard は一緒に消えるボード 1 枚。
type DeletedBoard struct {
	ID   string
	Name string
	// OtherMemberCount は、その人のほかのメンバーの数。1 以上なら共有していて、
	// 消すとその人たちもボードを失う。
	OtherMemberCount int
	// Losing はボードの削除で etoki から失われるもの（ADR 0042）。
	Losing BoardDeletion
}

// membership は利用者が抜けるときの、ボード 1 枚の扱い。
type membership struct {
	board   port.Board
	members []port.BoardMember
	// soleOwner はその人だけが owner か。真ならボードごと消える。
	soleOwner bool
}

// Deletion は利用者を消すと失われるものを返す。
func (s *AccountService) Deletion(ctx context.Context) (AccountDeletion, error) {
	user, err := userToDelete(ctx)
	if err != nil {
		return AccountDeletion{}, err
	}
	memberships, err := s.memberships(ctx, user)
	if err != nil {
		return AccountDeletion{}, err
	}

	out := AccountDeletion{Boards: []DeletedBoard{}}
	for _, m := range memberships {
		if !m.soleOwner {
			out.LeftBoardCount++
			continue
		}
		losing, err := s.boardSvc.Deletion(ctx, m.board.ID)
		// 引いてから数えるまでのあいだに消えたボードは、失われるものに数えない。
		if errors.Is(err, ErrBoardNotFound) {
			continue
		}
		if err != nil {
			return AccountDeletion{}, err
		}
		out.Boards = append(out.Boards, DeletedBoard{
			ID:               m.board.ID,
			Name:             m.board.Name,
			OtherMemberCount: len(m.members) - 1,
			Losing:           losing,
		})
	}
	return out, nil
}

// Delete は利用者を、その人だけが owner のボードとメンバーの行ごと消す。
//
// **どれを消すかは、ここで引き直して決める。** Deletion で見せた中身を照合
// しない（見せた数を前提条件にしない、`BoardService.Deletion`）。
func (s *AccountService) Delete(ctx context.Context) error {
	user, err := userToDelete(ctx)
	if err != nil {
		return err
	}
	listed, err := s.boards.List(ctx, user)
	if err != nil {
		return err
	}

	for _, a := range listed {
		if err := s.leave(ctx, a.Board.ID, user); err != nil {
			return err
		}
	}

	return s.accounts.DeleteUser(ctx, user)
}

// leave は 1 枚のボードから抜ける。その人だけが owner ならボードごと消し、
// そうでなければメンバーの行だけを外す。
//
// **どちらにするかは、ボードの排他の中で数え直して決める。** 外で数えた owner の
// 数は、ほかの owner が抜けると古くなる。古い数のまま外すと、owner のいない
// ボードが残る（`BoardMemberService.Remove` が排他の中で数えるのと同じ理由）。
// 排他は作成と同じものなので、作成の途中で消さない守り（ADR 0042）もこれで効く。
func (s *AccountService) leave(ctx context.Context, boardID, user string) error {
	release, err := s.boardSvc.locks.Acquire(ctx, boardID)
	if err != nil {
		return err
	}
	defer release()

	// 引いたボードはメンバーとして見えていたもの（`List`）なので、`ListMembers` の
	// 前の確かめ（`port.BoardRepository` の注記）は済んでいる。そのあとで外れて
	// いたら、下の RemoveMember が「もう無い」と答える。
	members, err := s.boards.ListMembers(ctx, boardID)
	if err != nil {
		return err
	}

	if soleOwner(members, user) {
		// 消えていたなら、消す手間が省けただけ。
		if err := s.boardSvc.deleteLocked(ctx, boardID); err != nil &&
			!errors.Is(err, ErrBoardNotFound) {
			return fmt.Errorf("delete board %s: %w", boardID, err)
		}
		return nil
	}
	if err := s.boards.RemoveMember(ctx, boardID, user); err != nil &&
		!errors.Is(err, port.ErrNotFound) {
		return fmt.Errorf("leave board %s: %w", boardID, err)
	}
	return nil
}

// memberships は利用者がメンバーであるボードと、抜けたときの扱いを返す。
// 押す前に見せるためのもので、消すときは `leave` が排他の中で決め直す。
//
// `List` は操作者がメンバーであるボードだけを返すので、`ListMembers` の前の
// 確かめ（`port.BoardRepository` の注記）はこれで済んでいる。`Find` で引き
// 直しても同じ問いを 2 度訊くだけになる。
func (s *AccountService) memberships(ctx context.Context, user string) ([]membership, error) {
	listed, err := s.boards.List(ctx, user)
	if err != nil {
		return nil, err
	}

	out := make([]membership, 0, len(listed))
	for _, a := range listed {
		members, err := s.boards.ListMembers(ctx, a.Board.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, membership{
			board:     a.Board,
			members:   members,
			soleOwner: soleOwner(members, user),
		})
	}
	return out, nil
}

// soleOwner は user がそのボードのただ 1 人の owner か。真なら、抜けるとボードを
// 管理できる人がいなくなるので、ボードごと消す。
func soleOwner(members []port.BoardMember, user string) bool {
	owners, isOwner := 0, false
	for _, m := range members {
		if m.Role != port.RoleOwner {
			continue
		}
		owners++
		if m.UserID == user {
			isOwner = true
		}
	}
	return isOwner && owners == 1
}

// userToDelete は消す利用者を ctx から返す。
//
// **空文字は消さない。** 空文字は無効値ではなく「認証なしの所有者」1 人で
// （ADR 0016）、消すとその構成の全ボードが消える。口を開くのは認証を設定した
// 構成だけ（`etoki.New`）だが、ここでも断る。
func userToDelete(ctx context.Context) (string, error) {
	user := actorOf(ctx)
	if user == "" {
		return "", fmt.Errorf("%w: the unauthenticated owner cannot be deleted", ErrInvalidInput)
	}
	return user, nil
}
