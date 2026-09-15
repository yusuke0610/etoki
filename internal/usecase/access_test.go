package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// ロールごとに何ができるかを 1 つの表で固定する（ADR 0017）。
//
// **通る側ではなく落ちる側を書く。** 判定は boardGuard に閉じているので、
// 操作を足したときにロールを見ないまま通る取りこぼしは、ここに行が増えないこと
// ではなく、期待した 403 が出ないことで現れる。
func TestRolePermissions(t *testing.T) {
	t.Parallel()

	// call は 1 つの操作。ロールだけを変えて同じものを呼ぶ。
	type call struct {
		name string
		do   func(ctx context.Context, boards *fakeBoards) error
	}

	calls := []call{
		{"閲覧", func(ctx context.Context, boards *fakeBoards) error {
			_, err := newBoardService(boards).Find(ctx, "board-1")
			return err
		}},
		{"注釈の状態", func(ctx context.Context, boards *fakeBoards) error {
			_, _, err := usecase.NewAnnotationService(boards, &fakeMappings{}).
				ListStates(ctx, "board-1")
			return err
		}},
		{"実行履歴", func(ctx context.Context, boards *fakeBoards) error {
			_, err := usecase.NewAnnotationService(boards, &fakeMappings{}).
				ListRuns(ctx, "board-1", "annot-1")
			return err
		}},
		{"改名", func(ctx context.Context, boards *fakeBoards) error {
			return newBoardService(boards).Rename(ctx, "board-1", "新しい名前")
		}},
		{"シーン保存", func(ctx context.Context, boards *fakeBoards) error {
			_, err := newBoardService(boards).SaveScene(ctx, "board-1", emptyScene, baseTime)
			return err
		}},
		{"解釈", func(ctx context.Context, boards *fakeBoards) error {
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			_, err := usecase.NewInterpretationService(boards, &fakeMappings{}, llm, newLimiter()).
				Interpret(ctx, "board-1", "annot-1", nil)
			return err
		}},
		{"図のドラフト", func(ctx context.Context, boards *fakeBoards) error {
			llm := &fakeLLM{responses: []string{validMermaid}}
			_, err := usecase.NewDiagramService(boards, llm, newLimiter()).
				Generate(ctx, "board-1", req("段取り"))
			return err
		}},
		{"作成", func(ctx context.Context, boards *fakeBoards) error {
			gh := &fakeGitHub{fields: projectFields()}
			_, err := usecase.NewCreationService(boards, &fakeMappings{}, gh,
				usecase.NewBoardLocks()).
				Create(ctx, "board-1", "annot-1", currentContentHash(t), interpretation())
			return err
		}},
		{"作成先の変更", func(ctx context.Context, boards *fakeBoards) error {
			return newBoardService(boards).SetTarget(ctx, "board-1", newTarget())
		}},
		{"削除", func(ctx context.Context, boards *fakeBoards) error {
			return newBoardService(boards).Delete(ctx, "board-1")
		}},
		{"削除で失われるもの", func(ctx context.Context, boards *fakeBoards) error {
			_, err := newBoardService(boards).Deletion(ctx, "board-1")
			return err
		}},
	}

	// allowed はその操作を通せる最小のロール。表の本体。
	allowed := map[string]port.BoardRole{
		"閲覧":    port.RoleViewer,
		"注釈の状態": port.RoleViewer,
		"実行履歴":  port.RoleViewer,
		// 名前はブレストの中身に属する表示物で、取り消せない作成の行き先を
		// 決めるものではない。シーンを書き換えられる editor に、表示名だけ
		// 直させない理由が無い（作成先の変更は owner だけ）。
		"改名":    port.RoleEditor,
		"シーン保存": port.RoleEditor,
		"解釈":    port.RoleEditor,
		// 生成も LLM を叩く外部呼び出しで課金を伴う。閲覧者に外部呼び出しを
		// 許すのは「閲覧」ではない（解釈と同じ理由）。
		"図のドラフト": port.RoleEditor,
		"作成":     port.RoleEditor,
		"作成先の変更": port.RoleOwner,
		// ボードごと畳む操作なので、シーンを書き換えられる editor ではなく、
		// 招待も作成先の変更もできる相手に限る（ADR 0042）。
		"削除": port.RoleOwner,
		// 押せない相手に、押したときに何が起きるかだけを見せる理由が無い。
		// 削除そのものと揃える。
		"削除で失われるもの": port.RoleOwner,
	}

	roles := []port.BoardRole{port.RoleOwner, port.RoleEditor, port.RoleViewer}

	for _, c := range calls {
		for _, role := range roles {
			t.Run(c.name+"/"+string(role), func(t *testing.T) {
				t.Parallel()

				boards := &fakeBoards{board: newBoard(interpretScene), role: role}
				err := c.do(t.Context(), boards)

				if role.AtLeast(allowed[c.name]) {
					if errors.Is(err, usecase.ErrForbidden) {
						t.Fatalf("%s は %s に許すはずが 403: %v", c.name, role, err)
					}
					return
				}

				if !errors.Is(err, usecase.ErrForbidden) {
					t.Fatalf("%s を %s に許している: err = %v", c.name, role, err)
				}
				if boards.writes != 0 {
					t.Errorf("弾いたのに書き込んでいる: writes = %d", boards.writes)
				}
			})
		}

		// メンバーでなければ、ロール不足ではなく「無い」として返す。区別すると
		// ID を総当たりして他人のボードの存在を確かめられる（ADR 0016 / 0017）。
		t.Run(c.name+"/非メンバー", func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene), owner: "someone-else"}
			err := c.do(t.Context(), boards)

			if !errors.Is(err, usecase.ErrBoardNotFound) {
				t.Fatalf("%s = %v, want ErrBoardNotFound", c.name, err)
			}
			if errors.Is(err, usecase.ErrForbidden) {
				t.Error("メンバーでないことを 403 で伝えている")
			}
		})
	}
}

// emptyScene は保存できる最小のシーン。ユースケース側の定数は非公開なので、
// テストからは同じ内容をここに置く。
const emptyScene = `{"type":"excalidraw","version":2,"source":"etoki","elements":[],"appState":{}}`

func newBoardService(boards *fakeBoards) *usecase.BoardService {
	return usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())
}
