package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// stubGitHub は draft issue を作ったことにする GitHubClient。
type stubGitHub struct {
	seq         int
	failOnTitle string
	fields      []port.ProjectField
}

func (s *stubGitHub) ListProjectFields(context.Context, string) ([]port.ProjectField, error) {
	if s.fields != nil {
		return s.fields, nil
	}
	return []port.ProjectField{
		{ID: "F_kind", Name: "Kind", DataType: "SINGLE_SELECT", Options: []port.ProjectFieldOption{
			{ID: "O_epic", Name: "epic"}, {ID: "O_issue", Name: "issue"},
		}},
		{ID: "F_parent", Name: "Parent", DataType: "TEXT"},
	}, nil
}

func (s *stubGitHub) CreateDraftIssue(_ context.Context, _ string, item port.DraftIssue) (string, error) {
	if item.Title == s.failOnTitle {
		return "", errors.New("github: boom")
	}
	s.seq++
	return "PVTI_" + string(rune('a'+s.seq-1)), nil
}

func (s *stubGitHub) SetItemFieldValue(context.Context, string, string, port.FieldValue) error {
	return nil
}

// createBody は解釈結果をそのまま送るリクエストボディ。
func createBody() map[string]any {
	return map[string]any{
		"summary": "決済まわりの課題出し",
		"items": []map[string]any{
			{"localId": "e1", "kind": "epic", "title": "決済フローの見直し", "body": "全体の方針"},
			{"localId": "i1", "kind": "issue", "title": "Stripe SDK の更新", "parentLocalId": "e1"},
		},
	}
}

// newCreateRouter は作成サービス付きのルーターを返す。
// gh が nil なら作成サービスを組み立てず、503 を返す構成になる。
func newCreateRouter(t *testing.T, gh port.GitHubClient) (*gin.Engine, port.MappingRepository) {
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
	if gh != nil {
		deps.Creations = usecase.NewCreationService(boards, mappings, gh, "PVT_1",
			usecase.WithCreationClock(func() time.Time { return fixedTime }))
	}

	return httpapi.NewRouter(deps), mappings
}

func itemsPath(boardID, annotationID string) string {
	return "/api/boards/" + boardID + "/annotations/" + annotationID + "/items"
}

func TestCreateItems(t *testing.T) {
	t.Parallel()

	r, mappings := newCreateRouter(t, &stubGitHub{})

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	got := decode[map[string]any](t, rec)
	items, _ := got["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if got["incomplete"] != nil {
		t.Errorf("incomplete = %v, want 未設定", got["incomplete"])
	}

	// run が記録され、状態が created に変わる。ここが繋がって初めて
	// 3 状態判定が uncreated 以外に遷移する。
	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run == nil {
		t.Fatal("run が記録されていない")
	}
	if len(run.Items) != 2 {
		t.Errorf("len(run.Items) = %d, want 2", len(run.Items))
	}

	states := decode[[]map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))
	if len(states) != 1 {
		t.Fatalf("len(states) = %d, want 1", len(states))
	}
	if states[0]["state"] != "created" {
		t.Errorf("state = %v, want created", states[0]["state"])
	}
}

// 再実行しても過去の run は消さない（ADR 0007）。
func TestCreateItems_KeepsPastRuns(t *testing.T) {
	t.Parallel()

	r, mappings := newCreateRouter(t, &stubGitHub{})

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	first := decode[map[string]any](t, do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody()))
	second := decode[map[string]any](t, do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody()))

	if first["runId"] == second["runId"] {
		t.Errorf("2 回目が同じ run を上書きしている: %v", first["runId"])
	}

	// 最新は 2 回目。過去の run は消えていない。
	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if float64(run.ID) != second["runId"] {
		t.Errorf("最新 run = %d, want %v", run.ID, second["runId"])
	}
}

// エラーだけ返すと、開発者は何も作られていないと誤解して再実行し重複を増やす。
func TestCreateItems_ReportsPartialCreation(t *testing.T) {
	t.Parallel()

	r, mappings := newCreateRouter(t, &stubGitHub{failOnTitle: "Stripe SDK の更新"})

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody())
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	got := decode[map[string]any](t, rec)
	if got["incomplete"] != true {
		t.Errorf("incomplete = %v, want true", got["incomplete"])
	}
	if got["error"] == nil || got["error"] == "" {
		t.Error("何が失敗したのか返っていない")
	}

	items, _ := got["items"].([]any)
	if len(items) != 1 {
		t.Errorf("len(items) = %d, want 1", len(items))
	}

	// 作れたぶんは記録する。記録漏れは追跡不能を生む（ADR 0009）。
	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run == nil || len(run.Items) != 1 {
		t.Errorf("部分的な run が記録されていない: %+v", run)
	}
}

func TestCreateItems_WithoutGitHub(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, nil)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
}

// 設定不足であって、リクエストの誤りではない。
func TestCreateItems_MissingProjectFields(t *testing.T) {
	t.Parallel()

	gh := &stubGitHub{fields: []port.ProjectField{
		{ID: "F_title", Name: "Title", DataType: "TITLE"},
	}}
	r, mappings := newCreateRouter(t, gh)

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}
	if msg := decode[map[string]string](t, rec)["error"]; msg == "" {
		t.Error("何を作ればよいのか返っていない")
	}

	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("作成していないのに run が記録されている: %+v", run)
	}
}

// フロントを経由しない呼び出しでも 2 階層の制約が守られる必要がある。
func TestCreateItems_RejectsInvalidBody(t *testing.T) {
	t.Parallel()

	tests := map[string]map[string]any{
		"summary が空": {
			"items": []map[string]any{{"localId": "e1", "kind": "epic", "title": "t"}},
		},
		"epic が親を持つ": {
			"summary": "s",
			"items": []map[string]any{
				{"localId": "e1", "kind": "epic", "title": "t1"},
				{"localId": "e2", "kind": "epic", "title": "t2", "parentLocalId": "e1"},
			},
		},
		"種別が不正": {
			"summary": "s",
			"items":   []map[string]any{{"localId": "p1", "kind": "project", "title": "t"}},
		},
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, mappings := newCreateRouter(t, &stubGitHub{})

			id := createBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)

			rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}

			run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
			if err != nil {
				t.Fatalf("FindLatestRun: %v", err)
			}
			if run != nil {
				t.Errorf("検証を通っていないのに run が記録されている: %+v", run)
			}
		})
	}
}

func TestCreateItems_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})

	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	if rec := do(t, r, http.MethodPost, itemsPath(id, "no-such-annot"), createBody()); rec.Code != http.StatusNotFound {
		t.Errorf("注釈が無い: status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if rec := do(t, r, http.MethodPost, itemsPath("no-such-board", "annot-1"), createBody()); rec.Code != http.StatusNotFound {
		t.Errorf("ボードが無い: status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
}
