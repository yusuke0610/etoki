package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// メンバー操作に固有のエラー。
var (
	// ErrAlreadyMember はすでにメンバーであることを表す。
	//
	// 黙ってロールを書き換えない。「招待した」と「ロールを変えた」を
	// 呼び出し側が区別できなくなる。
	ErrAlreadyMember = errors.New("etoki: already a member")

	// ErrLastOwner は最後の owner を外そう・降格しようとしたことを表す。
	//
	// 通すと、誰も招待できず作成先も変えられないボードが残る。所有者のいない
	// ボードは etoki claim でしか戻せない（ADR 0016）ので、画面からは直せない。
	ErrLastOwner = errors.New("etoki: board would have no owner")
)

// Member は表示に必要な情報を添えたメンバー。
//
// login と表示名は users にあり、board_members には無い。境界に返すには
// 両方が要るので、ここで組み立てる。
type Member struct {
	// Membership はロールと ID。
	Membership port.BoardMember
	// Login は認証基盤上のログイン名。利用者を引けなければ空文字。
	Login string
	// DisplayName は画面に出す名前。利用者を引けなければ空文字。
	DisplayName string
}

// userDirectory は招待と表示に必要な利用者の引き当て。
//
// *AuthService が満たす。認証基盤の識別子は AuthService が持っているので、
// 呼ぶ側が provider を持ち回らずに済む。認証を設定していない構成では
// BoardMemberService を組み立てない。招待は認証が無いと意味を持たない。
type userDirectory interface {
	FindUserByLogin(ctx context.Context, login string) (*port.User, error)
	FindUsers(ctx context.Context, ids []string) ([]port.User, error)
}

// BoardMemberService はボードのメンバーを扱う。
//
// **招待するのに GitHub は見ない。** ブレストに呼ぶ相手と GitHub に書ける相手は
// 同じではなく、リポジトリの権限を etoki 側に複製しないと決めた（ADR 0017）。
type BoardMemberService struct {
	boardGuard
	users userDirectory
	now   func() time.Time
}

// BoardMemberServiceOption は BoardMemberService の依存を差し替える。
type BoardMemberServiceOption func(*BoardMemberService)

// WithMemberClock は時刻の取得方法を差し替える。
func WithMemberClock(f func() time.Time) BoardMemberServiceOption {
	return func(s *BoardMemberService) { s.now = f }
}

// NewBoardMemberService は BoardMemberService を作る。
func NewBoardMemberService(
	boards port.BoardRepository, users userDirectory,
	opts ...BoardMemberServiceOption,
) *BoardMemberService {
	s := &BoardMemberService{
		boardGuard: boardGuard{boards: boards},
		users:      users,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// List はボードのメンバーを返す。
//
// メンバーなら誰でも見られる。誰と共有しているかを owner だけが知っている
// 状態にすると、招待された側は自分が何に呼ばれたのか分からない。
func (s *BoardMemberService) List(ctx context.Context, boardID string) ([]Member, error) {
	if _, err := s.access(ctx, boardID, port.RoleViewer); err != nil {
		return nil, err
	}

	members, err := s.boards.ListMembers(ctx, boardID)
	if err != nil {
		return nil, err
	}

	return s.withUsers(ctx, members)
}

// Invite は login で指した利用者をメンバーに加える。
func (s *BoardMemberService) Invite(
	ctx context.Context, boardID, login string, role port.BoardRole,
) (Member, error) {
	if login == "" {
		return Member{}, fmt.Errorf("%w: login is required", ErrInvalidInput)
	}
	if !role.Valid() {
		return Member{}, fmt.Errorf("%w: unknown role %q", ErrInvalidInput, role)
	}
	if _, err := s.access(ctx, boardID, port.RoleOwner); err != nil {
		return Member{}, err
	}

	user, err := s.users.FindUserByLogin(ctx, login)
	if err != nil {
		return Member{}, err
	}
	if user == nil {
		// 未ログインの login 宛に招待を積まない。login は改名で変わるので、
		// 空いた login を取った別人に権限が渡る（ADR 0017）。
		return Member{}, fmt.Errorf(
			"%w: %q has not signed in to etoki yet", ErrInvalidInput, login)
	}

	m := port.BoardMember{
		BoardID:   boardID,
		UserID:    user.ID,
		Role:      role,
		CreatedAt: s.now(),
	}
	if err := s.boards.AddMember(ctx, m); err != nil {
		if errors.Is(err, port.ErrAlreadyExists) {
			return Member{}, fmt.Errorf("%w: %s", ErrAlreadyMember, login)
		}
		return Member{}, err
	}

	return Member{Membership: m, Login: user.Login, DisplayName: user.DisplayName}, nil
}

// SetRole はメンバーのロールを変える。
func (s *BoardMemberService) SetRole(
	ctx context.Context, boardID, userID string, role port.BoardRole,
) (Member, error) {
	if !role.Valid() {
		return Member{}, fmt.Errorf("%w: unknown role %q", ErrInvalidInput, role)
	}
	if _, err := s.access(ctx, boardID, port.RoleOwner); err != nil {
		return Member{}, err
	}

	if role != port.RoleOwner {
		if err := s.ensureAnotherOwner(ctx, boardID, userID); err != nil {
			return Member{}, err
		}
	}

	if err := s.boards.UpdateMemberRole(ctx, boardID, userID, role); err != nil {
		return Member{}, err
	}

	members, err := s.boards.ListMembers(ctx, boardID)
	if err != nil {
		return Member{}, err
	}
	for _, m := range members {
		if m.UserID != userID {
			continue
		}
		withUsers, err := s.withUsers(ctx, []port.BoardMember{m})
		if err != nil {
			return Member{}, err
		}
		return withUsers[0], nil
	}

	// UpdateMemberRole を通った直後なので、消えているのは異常事態。
	return Member{}, fmt.Errorf("member %s of board %s: %w", userID, boardID, port.ErrNotFound)
}

// Remove はメンバーを外す。
func (s *BoardMemberService) Remove(ctx context.Context, boardID, userID string) error {
	if _, err := s.access(ctx, boardID, port.RoleOwner); err != nil {
		return err
	}
	if err := s.ensureAnotherOwner(ctx, boardID, userID); err != nil {
		return err
	}

	return s.boards.RemoveMember(ctx, boardID, userID)
}

// ensureAnotherOwner は userID を外しても owner が残ることを確かめる。
//
// 「自分自身か」ではなく「他に owner がいるか」で見る。owner が 2 人いれば
// 自分が抜けてよく、1 人しかいなければ、それが誰であっても抜けさせられない。
func (s *BoardMemberService) ensureAnotherOwner(
	ctx context.Context, boardID, userID string,
) error {
	members, err := s.boards.ListMembers(ctx, boardID)
	if err != nil {
		return err
	}

	for _, m := range members {
		if m.Role == port.RoleOwner && m.UserID != userID {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrLastOwner, boardID)
}

// withUsers はメンバーに login と表示名を添える。
//
// 引けなかった ID は空文字のまま返す。利用者を消してもボードは残る（ADR 0016）
// ので、指し先の無いメンバーは普通に起こる。ここで誤りにすると、メンバー一覧
// そのものが開けなくなる。
func (s *BoardMemberService) withUsers(
	ctx context.Context, members []port.BoardMember,
) ([]Member, error) {
	ids := make([]string, 0, len(members))
	for _, m := range members {
		ids = append(ids, m.UserID)
	}

	users, err := s.users.FindUsers(ctx, ids)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]port.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}

	out := make([]Member, 0, len(members))
	for _, m := range members {
		member := Member{Membership: m}
		if u, ok := byID[m.UserID]; ok {
			member.Login, member.DisplayName = u.Login, u.DisplayName
		}
		out = append(out, member)
	}

	return out, nil
}
