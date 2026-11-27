package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
)

// ボードに触る操作を 1 つの表に並べ、メンバーなら通り、メンバーでなければ
// 「無い」で返ることを固定する（ADR 0016 / 0107）。
//
// **落ちる側を書く。** 判定は boardGuard に閉じているので、操作を足したときに
// 判定を通さないまま引き当てる取りこぼしは、ここに行が増えないことではなく、
// 非メンバーに ErrBoardNotFound が返らないことで現れる。
func TestBoardGuard(t *testing.T) {
	t.Parallel()

	// call は 1 つの操作。操作者だけを変えて同じものを呼ぶ。
	type call struct {
		name string
		do   func(ctx context.Context, boards *fakeBoards) error
	}

	calls := []call{
		{"閲覧", func(ctx context.Context, boards *fakeBoards) error {
			_, err := newBoardService(boards).Find(ctx, "board-1")
			return err
		}},
		// 開く口は Find とは別の引き当て（FindWithFiles）を通る（ADR 0074）。
		// 判定を写し損ねると、画像だけ他人に読ませることになる。
		{"開く", func(ctx context.Context, boards *fakeBoards) error {
			_, err := newBoardService(boards).Open(ctx, "board-1")
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
		{"背景の書き換え", func(ctx context.Context, boards *fakeBoards) error {
			return newBoardService(boards).SetPremise(ctx, "board-1", "", "Go と React")
		}},
		{"シーン保存", func(ctx context.Context, boards *fakeBoards) error {
			_, err := newBoardService(boards).SaveScene(ctx, "board-1", emptyScene, nil, baseTime)
			return err
		}},
		{"解釈", func(ctx context.Context, boards *fakeBoards) error {
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			// 判定は始める時点で済む（jobTable.start の prepare）。走らせたあとの
			// ジョブは LLM しか触らないので、待たずに返してよい。
			_, err := usecase.NewInterpretationJobs(usecase.NewInterpretationService(
				boards, &fakeMappings{}, llm, newLimiter())).
				Start(ctx, "board-1", "annot-1", nil)
			return err
		}},
		{"図のドラフト", func(ctx context.Context, boards *fakeBoards) error {
			llm := &fakeLLM{responses: []string{validMermaid}}
			_, err := usecase.NewDiagramJobs(usecase.NewDiagramService(boards, llm, newLimiter())).
				Start(ctx, "board-1", req("段取り"))
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

	for _, c := range calls {
		// メンバーなら、どの操作も通す（ADR 0107）。ロールで分けない。
		t.Run(c.name+"/メンバー", func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			if err := c.do(t.Context(), boards); err != nil {
				t.Fatalf("%s = %v, want 通る", c.name, err)
			}
		})

		// メンバーでなければ「無い」として返す。区別すると ID を総当たりして
		// 他人のボードの存在を確かめられる（ADR 0016）。
		t.Run(c.name+"/非メンバー", func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene), owner: "someone-else"}
			err := c.do(t.Context(), boards)

			if !errors.Is(err, usecase.ErrBoardNotFound) {
				t.Fatalf("%s = %v, want ErrBoardNotFound", c.name, err)
			}
			if boards.writes != 0 {
				t.Errorf("弾いたのに書き込んでいる: writes = %d", boards.writes)
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
