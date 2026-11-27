package usecase

import (
	"context"
	"log/slog"

	"github.com/yusuke0610/etoki/port"
)

// ProjectAccess は作成先の Project に書けるかどうかの、いまの状態。
//
// **判定ではない。** 実際に作れるかは作成時に GitHub が返したものが正しい。
// これは画面に理由を出すための材料で、これを見て弾くことはしない（ADR 0017）。
type ProjectAccess string

// ProjectAccess の取りうる値。
const (
	// ProjectAccessAllowed は書けることを表す。
	ProjectAccessAllowed ProjectAccess = "allowed"
	// ProjectAccessDenied は書けないことを表す。GitHub の資格情報に、その
	// Project への書き込み権限が無い。
	ProjectAccessDenied ProjectAccess = "denied"
	// ProjectAccessUnknown は確かめられなかったことを表す。
	//
	// GitHub を設定していない、作成先が未選択、問い合わせに失敗した、のどれか。
	// **allowed か denied のどちらかに倒さない。** 倒すと、確かめていないことを
	// 確かめたように見せることになる（中核思想 3）。
	ProjectAccessUnknown ProjectAccess = "unknown"
)

// BoardAccessState は操作者から見たボードの権限。
type BoardAccessState struct {
	// ProjectAccess は GitHub 側の書き込み可否。
	ProjectAccess ProjectAccess
}

// BoardAccessService は作成先の Project に書けるかを返す（ADR 0017）。
//
// **判定ではなく状態。** 実際に作れるかは作成時に GitHub が返したものが正しい。
type BoardAccessService struct {
	boardGuard
	// github は nil でもよい。未設定なら ProjectAccessUnknown を返す。
	github port.GitHubClient
	logger *slog.Logger
}

// NewBoardAccessService は BoardAccessService を作る。
func NewBoardAccessService(
	boards port.BoardRepository, github port.GitHubClient, logger *slog.Logger,
) *BoardAccessService {
	if logger == nil {
		logger = slog.Default()
	}
	return &BoardAccessService{
		boardGuard: boardGuard{boards: boards},
		github:     github,
		logger:     logger,
	}
}

// Get は操作者から見たボードの権限を返す。
func (s *BoardAccessService) Get(ctx context.Context, boardID string) (BoardAccessState, error) {
	acc, err := s.access(ctx, boardID)
	if err != nil {
		return BoardAccessState{}, err
	}

	return BoardAccessState{ProjectAccess: s.projectAccess(ctx, acc.Board.Target)}, nil
}

// projectAccess は GitHub 側の書き込み可否を確かめる。
//
// **確かめられなかったことをエラーにしない。** ここで失敗を返すと、GitHub が
// 落ちているだけでボードの権限すら表示できなくなる。分からないことは
// 分からないとして返す。
func (s *BoardAccessService) projectAccess(
	ctx context.Context, target port.BoardTarget,
) ProjectAccess {
	if s.github == nil || !target.Selected() {
		return ProjectAccessUnknown
	}

	ok, err := s.github.CanWriteProject(ctx, target.ProjectID)
	if err != nil {
		s.logger.WarnContext(ctx, "could not check project write access",
			slog.String("projectId", target.ProjectID), slog.Any("error", err))
		return ProjectAccessUnknown
	}
	if !ok {
		return ProjectAccessDenied
	}

	return ProjectAccessAllowed
}
