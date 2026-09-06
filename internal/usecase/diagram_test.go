package usecase_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// validMermaid は todo / mindmap / architecture が求める記法の最小の出力。
const validMermaid = "flowchart TD\n  A[調べる] --> B[書く]"

func newDiagramService(
	t *testing.T, llm *fakeLLM, opts ...usecase.DiagramServiceOption,
) (*usecase.DiagramService, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	return usecase.NewDiagramService(boards, llm, newLimiter(), opts...), boards
}

// req は最小の入力。種類とプロンプトだけで、保存済みシーンは読まない。
func req(prompt string) usecase.DiagramRequest {
	return usecase.DiagramRequest{Kind: domain.DiagramKindTodo, Prompt: prompt}
}

func TestGenerateDiagram_Succeeds(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validMermaid}}
	svc, boards := newDiagramService(t, llm)

	got, err := svc.Generate(t.Context(), "board-1", req("リリースまでの段取り"))
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}

	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
	if got.Mermaid != validMermaid {
		t.Errorf("Mermaid = %q, want %q", got.Mermaid, validMermaid)
	}
	if got.Kind != domain.DiagramKindTodo {
		t.Errorf("Kind = %q, want todo", got.Kind)
	}
	// 1 往復使ったので、上限から 1 減る。**残りを返さないと、上限に当たって
	// から初めて知ることになる。**
	if want := usecase.MaxDiagramTurns - 1; got.TurnsRemaining != want {
		t.Errorf("TurnsRemaining = %d, want %d", got.TurnsRemaining, want)
	}

	// サーバーの状態を一切変えない（ADR 0041）。ここが切れると、チャットが
	// 「保存しないので取り消せない副作用が出ない」という前提を失う。
	if boards.writes != 0 {
		t.Errorf("生成なのに書き込んでいる: writes = %d", boards.writes)
	}
}

// 入力はプロンプトだけ。保存済みシーンもキャンバスも読まないので、画像は
// 空のまま（ADR 0041）。ここが切れると、解釈と同じ「保存してから」の制約が
// 要ることになる。
func TestGenerateDiagram_SendsNoImages(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validMermaid}}
	svc, _ := newDiagramService(t, llm)

	if _, err := svc.Generate(t.Context(), "board-1", req("段取り")); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if n := len(llm.requests[0].Images); n != 0 {
		t.Errorf("画像を %d 枚渡している。プロンプトだけのはず", n)
	}
}

// 「mermaid だけを返せ」と指示してもフェンスは付いてくる。指摘して往復する
// より、機械的に落とす。
func TestGenerateDiagram_StripsCodeFence(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{"```mermaid\n" + validMermaid + "\n```"}}
	svc, _ := newDiagramService(t, llm)

	got, err := svc.Generate(t.Context(), "board-1", req("段取り"))
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if got.Mermaid != validMermaid {
		t.Errorf("Mermaid = %q, want %q", got.Mermaid, validMermaid)
	}
	if len(llm.requests) != 1 {
		t.Errorf("フェンスを落とせば足りるのに再送している: %d 回", len(llm.requests))
	}
}

// 頼んだ記法と違うものは指摘して 1 度だけ投げ直す。**直せる失敗だから。**
func TestGenerateDiagram_RetriesOnWrongNotation(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{"sequenceDiagram\n  A->>B: やあ", validMermaid}}
	svc, _ := newDiagramService(t, llm)

	got, err := svc.Generate(t.Context(), "board-1", req("段取り"))
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("LLM 呼び出し回数 = %d, want 2", len(llm.requests))
	}
	if got.Mermaid != validMermaid {
		t.Errorf("Mermaid = %q", got.Mermaid)
	}

	// 再送は自己完結している必要がある。元の指示と、直前の出力と、指摘の
	// 3 つが揃っていないと、モデルは何を直すのか決められない。
	retry := llm.requests[1].Text
	for _, want := range []string{"段取り", "sequenceDiagram", "flowchart"} {
		if !strings.Contains(retry, want) {
			t.Errorf("再送メッセージに %q が無い:\n%s", want, retry)
		}
	}
}

func TestGenerateDiagram_GivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{"図は描けません。もう少し詳しく教えてください。"}}
	svc, _ := newDiagramService(t, llm)

	_, err := svc.Generate(t.Context(), "board-1", req("段取り"))
	if !errors.Is(err, usecase.ErrDiagramFailed) {
		t.Fatalf("Generate() = %v, want ErrDiagramFailed", err)
	}
	// 直らない出力を繰り返しても課金が増えるだけ。
	if len(llm.requests) != 2 {
		t.Errorf("LLM 呼び出し回数 = %d, want 2（上限）", len(llm.requests))
	}
}

// 接続やモデル側の失敗は指摘で直る問題ではないので、投げ直さない。
func TestGenerateDiagram_DoesNotRetryTransportErrors(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{err: errors.New("connection refused")}
	svc, _ := newDiagramService(t, llm)

	_, err := svc.Generate(t.Context(), "board-1", req("段取り"))
	if !errors.Is(err, usecase.ErrLLMUnavailable) {
		t.Fatalf("Generate() = %v, want ErrLLMUnavailable", err)
	}
	if errors.Is(err, usecase.ErrDiagramFailed) {
		t.Error("接続の失敗を「図が返らなかった」に畳んでいる")
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

// 弾くと決めた入力で LLM を呼ぶと、結果は捨てるのに課金だけ発生する。
func TestGenerateDiagram_RejectsBadInputWithoutCallingLLM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  usecase.DiagramRequest
	}{
		{
			// 既定に倒さない。何の図かは入力そのもの（中核思想 3）。
			name: "種類の指定なし",
			req:  usecase.DiagramRequest{Prompt: "段取り"},
		},
		{
			name: "知らない種類",
			req:  usecase.DiagramRequest{Kind: "gantt", Prompt: "段取り"},
		},
		{
			name: "プロンプトが空",
			req:  usecase.DiagramRequest{Kind: domain.DiagramKindTodo},
		},
		{
			// 正規化して空になるものは「空」。改行だけ渡されても、モデルは
			// 何を描けばよいか決められない。
			name: "プロンプトが空白だけ",
			req:  usecase.DiagramRequest{Kind: domain.DiagramKindTodo, Prompt: " \n\t "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			llm := &fakeLLM{responses: []string{validMermaid}}
			svc, _ := newDiagramService(t, llm)

			_, err := svc.Generate(t.Context(), "board-1", tt.req)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("Generate() = %v, want ErrInvalidInput", err)
			}
			if len(llm.requests) != 0 {
				t.Error("弾いた入力で LLM を呼んでいる")
			}
		})
	}
}

// 会話が伸びるとトークンが積み上がる。**黙って古いやりとりを捨てない。**
func TestGenerateDiagram_RejectsTooLongChat(t *testing.T) {
	t.Parallel()

	t.Run("往復の回数", func(t *testing.T) {
		t.Parallel()

		// 上限 3 に対して、過去 3 往復ぶん。今回を足すと 4 で超える。
		llm := &fakeLLM{responses: []string{validMermaid}}
		svc, _ := newDiagramService(t, llm, usecase.WithDiagramMaxTurns(3))

		r := req("もう一度")
		r.History = []usecase.DiagramTurn{
			{Prompt: "1", Mermaid: validMermaid},
			{Prompt: "2", Mermaid: validMermaid},
			{Prompt: "3", Mermaid: validMermaid},
		}

		_, err := svc.Generate(t.Context(), "board-1", r)
		if !errors.Is(err, usecase.ErrDiagramChatTooLong) {
			t.Fatalf("Generate() = %v, want ErrDiagramChatTooLong", err)
		}
		// 入力の誤りではない。畳むと、画面が「会話をやり直す」と案内できない。
		if errors.Is(err, usecase.ErrInvalidInput) {
			t.Error("上限超過を invalid_input に畳んでいる")
		}
		if len(llm.requests) != 0 {
			t.Error("上限を超えた会話で LLM を呼んでいる")
		}
	})

	// 回数だけでは足りない。1 通に長文を貼れば往復 1 回で同じだけ積み上がる。
	t.Run("会話の長さ", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validMermaid}}
		svc, _ := newDiagramService(t, llm)

		_, err := svc.Generate(t.Context(), "board-1",
			req(strings.Repeat("あ", usecase.MaxDiagramChatBytes)))
		if !errors.Is(err, usecase.ErrDiagramChatTooLong) {
			t.Fatalf("Generate() = %v, want ErrDiagramChatTooLong", err)
		}
		if len(llm.requests) != 0 {
			t.Error("上限を超えた会話で LLM を呼んでいる")
		}
	})

	// 上限ちょうどは通す。境界の外側だけを見ると、1 つずれた実装でも緑になる。
	t.Run("上限ちょうどは通す", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{validMermaid}}
		svc, _ := newDiagramService(t, llm, usecase.WithDiagramMaxTurns(3))

		r := req("3 回目")
		r.History = []usecase.DiagramTurn{
			{Prompt: "1", Mermaid: validMermaid},
			{Prompt: "2", Mermaid: validMermaid},
		}

		got, err := svc.Generate(t.Context(), "board-1", r)
		if err != nil {
			t.Fatalf("Generate() = %v", err)
		}
		if got.TurnsRemaining != 0 {
			t.Errorf("TurnsRemaining = %d, want 0", got.TurnsRemaining)
		}
	})
}

// 会話は毎回まるごと 1 通に組み立て直す（ADR 0005 / 0041）。port は拡張しない。
func TestGenerateDiagram_RebuildsConversationIntoOneMessage(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validMermaid}}
	svc, _ := newDiagramService(t, llm)

	r := req("支払いの分岐も足して")
	r.History = []usecase.DiagramTurn{
		{Prompt: "注文の流れ", Mermaid: "flowchart TD\n  受注 --> 出荷"},
	}

	if _, err := svc.Generate(t.Context(), "board-1", r); err != nil {
		t.Fatalf("Generate() = %v", err)
	}

	sent := llm.requests[0].Text
	for _, want := range []string{
		// 過去の指示と、そのとき返した図。**両方が要る。** 図だけだと何を
		// 頼まれて描いたのかが消え、指示だけだと直す土台が消える。
		"注文の流れ", "受注 --> 出荷",
		"支払いの分岐も足して",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("組み立てたメッセージに %q が無い:\n%s", want, sent)
		}
	}
}

// bot は絵の話しかしない（#58）。epic / issue を出せると分かった時点で、
// 会話の流れで構造化できてしまい、開発者の明示的なトリガーから外れる。
func TestGenerateDiagram_DoesNotAskForIssues(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validMermaid}}
	svc, _ := newDiagramService(t, llm)

	if _, err := svc.Generate(t.Context(), "board-1", req("段取り")); err != nil {
		t.Fatalf("Generate() = %v", err)
	}

	system := llm.requests[0].System
	if !strings.Contains(system, "epic") || !strings.Contains(system, "出さないでください") {
		t.Errorf("epic / issue を出させない指示が無い:\n%s", system)
	}
}

// 種類ごとに、どの mermaid 記法で書かせるか。**種類を足したらここにも足す。**
//
// 指示と検査（頭の語）が同じ表から出ていることを外側から固定する。片方だけ
// 直すと、頼んだとおりに書いた出力を弾くことになる。
func TestGenerateDiagram_NotationPerKind(t *testing.T) {
	t.Parallel()

	accepted := map[domain.DiagramKind]string{
		domain.DiagramKindTodo:         "flowchart TD",
		domain.DiagramKindMindmap:      "flowchart LR",
		domain.DiagramKindSequence:     "sequenceDiagram",
		domain.DiagramKindER:           "erDiagram",
		domain.DiagramKindArchitecture: "flowchart TD",
	}

	for _, kind := range domain.DiagramKinds() {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			head, ok := accepted[kind]
			if !ok {
				t.Fatalf("種類 %q に対応する記法がこの表に無い", kind)
			}

			llm := &fakeLLM{responses: []string{head + "\n  A --> B"}}
			svc, _ := newDiagramService(t, llm)

			got, err := svc.Generate(t.Context(), "board-1",
				usecase.DiagramRequest{Kind: kind, Prompt: "描いて"})
			if err != nil {
				t.Fatalf("%s の記法を弾いている: %v", head, err)
			}
			if !strings.HasPrefix(got.Mermaid, head) {
				t.Errorf("Mermaid = %q", got.Mermaid)
			}
			// 記法を名指しした指示が渡っていること。表に行が無いと、指示が
			// 空のまま頼むことになり、返ってくるものが種類によらず同じになる。
			notation, _, _ := strings.Cut(head, " ")
			if !strings.Contains(llm.requests[0].Text, notation) {
				t.Errorf("%s を書かせる指示が無い:\n%s", kind, llm.requests[0].Text)
			}
		})
	}
}

// mermaid には mindmap と architecture-beta の記法があるが、変換器が図形に
// 分解せず画像 1 枚にして返すため、そのまま頼むと**必ず置けないものが返る**
// （ADR 0040 / 0041）。記法を選ぶ基準は「mermaid で書けるか」ではなく
// 「Excalidraw の要素として置けるか」。
func TestGenerateDiagram_AvoidsUnplaceableNotations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind    domain.DiagramKind
		avoided string
	}{
		{domain.DiagramKindMindmap, "mindmap"},
		{domain.DiagramKindArchitecture, "architecture-beta"},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()

			llm := &fakeLLM{responses: []string{validMermaid}}
			svc, _ := newDiagramService(t, llm)

			if _, err := svc.Generate(t.Context(), "board-1",
				usecase.DiagramRequest{Kind: tt.kind, Prompt: "描いて"}); err != nil {
				t.Fatalf("Generate() = %v", err)
			}

			sent := llm.requests[0].Text
			if !strings.Contains(sent, "flowchart") {
				t.Errorf("flowchart で書かせていない:\n%s", sent)
			}
			// 使わせないことを名指しする。書かないと、モデルは種類の名前から
			// その記法を選ぶ。
			if !strings.Contains(sent, tt.avoided+" 記法は使わず") {
				t.Errorf("%s を使わせない指示が無い:\n%s", tt.avoided, sent)
			}
		})
	}

	// 変換器が返した mindmap をこちらが受け取ってしまわないこと。指示を
	// 無視した出力は投げ直しの対象になる。
	t.Run("mindmap 記法で返ってきたら受け取らない", func(t *testing.T) {
		t.Parallel()

		llm := &fakeLLM{responses: []string{"mindmap\n  root((etoki))\n    a"}}
		svc, _ := newDiagramService(t, llm)

		_, err := svc.Generate(t.Context(), "board-1",
			usecase.DiagramRequest{Kind: domain.DiagramKindMindmap, Prompt: "描いて"})
		if !errors.Is(err, usecase.ErrDiagramFailed) {
			t.Fatalf("Generate() = %v, want ErrDiagramFailed", err)
		}
	})
}

// 生成は課金を伴う外部呼び出しなので、呼んだ実績を残す（ADR 0031）。
func TestGenerateDiagram_LogsUsage(t *testing.T) {
	t.Parallel()

	buf, logger := captureLogs()
	llm := &fakeLLM{
		// 1 回目は記法違いで投げ直す。**失敗したぶんも課金されている。**
		responses: []string{"sequenceDiagram\n  A->>B: x", validMermaid},
		usages: []port.Usage{
			{InputTokens: 800, OutputTokens: 90},
			{InputTokens: 1000, OutputTokens: 120},
		},
	}
	svc, _ := newDiagramService(t, llm, usecase.WithDiagramLogger(logger))

	if _, err := svc.Generate(t.Context(), "board-1", req("段取り")); err != nil {
		t.Fatalf("Generate() = %v", err)
	}

	out := buf.String()
	// 1 回ぶんではなく生成 1 回ぶんを積む。1 回ぶんだけ見ても払った量にならない。
	for _, want := range []string{
		"attempts=2", "inputTokens=1800", "outputTokens=210", "ok=true",
		"boardId=board-1", "diagramKind=todo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("実績ログに %s が無い: %q", want, out)
		}
	}
}

// 直らなかったぶんも課金されている。成功したときだけ残すと、実績が実際より
// 小さく見える（ADR 0031）。
func TestGenerateDiagram_LogsUsageOnFailure(t *testing.T) {
	t.Parallel()

	buf, logger := captureLogs()
	llm := &fakeLLM{
		responses: []string{"図は描けません"},
		usages:    []port.Usage{{InputTokens: 700, OutputTokens: 40}},
	}
	svc, _ := newDiagramService(t, llm, usecase.WithDiagramLogger(logger))

	if _, err := svc.Generate(t.Context(), "board-1", req("段取り")); err == nil {
		t.Fatal("Generate() が成功している")
	}

	out := buf.String()
	if !strings.Contains(out, "ok=false") {
		t.Errorf("失敗が実績ログに残っていない: %q", out)
	}
	if !strings.Contains(out, "attempts=2") {
		t.Errorf("失敗した呼び出しが数えられていない: %q", out)
	}
}

// ボードが無ければ LLM を呼ばない。非メンバーも同じ（ADR 0016 / 0017）。
func TestGenerateDiagram_BoardNotFound(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validMermaid}}
	svc, _ := newDiagramService(t, llm)

	_, err := svc.Generate(t.Context(), "no-such-board", req("段取り"))
	if !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Fatalf("Generate() = %v, want ErrBoardNotFound", err)
	}
	if len(llm.requests) != 0 {
		t.Error("ボードが無いのに LLM を呼んでいる")
	}
}

func TestGenerateDiagram_IgnoresNonPositiveOptions(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validMermaid}}
	svc, _ := newDiagramService(t, llm,
		usecase.WithDiagramMaxAttempts(0), usecase.WithDiagramMaxTurns(-1),
		usecase.WithDiagramLogger(nil))

	got, err := svc.Generate(t.Context(), "board-1", req("段取り"))
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	// 既定に戻っていること。0 を通すと 1 度も呼ばずに「図が返らなかった」と
	// 報告することになり、何が起きたのか追えない。
	if want := usecase.MaxDiagramTurns - 1; got.TurnsRemaining != want {
		t.Errorf("TurnsRemaining = %d, want %d", got.TurnsRemaining, want)
	}
}
