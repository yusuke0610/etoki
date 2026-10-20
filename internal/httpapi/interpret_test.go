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
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
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

	return newInterpretRouterWithLimits(t, llm, newLimiter())
}

// newInterpretRouterWithLimits は実行の上限を指定してルーターを返す。
//
// 上限に当たったときの応答を見るテストだけが使う（ADR 0044）。
func newInterpretRouterWithLimits(
	t *testing.T, llm port.LLMClient, limits *usecase.LLMLimiter,
) *gin.Engine {
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
		deps.Interpretations = usecase.NewInterpretationService(boards, mappings, llm, limits,
			usecase.WithMaxAttempts(2))
	}

	return httpapi.NewRouter(deps)
}

// interpretPath は解釈エンドポイントの URL を組み立てる。
func interpretPath(boardID, annotationID string) string {
	return "/api/boards/" + boardID + "/annotations/" + annotationID + "/interpret"
}

// jobPath は解釈のジョブの URL を組み立てる。空の jobID はボードの一覧。
func jobPath(boardID, jobID string) string {
	p := "/api/boards/" + boardID + "/interpretation-jobs"
	if jobID != "" {
		p += "/" + jobID
	}
	return p
}

// startInterpretation は解釈を始めて、202 で返ったジョブを返す。
func startInterpretation(
	t *testing.T, r *gin.Engine, boardID, annotationID string, body any,
) apitypes.InterpretationJob {
	t.Helper()

	rec := do(t, r, http.MethodPost, interpretPath(boardID, annotationID), body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body)
	}
	return decode[apitypes.InterpretationJob](t, rec)
}

// interpretAndWait は解釈を始めて、走り終わったジョブを返す。
func interpretAndWait(
	t *testing.T, r *gin.Engine, boardID, annotationID string, body any,
) apitypes.InterpretationJob {
	t.Helper()

	started := startInterpretation(t, r, boardID, annotationID, body)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rec := do(t, r, http.MethodGet, jobPath(boardID, started.ID), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET job: status = %d, want 200 (%s)", rec.Code, rec.Body)
		}
		job := decode[apitypes.InterpretationJob](t, rec)
		if job.Status != apitypes.InterpretationJobStatusRunning {
			return job
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("ジョブが終わらない")
	return apitypes.InterpretationJob{}
}

func TestInterpretAnnotation(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	job := interpretAndWait(t, r, id, "annot-1", nil)
	if job.Status != apitypes.InterpretationJobStatusSucceeded || job.Result == nil {
		t.Fatalf("job = %+v, want succeeded with result", job)
	}
	if job.AnnotationID != "annot-1" {
		t.Errorf("annotationId = %q", job.AnnotationID)
	}
	got := *job.Result

	// summary は GitHub に作らないが、確認材料として必ず返す（ADR 0006）。
	if got.Summary != "決済まわりの課題出し" {
		t.Errorf("summary = %v", got.Summary)
	}
	if got.ContentHash != currentHash(t, r, id) {
		t.Errorf("contentHash = %v, want %s", got.ContentHash, currentHash(t, r, id))
	}

	if len(got.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(got.Items))
	}

	epic := got.Items[0]
	if epic.Kind != apitypes.ItemKindEpic || epic.LocalID != "e1" {
		t.Errorf("items[0] = %v", epic)
	}
	if epic.ParentLocalID != "" {
		t.Errorf("epic に parentLocalId が付いている: %v", epic)
	}

	if got.Items[1].ParentLocalID != "e1" {
		t.Errorf("items[1].parentLocalId = %v, want e1", got.Items[1].ParentLocalID)
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

	interpretAndWait(t, r, id, "annot-1", imageBody("image/png", 32))

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

			interpretAndWait(t, r, id, "annot-1", body)
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
//
// **歯止めに当たった失敗も契約の code に写す**（`.claude/rules/api-contract.md`、
// issue #147）。400 に落とすと、同じ「大きすぎる」がボディの大きさしだいで
// 400 と 413 に割れる。
func TestInterpretAnnotation_RejectsOversizedBody(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	// base64 は 4/3 に膨らむので、上限の倍を送れば読み込みの歯止めに当たる。
	body := imageBody("image/png", usecase.MaxImageBytes*2)

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), body)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeRequestTooLarge {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeRequestTooLarge)
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
		Boards:      boardSvc,
		Annotations: usecase.NewAnnotationService(boards, mappings),
		Interpretations: usecase.NewInterpretationService(
			boards, mappings, &stubLLM{text: validInterpretation}, newLimiter()),
	})

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	interpretAndWait(t, r, id, "annot-1", nil)

	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("解釈だけで run が記録されている: %+v", run)
	}

	// 状態も uncreated のまま。
	states := listAnnotations(t, r, id).Annotations
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
	body := decode[apitypes.ErrorResponse](t, rec)
	// GitHub 未設定と同じ code に畳まない。設定するものが違う（#48 が使う）。
	if body.Code != apitypes.ErrorCodeLlmNotConfigured {
		t.Errorf("code = %q, want %q", body.Code, apitypes.ErrorCodeLlmNotConfigured)
	}
	if body.Error == "" {
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
		code apitypes.ErrorCode
	}{
		"呼び出しが失敗する": {
			llm:  &stubLLM{err: errors.New("messages api: 401 authentication_error")},
			want: "401",
			code: apitypes.ErrorCodeLlmUnavailable,
		},
		"出力がスキーマを満たさない": {
			llm:  &stubLLM{text: `{"summary":"","items":[]}`},
			want: "schema",
			code: apitypes.ErrorCodeInterpretationFailed,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newInterpretRouter(t, tt.llm)

			id := createBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)

			// 失敗はジョブの中身に載る。始めるリクエストは 202 で返っている。
			job := interpretAndWait(t, r, id, "annot-1", nil)
			if job.Status != apitypes.InterpretationJobStatusFailed || job.Error == nil {
				t.Fatalf("job = %+v, want failed with error", job)
			}
			if job.Result != nil {
				t.Error("失敗したのに result が入っている")
			}
			// 打ち手は code で分ける。リクエストの失敗と同じ表で写す。
			if job.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", job.Error.Code, tt.code)
			}
			if !strings.Contains(job.Error.Error, tt.want) {
				t.Errorf("error = %q, want to contain %q", job.Error.Error, tt.want)
			}
		})
	}
}

// 粒度が不正なシーンはリクエストの問題として 400 で返す。
//
// **弾くのは保存の入口**（#276）。保存を通らずに残った古いシーンを解釈で弾く
// 経路は、ユースケースのテスト（TestInterpret_RejectsUnknownGranularity）が
// LLM を呼ばないことまで見ている。
func TestInterpretAnnotation_InvalidGranularity(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)

	id := createBoard(t, r, "設計会")
	scene := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"project"}}}]}`
	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene", saveSceneBody(scene, fixedTime))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("save scene: status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeInvalidInput {
		t.Errorf("code = %s, want invalid_input", code)
	}
	if llm.calls != 0 {
		t.Error("粒度が不正なのに LLM を呼んでいる")
	}
}

// 上限に当たったら 429 と、打ち手の分かる code を返す（ADR 0044）。
//
// **LLM は呼ばない。** 呼んでから捨てると課金だけが発生する。
func TestInterpretAnnotation_RateLimited(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouterWithLimits(t, llm,
		usecase.NewLLMLimiter(usecase.LLMLimits{RateLimit: 1}))

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	interpretAndWait(t, r, id, "annot-1", nil)

	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-1"), nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("2 回目の status = %d, want 429 (%s)", rec.Code, rec.Body)
	}
	if got := decode[apitypes.ErrorResponse](t, rec); got.Code != apitypes.ErrorCodeRateLimited {
		t.Errorf("code = %q, want %q", got.Code, apitypes.ErrorCodeRateLimited)
	}
	if llm.calls != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", llm.calls)
	}
}

// gateLLM は release が閉じるまで応答を返さない LLMClient。走っている最中の
// 振る舞いを見るために使う。
type gateLLM struct {
	text    string
	release chan struct{}
	started chan struct{}
	ended   chan struct{}
}

func newGateLLM() *gateLLM {
	return &gateLLM{
		text:    validInterpretation,
		release: make(chan struct{}),
		started: make(chan struct{}, 4),
		ended:   make(chan struct{}, 4),
	}
}

func (g *gateLLM) Complete(ctx context.Context, _ port.VisionRequest) (port.VisionResponse, error) {
	g.started <- struct{}{}
	select {
	case <-g.release:
		return port.VisionResponse{Text: g.text}, nil
	case <-ctx.Done():
		g.ended <- struct{}{}
		return port.VisionResponse{}, ctx.Err()
	}
}

// 始めるリクエストは LLM の応答を待たない。走っている最中はジョブが running で、
// 同じ注釈をもう一度押しても同じジョブが返る（ADR 0085）。
func TestInterpretJobs_RunningThenSameJobOnSecondStart(t *testing.T) {
	t.Parallel()

	llm := newGateLLM()
	r := newInterpretRouter(t, llm)
	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	first := startInterpretation(t, r, id, "annot-1", nil)
	if first.Status != apitypes.InterpretationJobStatusRunning || first.ID == "" {
		t.Fatalf("first = %+v, want running", first)
	}
	<-llm.started

	second := startInterpretation(t, r, id, "annot-1", nil)
	if second.ID != first.ID {
		t.Errorf("2 回目の id = %q, want %q", second.ID, first.ID)
	}

	// 一覧には走っているジョブが見え、結果は載らない。
	list := decode[[]apitypes.InterpretationJob](t, do(t, r, http.MethodGet, jobPath(id, ""), nil))
	if len(list) != 1 || list[0].ID != first.ID || list[0].Status != apitypes.InterpretationJobStatusRunning {
		t.Errorf("list = %+v", list)
	}

	close(llm.release)
}

// 一覧は結果を運ばず、1 件の取得は読んでも消えない。捨てると両方から消える。
func TestInterpretJobs_ListGetDiscard(t *testing.T) {
	t.Parallel()

	r := newInterpretRouter(t, &stubLLM{text: validInterpretation})
	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	done := interpretAndWait(t, r, id, "annot-1", nil)

	list := decode[[]apitypes.InterpretationJob](t, do(t, r, http.MethodGet, jobPath(id, ""), nil))
	if len(list) != 1 || list[0].Status != apitypes.InterpretationJobStatusSucceeded || list[0].Result != nil {
		t.Errorf("list = %+v, want 成功・結果なし", list)
	}

	for range 2 {
		got := decode[apitypes.InterpretationJob](t, do(t, r, http.MethodGet, jobPath(id, done.ID), nil))
		if got.Result == nil {
			t.Fatal("読み直したら結果が無い")
		}
	}

	if rec := do(t, r, http.MethodDelete, jobPath(id, done.ID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if rec := do(t, r, http.MethodGet, jobPath(id, done.ID), nil); rec.Code != http.StatusNotFound {
		t.Errorf("捨てた後の GET status = %d, want 404", rec.Code)
	}
	if rec := do(t, r, http.MethodDelete, jobPath(id, done.ID), nil); rec.Code != http.StatusNotFound {
		t.Errorf("2 回目の DELETE status = %d, want 404", rec.Code)
	}
	list = decode[[]apitypes.InterpretationJob](t, do(t, r, http.MethodGet, jobPath(id, ""), nil))
	if len(list) != 0 {
		t.Errorf("捨てた後の list = %+v, want 空", list)
	}
}

// 走っているジョブを捨てると LLM の呼び出しを止める。
func TestInterpretJobs_DiscardRunningStopsLLM(t *testing.T) {
	t.Parallel()

	llm := newGateLLM()
	r := newInterpretRouter(t, llm)
	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	job := startInterpretation(t, r, id, "annot-1", nil)
	<-llm.started

	if rec := do(t, r, http.MethodDelete, jobPath(id, job.ID), nil); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	select {
	case <-llm.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("捨てても LLM の呼び出しが止まらない")
	}
}

// 始めるリクエストが終わって（ctx が切れて）も、解釈は止まらない。#307 の本題。
func TestInterpretJobs_SurvivesRequestEnd(t *testing.T) {
	t.Parallel()

	llm := newGateLLM()
	r := newInterpretRouter(t, llm)
	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	ctx, cancel := context.WithCancel(t.Context())
	rec := doWithContext(t, ctx, r, http.MethodPost, interpretPath(id, "annot-1"), nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (%s)", rec.Code, rec.Body)
	}
	job := decode[apitypes.InterpretationJob](t, rec)
	<-llm.started
	cancel()

	select {
	case <-llm.ended:
		t.Fatal("リクエストが終わったら解釈が止まった")
	case <-time.After(50 * time.Millisecond):
	}

	close(llm.release)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got := decode[apitypes.InterpretationJob](t, do(t, r, http.MethodGet, jobPath(id, job.ID), nil))
		if got.Status == apitypes.InterpretationJobStatusSucceeded {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("リクエストが終わった後で結果が取れない")
}

// 別の注釈を解釈している最中は、これまでどおり 429（ADR 0044）。
func TestInterpretJobs_OtherAnnotationIsConcurrencyLimited(t *testing.T) {
	t.Parallel()

	llm := newGateLLM()
	r := newInterpretRouter(t, llm)
	id := createBoard(t, r, "設計会")
	scene := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"a","customData":{"etoki":{"granularity":""}}},
		{"id":"annot-2","type":"frame","name":"b","customData":{"etoki":{"granularity":""}}}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene", saveSceneBody(scene, fixedTime)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d (%s)", rec.Code, rec.Body)
	}

	startInterpretation(t, r, id, "annot-1", nil)
	rec := do(t, r, http.MethodPost, interpretPath(id, "annot-2"), nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 (%s)", rec.Code, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeConcurrencyLimited {
		t.Errorf("code = %q", code)
	}
	close(llm.release)
}

// 知らないジョブは 404。ボードが無いときも 404。
func TestInterpretJobs_NotFound(t *testing.T) {
	t.Parallel()

	r := newInterpretRouter(t, &stubLLM{text: validInterpretation})
	id := createBoard(t, r, "設計会")

	for name, tt := range map[string]struct{ method, path string }{
		"GET 知らないジョブ":    {http.MethodGet, jobPath(id, "nope")},
		"DELETE 知らないジョブ": {http.MethodDelete, jobPath(id, "nope")},
		"一覧 知らないボード":     {http.MethodGet, jobPath("nope", "")},
	} {
		if rec := do(t, r, tt.method, tt.path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (%s)", name, rec.Code, rec.Body)
		}
	}
}

// LLM が無いときは 3 つとも 503。
func TestInterpretJobs_WithoutLLM(t *testing.T) {
	t.Parallel()

	r := newInterpretRouter(t, nil)
	id := createBoard(t, r, "設計会")

	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, jobPath(id, "")},
		{http.MethodGet, jobPath(id, "x")},
		{http.MethodDelete, jobPath(id, "x")},
	} {
		rec := do(t, r, tt.method, tt.path, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", tt.method, tt.path, rec.Code)
		}
	}
}
