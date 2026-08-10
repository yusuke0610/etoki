package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// failingGitHub は CanWriteProject が失敗する GitHubClient。
type failingGitHub struct {
	fakeGitHub
}

func (f *failingGitHub) CanWriteProject(context.Context, string) (bool, error) {
	return false, errors.New("github is down")
}

func TestBoardAccess(t *testing.T) {
	t.Parallel()

	withTarget := newBoard(interpretScene)
	noTarget := newBoard(interpretScene)
	noTarget.Target = port.BoardTarget{}

	tests := map[string]struct {
		board  *port.Board
		github port.GitHubClient
		want   usecase.ProjectAccess
	}{
		"書ける":         {withTarget, &fakeGitHub{canWrite: true}, usecase.ProjectAccessAllowed},
		"書けない":        {withTarget, &fakeGitHub{}, usecase.ProjectAccessDenied},
		"GitHub が未設定": {withTarget, nil, usecase.ProjectAccessUnknown},
		"作成先が未選択":     {noTarget, &fakeGitHub{canWrite: true}, usecase.ProjectAccessUnknown},
		"問い合わせに失敗した":  {withTarget, &failingGitHub{}, usecase.ProjectAccessUnknown},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: tt.board, role: port.RoleEditor}
			svc := usecase.NewBoardAccessService(boards, tt.github, nil)

			got, err := svc.Get(t.Context(), "board-1")
			// 確かめられないことをエラーにしない。GitHub が落ちているだけで
			// ボードの権限すら表示できなくなる（ADR 0017）。
			if err != nil {
				t.Fatalf("Get() = %v", err)
			}
			if got.ProjectAccess != tt.want {
				t.Errorf("ProjectAccess = %q, want %q", got.ProjectAccess, tt.want)
			}
			if got.Role != port.RoleEditor {
				t.Errorf("Role = %q, want editor", got.Role)
			}
		})
	}
}

// viewer でも自分の権限は見られる。何ができないのかが分からないと、
// できない理由を画面に出せない。
func TestBoardAccess_AllowsViewer(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene), role: port.RoleViewer}
	svc := usecase.NewBoardAccessService(boards, &fakeGitHub{canWrite: true}, nil)

	got, err := svc.Get(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.Role != port.RoleViewer {
		t.Errorf("Role = %q, want viewer", got.Role)
	}
}

func TestBoardAccess_HidesBoardFromNonMembers(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene), owner: "someone-else"}
	svc := usecase.NewBoardAccessService(boards, &fakeGitHub{canWrite: true}, nil)

	if _, err := svc.Get(t.Context(), "board-1"); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Fatalf("Get() = %v, want ErrBoardNotFound", err)
	}
}
