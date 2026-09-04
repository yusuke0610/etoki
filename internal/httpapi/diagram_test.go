package httpapi_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// validDiagram は todo が求める記法の最小の出力。
const validDiagram = "flowchart TD\n  A[調べる] --> B[書く]"

// newDiagramRouter は LLM を差したルーターを返す。
// llm が nil なら生成サービスを組み立てず、503 を返す構成になる。
func newDiagramRouter(t *testing.T, llm port.LLMClient) *gin.Engine {
	t.Helper()

	boards, mappings := newRepos(t)

	seq := 0
	boardSvc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks(),
		usecase.WithClock(func() time.Time { return fixedTime }),
		usecase.WithIDGenerator(func() string {
			seq++
			return "board-" + string(rune('0'+seq))
		}),
	)

	deps := httpapi.Deps{
		Boards:      boardSvc,
		Annotations: usecase.NewAnnotationService(boards, mappings),
	}
	if llm != nil {
		deps.Diagrams = usecase.NewDiagramService(boards, llm, newLimiter(),
			usecase.WithDiagramMaxAttempts(2))
	}

	return httpapi.NewRouter(deps)
}

func diagramPath(boardID string) string {
	return "/api/boards/" + boardID + "/diagram-draft"
}

// diagramBody は最小のリクエストボディ。**契約の型で書く。** 手書きの map に
// すると、契約が変わってもテストだけ古い形のまま通る（ADR 0011）。
func diagramBody(prompt string) apitypes.GenerateDiagramRequest {
	return apitypes.GenerateDiagramRequest{
		Kind:   apitypes.DiagramKindTodo,
		Prompt: prompt,
	}
}

func TestGenerateDiagramDraft(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validDiagram}
	r := newDiagramRouter(t, llm)
	id := createBoard(t, r, "設計会")

	rec := do(t, r, http.MethodPost, diagramPath(id), diagramBody("リリースまでの段取り"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	got := decode[apitypes.DiagramDraft](t, rec)
	if got.Mermaid != validDiagram {
		t.Errorf("mermaid = %q, want %q", got.Mermaid, validDiagram)
	}
	if got.Kind != apitypes.DiagramKindTodo {
		t.Errorf("kind = %q, want todo", got.Kind)
	}
	if want := usecase.MaxDiagramTurns - 1; got.TurnsRemaining != want {
		t.Errorf("turnsRemaining = %d, want %d", got.TurnsRemaining, want)
	}
}

// **保存済みシーンを読まないので、囲みが 1 つも無いボードでも呼べる**
// （ADR 0041）。ここが切れると、解釈と同じ「保存してから」の制約が要ることに
// なり、未保存でも使えるという設計そのものが消える。
func TestGenerateDiagramDraft_WorksOnAnEmptyBoard(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validDiagram}
	r := newDiagramRouter(t, llm)
	// シーンを一度も保存していないボード。
	id := createBoard(t, r, "まだ何も描いていない")

	rec := do(t, r, http.MethodPost, diagramPath(id), diagramBody("段取り"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}

// 会話は毎回まるごと送られてくる。サーバーは持たない（ADR 0041）ので、
// 送られたものが LLM への入力に届いていることを見る。
func TestGenerateDiagramDraft_PassesHistoryToLLM(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validDiagram}
	r := newDiagramRouter(t, llm)
	id := createBoard(t, r, "設計会")

	body := diagramBody("支払いの分岐も足して")
	body.History = &[]apitypes.DiagramTurn{
		{Prompt: "注文の流れ", Mermaid: "flowchart TD\n  受注 --> 出荷"},
	}

	rec := do(t, r, http.MethodPost, diagramPath(id), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	for _, want := range []string{"注文の流れ", "受注 --> 出荷", "支払いの分岐も足して"} {
		if !strings.Contains(llm.last.Text, want) {
			t.Errorf("LLM への入力に %q が無い:\n%s", want, llm.last.Text)
		}
	}
}

func TestGenerateDiagramDraft_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		llm    port.LLMClient
		body   any
		status int
		code   apitypes.ErrorCode
	}{
		{
			// 設定の不足であって URL の誤りではないので 404 にしない。
			name:   "LLM が未設定",
			llm:    nil,
			body:   diagramBody("段取り"),
			status: http.StatusServiceUnavailable,
			code:   apitypes.ErrorCodeLlmNotConfigured,
		},
		{
			name:   "種類の指定なし",
			llm:    &stubLLM{text: validDiagram},
			body:   apitypes.GenerateDiagramRequest{Prompt: "段取り"},
			status: http.StatusBadRequest,
			code:   apitypes.ErrorCodeInvalidInput,
		},
		{
			name:   "プロンプトが空白だけ",
			llm:    &stubLLM{text: validDiagram},
			body:   diagramBody("   "),
			status: http.StatusBadRequest,
			code:   apitypes.ErrorCodeInvalidInput,
		},
		{
			// 接続やモデル側の失敗。500 に丸めると、開発者は自分の設定を疑えない。
			name:   "LLM の呼び出しが失敗",
			llm:    &stubLLM{err: errors.New("connection refused")},
			body:   diagramBody("段取り"),
			status: http.StatusBadGateway,
			code:   apitypes.ErrorCodeLlmUnavailable,
		},
		{
			// 接続はできたが図が返らなかった。llm_unavailable と分けるのは、
			// 打ち手が「設定を見る」ではなく「言い方を変える」になるため。
			name:   "図が返らなかった",
			llm:    &stubLLM{text: "もう少し詳しく教えてください。"},
			body:   diagramBody("段取り"),
			status: http.StatusBadGateway,
			code:   apitypes.ErrorCodeDiagramFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newDiagramRouter(t, tt.llm)
			id := createBoard(t, r, "設計会")

			rec := do(t, r, http.MethodPost, diagramPath(id), tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tt.status, rec.Body)
			}
			if code := decode[apitypes.ErrorResponse](t, rec).Code; code != tt.code {
				t.Errorf("code = %q, want %q", code, tt.code)
			}
		})
	}
}

// 積み上がりすぎた会話は 413。**400 に畳まない。** 送られた内容は正しく、
// 打ち手が「会話をやり直す」になる（ADR 0041、ADR 0038 と同じ切り方）。
func TestGenerateDiagramDraft_ChatTooLong(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validDiagram}
	r := newDiagramRouter(t, llm)
	id := createBoard(t, r, "設計会")

	rec := do(t, r, http.MethodPost, diagramPath(id),
		diagramBody(strings.Repeat("あ", usecase.MaxDiagramChatBytes)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeDiagramChatTooLong {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeDiagramChatTooLong)
	}
	if llm.calls != 0 {
		t.Error("上限を超えた会話で LLM を呼んでいる")
	}
}

// ボディの読み込みにも歯止めがある。**歯止めに当たった側も 413 で返す。**
// 同じ「積み上がりすぎた会話」が、ボディの大きさしだいで 400 と 413 に割れると、
// 画面が同じ原因を 2 通りに案内することになる（ADR 0038 と同じ切り方）。
//
// このテストが落ちるのは、歯止めに当たったボディを 400 に写す実装にしたとき。
func TestGenerateDiagramDraft_RejectsOversizedBody(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validDiagram}
	r := newDiagramRouter(t, llm)
	id := createBoard(t, r, "設計会")

	// 歯止めは上限の 6 倍に取ってある（エスケープで膨らむため）。7 倍を送れば、
	// エスケープの要らない文字で埋めても必ず当たる。
	rec := do(t, r, http.MethodPost, diagramPath(id),
		diagramBody(strings.Repeat("a", usecase.MaxDiagramChatBytes*7)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeDiagramChatTooLong {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeDiagramChatTooLong)
	}
	if llm.calls != 0 {
		t.Error("歯止めに当たったボディで LLM を呼んでいる")
	}
}

// 読み込みの歯止めは、正本の上限をエスケープの最悪値で割り増して置く。
// **上限ちょうどの会話が歯止め側で落ちてはならない**（ADR 0038 と同じ形）。
// 落ちると、減らす必要のないものを減らせと言うことになる。
func TestGenerateDiagramDraft_BodyGuardDoesNotCutBeforeTheLimit(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validDiagram}
	r := newDiagramRouter(t, llm)
	id := createBoard(t, r, "設計会")

	// 上限ちょうど。日本語 1 文字は UTF-8 で 3 バイト。
	prompt := strings.Repeat("あ", usecase.MaxDiagramChatBytes/3)

	rec := do(t, r, http.MethodPost, diagramPath(id), diagramBody(prompt))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
}
