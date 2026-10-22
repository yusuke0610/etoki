package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
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

// diagramJobPath は図のドラフト生成のジョブの URL を組み立てる。空の jobID は
// ボードの一覧。
func diagramJobPath(boardID, jobID string) string {
	p := "/api/boards/" + boardID + "/diagram-jobs"
	if jobID != "" {
		p += "/" + jobID
	}
	return p
}

// startDiagram は生成を始めて、202 で返ったジョブを返す。
func startDiagram(t *testing.T, r *gin.Engine, boardID string, body any) apitypes.DiagramJob {
	t.Helper()

	rec := do(t, r, http.MethodPost, diagramPath(boardID), body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body)
	}
	return decode[apitypes.DiagramJob](t, rec)
}

// waitDiagramJob はジョブが走り終わるのを待って返す。
func waitDiagramJob(t *testing.T, r *gin.Engine, boardID, jobID string) apitypes.DiagramJob {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := do(t, r, http.MethodGet, diagramJobPath(boardID, jobID), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET job: status = %d, want 200 (%s)", rec.Code, rec.Body)
		}
		job := decode[apitypes.DiagramJob](t, rec)
		if job.Status != apitypes.DiagramJobStatusRunning {
			return job
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("ジョブが終わらない")
	return apitypes.DiagramJob{}
}

// generateAndWait は生成を始めて、走り終わったジョブを返す。
func generateAndWait(t *testing.T, r *gin.Engine, boardID string, body any) apitypes.DiagramJob {
	t.Helper()

	return waitDiagramJob(t, r, boardID, startDiagram(t, r, boardID, body).ID)
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

	job := generateAndWait(t, r, id, diagramBody("リリースまでの段取り"))
	if job.Status != apitypes.DiagramJobStatusSucceeded || job.Result == nil {
		t.Fatalf("job = %+v, want succeeded with result", job)
	}

	got := *job.Result
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

// 開き直した画面は、ジョブの入力から会話を戻す（ADR 0091）。送ったものが
// そのまま返らないと、どの指示への答えかが分からない。**内部の印も往復させる。**
// 落とすと、変換の投げ直しが利用者の指示として並ぶ。
func TestGenerateDiagramDraft_EchoesRequest(t *testing.T) {
	t.Parallel()

	r := newDiagramRouter(t, &stubLLM{text: validDiagram})
	id := createBoard(t, r, "設計会")

	body := diagramBody("直前の図は変換できませんでした")
	body.Internal = true
	body.History = &[]apitypes.DiagramTurn{
		{Prompt: "注文の流れ", Mermaid: validDiagram},
		{Prompt: "投げ直し", Mermaid: validDiagram, Internal: true},
	}

	job := generateAndWait(t, r, id, body)
	if !reflect.DeepEqual(job.Request, body) {
		t.Errorf("request = %+v, want %+v", job.Request, body)
	}

	// 1 往復目には往復が無い。null ではなく空配列で返す。
	first := generateAndWait(t, r, id, diagramBody("段取り"))
	if first.Request.History == nil || len(*first.Request.History) != 0 {
		t.Errorf("history = %v, want 空配列", first.Request.History)
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

	if job := generateAndWait(t, r, id, diagramBody("段取り")); job.Status != apitypes.DiagramJobStatusSucceeded {
		t.Fatalf("job = %+v, want succeeded", job)
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

	if job := generateAndWait(t, r, id, body); job.Status != apitypes.DiagramJobStatusSucceeded {
		t.Fatalf("job = %+v, want succeeded", job)
	}

	for _, want := range []string{"注文の流れ", "受注 --> 出荷", "支払いの分岐も足して"} {
		if !strings.Contains(llm.last.Text, want) {
			t.Errorf("LLM への入力に %q が無い:\n%s", want, llm.last.Text)
		}
	}
}

// 入力だけで決まる失敗は、始める時点で返す。ジョブにしない（ADR 0044）。
func TestGenerateDiagramDraft_RejectsBeforeStarting(t *testing.T) {
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

// LLM を叩いてから分かる失敗は、ジョブの失敗として返す。code は応答の失敗と
// 同じ表で写す（errors.go）。写し替えを 2 つ持つと、ジョブの失敗だけ画面の
// 対応表から漏れる。
func TestGenerateDiagramDraft_JobFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		llm  port.LLMClient
		code apitypes.ErrorCode
	}{
		{
			// 接続やモデル側の失敗。internal に丸めると、開発者は自分の設定を疑えない。
			name: "LLM の呼び出しが失敗",
			llm:  &stubLLM{err: errors.New("connection refused")},
			code: apitypes.ErrorCodeLlmUnavailable,
		},
		{
			// 接続はできたが図が返らなかった。llm_unavailable と分けるのは、
			// 打ち手が「設定を見る」ではなく「言い方を変える」になるため。
			name: "図が返らなかった",
			llm:  &stubLLM{text: "もう少し詳しく教えてください。"},
			code: apitypes.ErrorCodeDiagramFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newDiagramRouter(t, tt.llm)
			id := createBoard(t, r, "設計会")

			job := generateAndWait(t, r, id, diagramBody("段取り"))
			if job.Status != apitypes.DiagramJobStatusFailed || job.Error == nil {
				t.Fatalf("job = %+v, want failed with error", job)
			}
			if job.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", job.Error.Code, tt.code)
			}
			if job.Result != nil {
				t.Error("失敗したジョブに result が入っている")
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
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body)
	}
}

// 一覧は入力を運び、結果は運ばない。1 件の取得は読んでも消えない。捨てると両方から
// 消える。
func TestDiagramJobs_ListGetDiscard(t *testing.T) {
	t.Parallel()

	r := newDiagramRouter(t, &stubLLM{text: validDiagram})
	id := createBoard(t, r, "設計会")

	done := generateAndWait(t, r, id, diagramBody("段取り"))

	list := decode[[]apitypes.DiagramJob](t, do(t, r, http.MethodGet, diagramJobPath(id, ""), nil))
	if len(list) != 1 || list[0].Status != apitypes.DiagramJobStatusSucceeded || list[0].Result != nil {
		t.Fatalf("list = %+v, want 成功・結果なし", list)
	}
	if list[0].Request.Prompt != "段取り" {
		t.Errorf("list の request.prompt = %q, want 段取り", list[0].Request.Prompt)
	}

	for range 2 {
		got := decode[apitypes.DiagramJob](t, do(t, r, http.MethodGet, diagramJobPath(id, done.ID), nil))
		if got.Result == nil {
			t.Fatal("読み直したら結果が無い")
		}
	}

	if rec := do(t, r, http.MethodDelete, diagramJobPath(id, done.ID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if rec := do(t, r, http.MethodGet, diagramJobPath(id, done.ID), nil); rec.Code != http.StatusNotFound {
		t.Errorf("捨てた後の GET status = %d, want 404", rec.Code)
	}
	list = decode[[]apitypes.DiagramJob](t, do(t, r, http.MethodGet, diagramJobPath(id, ""), nil))
	if len(list) != 0 {
		t.Errorf("捨てた後の list = %+v, want 空", list)
	}
}

// 始めるリクエストが終わって（ctx が切れて）も、生成は止まらない。#329 の本題。
func TestDiagramJobs_SurvivesRequestEnd(t *testing.T) {
	t.Parallel()

	llm := newGateLLM()
	llm.text = validDiagram
	r := newDiagramRouter(t, llm)
	id := createBoard(t, r, "設計会")

	ctx, cancel := context.WithCancel(t.Context())
	rec := doWithContext(t, ctx, r, http.MethodPost, diagramPath(id), diagramBody("段取り"))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body)
	}
	job := decode[apitypes.DiagramJob](t, rec)
	<-llm.started
	cancel()

	select {
	case <-llm.ended:
		t.Fatal("リクエストが終わったら生成が止まった")
	case <-time.After(50 * time.Millisecond):
	}
	close(llm.release)

	if got := waitDiagramJob(t, r, id, job.ID); got.Status != apitypes.DiagramJobStatusSucceeded {
		t.Errorf("job = %+v, want succeeded", got)
	}
}

// 知らないジョブは 404。ボードが無いときも 404。
func TestDiagramJobs_NotFound(t *testing.T) {
	t.Parallel()

	r := newDiagramRouter(t, &stubLLM{text: validDiagram})
	id := createBoard(t, r, "設計会")

	for name, tt := range map[string]struct{ method, path string }{
		"GET 知らないジョブ":    {http.MethodGet, diagramJobPath(id, "nope")},
		"DELETE 知らないジョブ": {http.MethodDelete, diagramJobPath(id, "nope")},
		"一覧 知らないボード":     {http.MethodGet, diagramJobPath("nope", "")},
	} {
		if rec := do(t, r, tt.method, tt.path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (%s)", name, rec.Code, rec.Body)
		}
	}
}

// LLM が無いときは 3 つとも 503。
func TestDiagramJobs_WithoutLLM(t *testing.T) {
	t.Parallel()

	r := newDiagramRouter(t, nil)
	id := createBoard(t, r, "設計会")

	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, diagramJobPath(id, "")},
		{http.MethodGet, diagramJobPath(id, "x")},
		{http.MethodDelete, diagramJobPath(id, "x")},
	} {
		rec := do(t, r, tt.method, tt.path, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", tt.method, tt.path, rec.Code)
		}
	}
}
