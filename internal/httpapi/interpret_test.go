package httpapi_test

import (
	"context"
	"encoding/base64"
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
	// last は最後に受け取った入力。画像が届いたかを確かめるために持つ。
	last port.VisionRequest
}

func (s *stubLLM) Complete(_ context.Context, req port.VisionRequest) (port.VisionResponse, error) {
	s.calls++
	s.last = req
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
		deps.Interpretations = usecase.NewInterpretationService(boards, mappings, llm,
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
	if got["contentHash"] != currentHash(t, r, id) {
		t.Errorf("contentHash = %v, want %s", got["contentHash"], currentHash(t, r, id))
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

// imageBody は画像 1 枚を載せた解釈のリクエストボディを組み立てる。
//
// data は base64 の文字列として送る。契約が format: byte だからで、Go の
// encoding/json はこれを []byte に復号する。
func imageBody(mediaType string, size int) map[string]any {
	return map[string]any{
		"image": map[string]any{
			"mediaType": mediaType,
			"data":      base64.StdEncoding.EncodeToString(make([]byte, size)),
		},
	}
}

// 画像はテキストに現れない構造を渡すためのもの（ADR 0018）。
func TestInterpretAnnotation_ForwardsImage(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), imageBody("image/png", 32))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	if len(llm.last.Images) != 1 {
		t.Fatalf("len(Images) = %d, want 1", len(llm.last.Images))
	}
	if llm.last.Images[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q", llm.last.Images[0].MediaType)
	}
	// base64 のまま渡すと、アダプタが二重にエンコードする。
	if len(llm.last.Images[0].Data) != 32 {
		t.Errorf("len(Data) = %d, want 32", len(llm.last.Images[0].Data))
	}
}

// 画像は任意。省略した経路をこれまでどおり動かす（ADR 0018）。
func TestInterpretAnnotation_WithoutImage(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		"ボードごと省略":   nil,
		"image を省略": map[string]any{},
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			llm := &stubLLM{text: validInterpretation}
			r := newInterpretRouter(t, llm)

			id := createBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)

			rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
			}
			if len(llm.last.Images) != 0 {
				t.Errorf("len(Images) = %d, want 0", len(llm.last.Images))
			}
		})
	}
}

// 上限を超えた画像は縮小せずに弾く（ADR 0018）。
func TestInterpretAnnotation_RejectsBadImage(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		"上限を超えた画像": imageBody("image/png", usecase.MaxImageBytes+1),
		"PNG 以外":   imageBody("image/jpeg", 32),
		"data が base64 でない": map[string]any{
			"image": map[string]any{"mediaType": "image/png", "data": "!!!"},
		},
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			llm := &stubLLM{text: validInterpretation}
			r := newInterpretRouter(t, llm)

			id := createBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)

			rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			// 弾くと決めた入力で叩くと、結果は捨てるのに課金だけ発生する。
			if llm.calls != 0 {
				t.Errorf("弾いたのに LLM を呼んでいる: %d 回", llm.calls)
			}
		})
	}
}

// 画像の上限はユースケース層が持つが、その判定はボディを全部メモリに載せた
// あとにしかできない。読み込み自体にも歯止めを置いてある。
func TestInterpretAnnotation_RejectsOversizedBody(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	// base64 は 4/3 に膨らむので、上限の倍を送れば読み込みの歯止めに当たる。
	body := imageBody("image/png", usecase.MaxImageBytes*2)

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if llm.calls != 0 {
		t.Errorf("弾いたのに LLM を呼んでいる: %d 回", llm.calls)
	}
}

// このエンドポイントは読むだけ。作成も実行記録も行わない（中核思想 3）。
func TestInterpretAnnotation_DoesNotRecordRun(t *testing.T) {
	t.Parallel()

	boards, mappings := newRepos(t)

	seq := 0
	boardSvc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks(),
		usecase.WithClock(func() time.Time { return fixedTime }),
		usecase.WithIDGenerator(func() string {
			seq++
			return "board-" + string(rune('0'+seq))
		}),
	)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:          boardSvc,
		Annotations:     usecase.NewAnnotationService(boards, mappings),
		Interpretations: usecase.NewInterpretationService(boards, mappings, &stubLLM{text: validInterpretation}),
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
		saveSceneBody(scene, fixedTime)); rec.Code != http.StatusOK {
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
