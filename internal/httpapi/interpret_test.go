package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// stubLLM は決まった応答かエラーを返す LLMClient。
type stubLLM struct {
	text  string
	err   error
	calls int
}

func (s *stubLLM) Complete(context.Context, port.VisionRequest) (port.VisionResponse, error) {
	s.calls++
	if s.err != nil {
		return port.VisionResponse{}, s.err
	}
	return port.VisionResponse{Text: s.text}, nil
}

const validInterpretation = `{"summary":"決済まわりの課題出し","items":[
	{"localId":"e1","kind":"epic","title":"決済フローの見直し","body":"全体の方針","parentLocalId":null},
	{"localId":"i1","kind":"issue","title":"Stripe SDK の更新","body":"","parentLocalId":"e1"}]}`

// newInterpretRouter は LLM を差したルーターを返す。
// llm が nil なら解釈サービスを組み立てず、503 を返す構成になる。
func newInterpretRouter(t *testing.T, llm port.LLMClient) *gin.Engine {
	t.Helper()

	boards, mappings := newRepos(t)

	seq := 0
	boardSvc := usecase.NewBoardService(boards,
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
		deps.Interpretations = usecase.NewInterpretationService(boards, llm,
			usecase.WithMaxAttempts(2))
	}

	return httpapi.NewRouter(deps)
}

// interpretPath は解釈エンドポイントの URL を組み立てる。
func interpretPath(boardID, annotationID string) string {
	return "/api/boards/" + boardID + "/annotations/" + annotationID + "/interpret"
}

func TestInterpretAnnotation(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	got := decode[map[string]any](t, rec)

	// summary は GitHub に作らないが、確認材料として必ず返す（ADR 0006）。
	if got["summary"] != "決済まわりの課題出し" {
		t.Errorf("summary = %v", got["summary"])
	}

	items, _ := got["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}

	epic, _ := items[0].(map[string]any)
	if epic["kind"] != "epic" || epic["localId"] != "e1" {
		t.Errorf("items[0] = %v", epic)
	}
	if _, ok := epic["parentLocalId"]; ok {
		t.Errorf("epic に parentLocalId が付いている: %v", epic)
	}

	issue, _ := items[1].(map[string]any)
	if issue["parentLocalId"] != "e1" {
		t.Errorf("items[1].parentLocalId = %v, want e1", issue["parentLocalId"])
	}

	if llm.calls != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", llm.calls)
	}
}

// このエンドポイントは読むだけ。作成も実行記録も行わない（中核思想 3）。
func TestInterpretAnnotation_DoesNotRecordRun(t *testing.T) {
	t.Parallel()

	boards, mappings := newRepos(t)

	seq := 0
	boardSvc := usecase.NewBoardService(boards,
		usecase.WithClock(func() time.Time { return fixedTime }),
		usecase.WithIDGenerator(func() string {
			seq++
			return "board-" + string(rune('0'+seq))
		}),
	)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:          boardSvc,
		Annotations:     usecase.NewAnnotationService(boards, mappings),
		Interpretations: usecase.NewInterpretationService(boards, &stubLLM{text: validInterpretation}),
	})

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	if rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), nil); rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}

	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("解釈だけで run が記録されている: %+v", run)
	}

	// 状態も uncreated のまま。
	states := decode[[]map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
	if states[0]["state"] != "uncreated" {
		t.Errorf("state = %v, want uncreated", states[0]["state"])
	}
}

// 404 だと「機能が無い」のか「URL が違う」のか区別できない。
func TestInterpretAnnotation_WithoutLLM(t *testing.T) {
	t.Parallel()

	r := newInterpretRouter(t, nil)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if msg := decode[map[string]string](t, rec)["error"]; msg == "" {
		t.Error("エラーメッセージが空")
	}
}

func TestInterpretAnnotation_NotFound(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		boardID      string
		annotationID string
	}{
		"ボードが無い": {boardID: "no-such-board", annotationID: "annot-1"},
		"注釈が無い":  {boardID: "", annotationID: "no-such-annot"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			llm := &stubLLM{text: validInterpretation}
			r := newInterpretRouter(t, llm)

			id := createBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)

			boardID := tt.boardID
			if boardID == "" {
				boardID = id
			}

			rec := do(t, r, http.MethodPost, interpretPath(boardID, tt.annotationID), nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body)
			}
			if llm.calls != 0 {
				t.Error("対象が無いのに LLM を呼んでいる")
			}
		})
	}
}

// LLM 側の失敗を 500 に丸めると、開発者は自分の設定を疑えない。
func TestInterpretAnnotation_LLMErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		llm  *stubLLM
		want string
	}{
		"呼び出しが失敗する": {
			llm:  &stubLLM{err: errors.New("messages api: 401 authentication_error")},
			want: "401",
		},
		"出力がスキーマを満たさない": {
			llm:  &stubLLM{text: `{"summary":"","items":[]}`},
			want: "schema",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newInterpretRouter(t, tt.llm)

			id := createBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)

			rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), nil)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502 (%s)", rec.Code, rec.Body)
			}

			msg := decode[map[string]string](t, rec)["error"]
			if msg == "" {
				t.Fatal("エラーメッセージが空")
			}
			if !strings.Contains(msg, tt.want) {
				t.Errorf("error = %q, want to contain %q", msg, tt.want)
			}
		})
	}
}

// 粒度が不正なシーンはリクエストの問題として 400 で返す。
func TestInterpretAnnotation_InvalidGranularity(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	scene := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"project"}}}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		map[string]string{"scene": scene}); rec.Code != http.StatusNoContent {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if llm.calls != 0 {
		t.Error("粒度が不正なのに LLM を呼んでいる")
	}
}
