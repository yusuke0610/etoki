package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// fakeBoards は解釈に必要な Find だけを持つボードリポジトリ。
//
// 書き込み系が呼ばれたら数える。解釈は読むだけで何も残さない、という約束を
// テストから確かめるため。
type fakeBoards struct {
	board  *port.Board
	writes int
}

func (f *fakeBoards) Find(_ context.Context, id string) (*port.Board, error) {
	if f.board == nil || f.board.ID != id {
		return nil, nil
	}
	return f.board, nil
}

func (f *fakeBoards) Create(context.Context, port.Board) error {
	f.writes++
	return nil
}

func (f *fakeBoards) UpdateScene(context.Context, string, string, time.Time) error {
	f.writes++
	return nil
}

func (f *fakeBoards) List(context.Context) ([]port.Board, error) { return nil, nil }

// fakeLLM は決められた応答を順に返す LLMClient。
//
// 応答を使い切ったら最後のものを返し続ける。上限まで失敗し続ける場合を
// 書きやすくするため。
type fakeLLM struct {
	responses []string
	err       error
	requests  []port.VisionRequest
}

func (f *fakeLLM) Complete(_ context.Context, req port.VisionRequest) (port.VisionResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return port.VisionResponse{}, f.err
	}

	i := min(len(f.requests)-1, len(f.responses)-1)
	return port.VisionResponse{Text: f.responses[i]}, nil
}

const interpretScene = `{"type":"excalidraw","elements":[
	{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":""}}},
	{"id":"t1","type":"text","text":"Stripe の SDK が古い","frameId":"annot-1"},
	{"id":"t2","type":"text","text":"返金導線が無い","frameId":"annot-1"},
	{"id":"t9","type":"text","text":"囲みの外のメモ"}]}`

const validLLMOutput = `{"summary":"決済まわりの課題出し。SDK 更新と返金導線の 2 件。","items":[
	{"localId":"e1","kind":"epic","title":"決済まわりの整備","body":"","parentLocalId":null},
	{"localId":"i1","kind":"issue","title":"Stripe SDK の更新","body":"","parentLocalId":"e1"}]}`

// title が空なので items[0].title で弾かれる。
const invalidLLMOutput = `{"summary":"決済まわりの課題出し","items":[
	{"localId":"e1","kind":"epic","title":"","body":"","parentLocalId":null}]}`

func newBoard(scene string) *port.Board {
	return &port.Board{ID: "board-1", Name: "設計会", Scene: scene}
}

func newInterpretService(t *testing.T, board *port.Board, llm *fakeLLM) (*usecase.InterpretationService, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: board}
	svc := usecase.NewInterpretationService(boards, llm, usecase.WithMaxAttempts(3))
	return svc, boards
}

func TestInterpret_Succeeds(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, boards := newInterpretService(t, newBoard(interpretScene), llm)

	in, err := svc.Interpret(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("Interpret() = %v", err)
	}

	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
	if len(in.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(in.Items))
	}
	if in.Items[0].Kind != domain.KindEpic {
		t.Errorf("Items[0].Kind = %q", in.Items[0].Kind)
	}
	if in.Summary == "" {
		t.Error("Summary が空")
	}

	// 解釈は読むだけ。ボードにも実行記録にも何も書かない。
	if boards.writes != 0 {
		t.Errorf("ボードへの書き込み = %d 回, want 0", boards.writes)
	}
}

func TestInterpret_BuildsPrompt(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1"); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}

	req := llm.requests[0]

	for _, want := range []string{"Stripe の SDK が古い", "返金導線が無い", "決済まわり"} {
		if !strings.Contains(req.Text, want) {
			t.Errorf("プロンプトに %q が無い:\n%s", want, req.Text)
		}
	}
	// 囲みの外のテキストは注釈の内容ではない。
	if strings.Contains(req.Text, "囲みの外のメモ") {
		t.Errorf("囲みの外のテキストがプロンプトに入っている:\n%s", req.Text)
	}
	if !strings.Contains(req.Text, "粒度の指定はありません") {
		t.Errorf("粒度の指示が無い:\n%s", req.Text)
	}
	if !strings.Contains(req.System, "epic") || !strings.Contains(req.System, "localId") {
		t.Errorf("システム指示にスキーマの説明が無い:\n%s", req.System)
	}
	// 画像は #9 で扱う。今は載せない。
	if len(req.Images) != 0 {
		t.Errorf("len(Images) = %d, want 0", len(req.Images))
	}
}

func TestInterpret_PassesGranularityInstruction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		granularity string
		want        string
	}{
		{granularity: "epic", want: "epic 相当"},
		{granularity: "issue", want: "issue 相当"},
	}

	for _, tt := range tests {
		t.Run(tt.granularity, func(t *testing.T) {
			t.Parallel()

			scene := strings.Replace(interpretScene, `"granularity":""`,
				`"granularity":"`+tt.granularity+`"`, 1)

			// epic 指定でも issue 指定でも通る出力を返す必要はない。
			// プロンプトさえ確かめられればよいので、失敗しても構わない。
			llm := &fakeLLM{responses: []string{validLLMOutput}}
			svc, _ := newInterpretService(t, newBoard(scene), llm)

			_, _ = svc.Interpret(t.Context(), "board-1", "annot-1")

			if len(llm.requests) == 0 {
				t.Fatal("LLM が呼ばれていない")
			}
			if !strings.Contains(llm.requests[0].Text, tt.want) {
				t.Errorf("プロンプトに %q が無い:\n%s", tt.want, llm.requests[0].Text)
			}
		})
	}
}

// 再送は「何が悪かったか」を伝えて初めて意味を持つ。
func TestInterpret_RetriesWithCorrections(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{invalidLLMOutput, validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	in, err := svc.Interpret(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("Interpret() = %v", err)
	}
	if len(in.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2", len(in.Items))
	}

	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}

	retry := llm.requests[1].Text
	if !strings.Contains(retry, "items[0].title") {
		t.Errorf("再送に検証エラーの箇所が含まれていない:\n%s", retry)
	}
	if !strings.Contains(retry, "title を空にはできません") {
		t.Errorf("再送に検証エラーの内容が含まれていない:\n%s", retry)
	}
	if !strings.Contains(retry, invalidLLMOutput) {
		t.Errorf("再送に直前の出力が含まれていない:\n%s", retry)
	}
	// VisionRequest は会話履歴を運べないので、再送も自己完結している必要がある。
	if !strings.Contains(retry, "Stripe の SDK が古い") {
		t.Errorf("再送に元の入力が含まれていない:\n%s", retry)
	}
}

func TestInterpret_GivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{invalidLLMOutput}}
	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewInterpretationService(boards, llm, usecase.WithMaxAttempts(2))

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1")
	if !errors.Is(err, usecase.ErrInterpretationFailed) {
		t.Fatalf("Interpret() = %v, want ErrInterpretationFailed", err)
	}

	if len(llm.requests) != 2 {
		t.Errorf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	// 最後の指摘が残っていないと、開発者は何が起きたのか分からない。
	if !strings.Contains(err.Error(), "items[0].title") {
		t.Errorf("エラーに最後の指摘が含まれていない: %v", err)
	}
}

func TestInterpret_AcceptsFencedOutput(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"コードフェンス付き":  "```json\n" + validLLMOutput + "\n```",
		"言語指定なしフェンス": "```\n" + validLLMOutput + "\n```",
		"前置きと後書き付き":  "解釈しました。\n" + validLLMOutput + "\n以上です。",
	}

	for name, output := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			llm := &fakeLLM{responses: []string{output}}
			svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

			in, err := svc.Interpret(t.Context(), "board-1", "annot-1")
			if err != nil {
				t.Fatalf("Interpret() = %v", err)
			}
			if len(in.Items) != 2 {
				t.Errorf("len(Items) = %d, want 2", len(in.Items))
			}
			// 1 往復で読めたなら再送していないはず。
			if len(llm.requests) != 1 {
				t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
			}
		})
	}
}

func TestInterpret_RetriesOnUnparsableOutput(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{"JSON は出せません。", validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1"); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	if !strings.Contains(llm.requests[1].Text, "指摘:") {
		t.Errorf("再送に指摘が含まれていない:\n%s", llm.requests[1].Text)
	}
}

// 接続やモデル側の失敗は修正指示で直る問題ではないので再送しない。
func TestInterpret_DoesNotRetryTransportErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("api key is invalid")
	llm := &fakeLLM{err: wantErr}
	svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Interpret() = %v, want %v", err, wantErr)
	}
	// スキーマ違反とは原因も打ち手も違うので、呼び出し側が区別できる必要がある。
	if !errors.Is(err, usecase.ErrLLMUnavailable) {
		t.Errorf("Interpret() = %v, want ErrLLMUnavailable", err)
	}
	if errors.Is(err, usecase.ErrInterpretationFailed) {
		t.Errorf("接続の失敗がスキーマ違反として扱われている: %v", err)
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

func TestInterpret_NotFound(t *testing.T) {
	t.Parallel()

	t.Run("ボードが無い", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

		_, err := svc.Interpret(t.Context(), "no-such-board", "annot-1")
		if !errors.Is(err, usecase.ErrBoardNotFound) {
			t.Fatalf("Interpret() = %v, want ErrBoardNotFound", err)
		}
		if len(llm.requests) != 0 {
			t.Error("ボードが無いのに LLM を呼んでいる")
		}
	})

	t.Run("注釈が無い", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(interpretScene), llm)

		_, err := svc.Interpret(t.Context(), "board-1", "no-such-annot")
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("Interpret() = %v, want ErrAnnotationNotFound", err)
		}
		if len(llm.requests) != 0 {
			t.Error("注釈が無いのに LLM を呼んでいる")
		}
	})

	// frame でも customData.etoki が無ければ注釈ではない。
	t.Run("注釈ではない frame", func(t *testing.T) {
		t.Parallel()

		scene := `{"elements":[{"id":"f1","type":"frame","name":"ただの枠"}]}`
		llm := &fakeLLM{responses: []string{validLLMOutput}}
		svc, _ := newInterpretService(t, newBoard(scene), llm)

		_, err := svc.Interpret(t.Context(), "board-1", "f1")
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("Interpret() = %v, want ErrAnnotationNotFound", err)
		}
	})
}

// 図形と矢印だけの囲みがありうるので、テキストが無くても打ち切らない。
// 中身を渡す手段は画像（#9）であって、ここで弾くと後で解禁し直すことになる。
func TestInterpret_InterpretsAnnotationWithoutTexts(t *testing.T) {
	t.Parallel()

	// 名前なしの frame。AnnotationTexts は 1 件も返さない。
	scene := `{"elements":[{"id":"annot-1","type":"frame","customData":{"etoki":{"granularity":""}}}]}`
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1"); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
	// 黙って空にすると、モデルが入力の欠落を疑って作り話を始める。
	if !strings.Contains(llm.requests[0].Text, "(なし)") {
		t.Errorf("テキストが無いことが伝わっていない:\n%s", llm.requests[0].Text)
	}
}

// 上限に 0 以下を渡しても、1 度も呼ばずに失敗しては何が起きたか追えない。
func TestInterpret_IgnoresNonPositiveMaxAttempts(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewInterpretationService(boards, llm, usecase.WithMaxAttempts(0))

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1"); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

// 既定に倒して進めると、開発者が指定したつもりの制約が黙って外れる。
func TestInterpret_RejectsUnknownGranularity(t *testing.T) {
	t.Parallel()

	scene := strings.Replace(interpretScene, `"granularity":""`, `"granularity":"project"`, 1)
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1")
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Interpret() = %v, want ErrInvalidInput", err)
	}
	if len(llm.requests) != 0 {
		t.Error("粒度が不正なのに LLM を呼んでいる")
	}
}

// 粒度指定はプロンプトでも伝えるが、従わない出力は検証で弾いて再送する。
func TestInterpret_EnforcesGranularityOnOutput(t *testing.T) {
	t.Parallel()

	scene := strings.Replace(interpretScene, `"granularity":""`, `"granularity":"issue"`, 1)
	onlyIssues := `{"summary":"返金導線の整理","items":[
		{"localId":"i1","kind":"issue","title":"返金導線の整理","body":"","parentLocalId":null}]}`

	// 1 回目は epic を含む出力（issue 指定では通らない）。
	llm := &fakeLLM{responses: []string{validLLMOutput, onlyIssues}}
	svc, _ := newInterpretService(t, newBoard(scene), llm)

	in, err := svc.Interpret(t.Context(), "board-1", "annot-1")
	if err != nil {
		t.Fatalf("Interpret() = %v", err)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	for _, it := range in.Items {
		if it.Kind == domain.KindEpic {
			t.Error("issue 指定なのに epic が通っている")
		}
	}
}
