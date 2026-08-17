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

// stubGitHub は draft issue を作ったことにする GitHubClient。
type stubGitHub struct {
	seq         int
	failOnTitle string
	fields      []port.ProjectField
	// repos と projects は作成先の候補一覧が返すもの。
	repos    []port.Repository
	projects []port.Project
	// listErr が非 nil なら候補一覧が失敗する。
	listErr error
	// canWrite は CanWriteProject が返す値。
	canWrite bool
}

func (s *stubGitHub) CanWriteProject(context.Context, string) (bool, error) {
	return s.canWrite, nil
}

func (s *stubGitHub) ListRepositories(context.Context) ([]port.Repository, error) {
	return s.repos, s.listErr
}

func (s *stubGitHub) ListRepositoryProjects(context.Context, string, string) ([]port.Project, error) {
	return s.projects, s.listErr
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

func (s *stubGitHub) UpdateDraftIssue(_ context.Context, _ string, item port.DraftIssue) error {
	if item.Title == s.failOnTitle {
		return errors.New("github: boom")
	}
	return nil
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
func createBody(contentHash string) map[string]any {
	return map[string]any{
		"summary":     "決済まわりの課題出し",
		"contentHash": contentHash,
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

	// 作成先の固定を守るには、判定する側と前提を崩す側が同じ排他を見る必要が
	// ある。本番の配線（etoki.New）と同じく 1 つを共有させる。
	locks := usecase.NewBoardLocks()

	seq := 0
	boardSvc := usecase.NewBoardService(boards, mappings, locks,
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
		deps.Creations = usecase.NewCreationService(boards, mappings, gh, locks,
			usecase.WithCreationClock(func() time.Time { return fixedTime }))
		deps.Catalog = usecase.NewGitHubCatalogService(gh)
	}

	return httpapi.NewRouter(deps), mappings
}

// newCreateRouterWithBoards は newCreateRouter と同じものに、ボードの
// リポジトリを添えて返す。
//
// 作成先が未選択のボードは API では作れなくなった（ADR 0017）。移行前の
// ボードだけがその形で残るので、API を通さずに作るしかない。
func newCreateRouterWithBoards(
	t *testing.T, gh port.GitHubClient,
) (*gin.Engine, port.BoardRepository) {
	t.Helper()

	boards, mappings := newRepos(t)
	locks := usecase.NewBoardLocks()

	deps := httpapi.Deps{
		Boards: usecase.NewBoardService(boards, mappings, locks,
			usecase.WithClock(func() time.Time { return fixedTime })),
		Annotations: usecase.NewAnnotationService(boards, mappings),
	}
	if gh != nil {
		deps.Creations = usecase.NewCreationService(boards, mappings, gh, locks,
			usecase.WithCreationClock(func() time.Time { return fixedTime }))
	}

	return httpapi.NewRouter(deps), boards
}

// createTargetedBoard はボードを作り、draft issue の作成先まで設定する。
//
// 作成先が未選択のボードには作れない（ADR 0014）。作成そのものを見るテストは
// 全部ここを通す。
func createTargetedBoard(t *testing.T, r *gin.Engine, name string) string {
	t.Helper()

	id := createBoard(t, r, name)

	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/target", map[string]string{
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("set target: %d %s", rec.Code, rec.Body)
	}

	return id
}

func itemsPath(boardID, annotationID string) string {
	return "/api/boards/" + boardID + "/annotations/" + annotationID + "/items"
}

func TestCreateItems(t *testing.T) {
	t.Parallel()

	r, mappings := newCreateRouter(t, &stubGitHub{})

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id)))
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

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	first := decode[map[string]any](t, do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id))))
	second := decode[map[string]any](t, do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id))))

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

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id)))
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

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id)))
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

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id)))
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

// フロントを経由しない呼び出しでもリクエストの内容は信用しない。
func TestCreateItems_RejectsInvalidBody(t *testing.T) {
	t.Parallel()

	tests := map[string]map[string]any{
		"contentHash が無い": {
			"summary":     "s",
			"contentHash": "",
			"items":       []map[string]any{{"localId": "e1", "kind": "epic", "title": "t"}},
		},
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

			id := createTargetedBoard(t, r, "設計会")
			saveAnnotatedScene(t, r, id)
			if _, ok := body["contentHash"]; !ok {
				body["contentHash"] = currentHash(t, r, id)
			}

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

// previousItemId はリクエストから来る。確かめずに通すと、任意の node ID を書いて
// 無関係な draft issue を書き換えられる（ADR 0026）。
func TestCreateItems_RejectsUnknownPreviousItem(t *testing.T) {
	t.Parallel()

	gh := &stubGitHub{}
	r, mappings := newCreateRouter(t, gh)

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	body := createBody(currentHash(t, r, id))
	items, _ := body["items"].([]map[string]any)
	items[0]["previousItemId"] = "PVTI_someone_else"

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}

	// 1 件も作らず 1 件も更新しない。半分だけ進めると、取り消せないのに
	// どこまで進んだのかが分からなくなる。
	if gh.seq != 0 {
		t.Errorf("見知らぬ更新先なのに GitHub を呼んでいる: %d 回", gh.seq)
	}
	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("止めたのに run が記録されている: %+v", run)
	}
}

func TestCreateItems_RejectsMismatchedContentHash(t *testing.T) {
	t.Parallel()

	gh := &stubGitHub{}
	r, mappings := newCreateRouter(t, gh)

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody("stale-hash"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if msg := decode[map[string]string](t, rec)["error"]; !strings.Contains(msg, "interpret again") {
		t.Errorf("解釈し直すべきことが返っていない: %q", msg)
	}
	if gh.seq != 0 {
		t.Errorf("hash が食い違っているのに GitHub を呼んでいる: %d 回", gh.seq)
	}

	run, err := mappings.FindLatestRun(t.Context(), id, "annot-1")
	if err != nil {
		t.Fatalf("FindLatestRun: %v", err)
	}
	if run != nil {
		t.Errorf("hash が食い違っているのに run が記録されている: %+v", run)
	}
}

func TestCreateItems_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})

	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	if rec := do(t, r, http.MethodPost, itemsPath(id, "no-such-annot"), createBody(currentHash(t, r, id))); rec.Code != http.StatusNotFound {
		t.Errorf("注釈が無い: status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if rec := do(t, r, http.MethodPost, itemsPath("no-such-board", "annot-1"), createBody(currentHash(t, r, id))); rec.Code != http.StatusNotFound {
		t.Errorf("ボードが無い: status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
}

// ---------------------------------------------------------------------------
// 作成先の設定と候補一覧（ADR 0014）
// ---------------------------------------------------------------------------

func targetPath(boardID string) string { return "/api/boards/" + boardID + "/target" }

func setTargetBody() map[string]string {
	return map[string]string{
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
	}
}

// 新しいボードは作成先を持って生まれる。候補は書ける Project だけに絞って
// あるので、書ける先を 1 つも持たない人はここまで来られない（ADR 0017）。
func TestCreateBoard_StartsWithTarget(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})
	id := createBoard(t, r, "設計会")

	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["projectId"] != "PVT_1" || got["repositoryOwner"] != "acme" {
		t.Errorf("作成先が入っていない: %+v", got)
	}
	if got["targetLocked"] != false {
		t.Errorf("targetLocked = %v, want false", got["targetLocked"])
	}
}

// 作成先を省いたら作れない。「ボードの作成にはリポジトリへのアクセス権が
// 要る」を、この 1 本で守っている（ADR 0017）。
func TestCreateBoard_RequiresTarget(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})

	for name, body := range map[string]map[string]string{
		"作成先が無い":      {"name": "設計会"},
		"project が無い": {"name": "設計会", "repositoryOwner": "acme", "repositoryName": "web"},
		"リポジトリが無い":    {"name": "設計会", "projectId": "PVT_1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, r, http.MethodPost, "/api/boards", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
		})
	}
}

func TestSetBoardTarget_StoresAndReturnsBoard(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})
	id := createBoard(t, r, "設計会")

	rec := do(t, r, http.MethodPut, targetPath(id), setTargetBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	got := decode[map[string]any](t, rec)
	if got["repositoryOwner"] != "acme" || got["repositoryName"] != "web" || got["projectId"] != "PVT_1" {
		t.Errorf("設定後のボード = %+v", got)
	}

	// 読み直しても残っている。
	again := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if again["projectId"] != "PVT_1" {
		t.Errorf("projectId = %v, want PVT_1", again["projectId"])
	}
}

// 最初の draft issue を作ると固定される。以後の変更は 409。
func TestSetBoardTarget_ConflictsAfterFirstCreation(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})
	id := createTargetedBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodPost, itemsPath(id, "annot-1"), createBody(currentHash(t, r, id)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("作成に失敗: %d %s", rec.Code, rec.Body)
	}

	rec = do(t, r, http.MethodPut, targetPath(id), map[string]string{
		"repositoryOwner": "acme", "repositoryName": "other", "projectId": "PVT_2",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}

	// 固定は状態として返る。フロントは sync_runs を数えられない。
	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["targetLocked"] != true {
		t.Errorf("targetLocked = %v, want true", got["targetLocked"])
	}
	if got["repositoryName"] != "web" {
		t.Errorf("作成先が変わっている: %v", got["repositoryName"])
	}
}

func TestSetBoardTarget_RejectsIncomplete(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})
	id := createBoard(t, r, "設計会")

	rec := do(t, r, http.MethodPut, targetPath(id), map[string]string{
		"repositoryOwner": "acme", "repositoryName": "web", "projectId": "",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
}

func TestSetBoardTarget_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})

	rec := do(t, r, http.MethodPut, targetPath("missing"), setTargetBody())
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
}

// 作成先が未選択のボードには作れない。設定不足なので 422。
//
// 新規には生まれない形だが（ADR 0017）、移行前のボードは未選択のまま残るので
// 経路は消えない。API を通さずに作って確かめる。
func TestCreateItems_RejectsBoardWithoutTarget(t *testing.T) {
	t.Parallel()

	r, boards := newCreateRouterWithBoards(t, &stubGitHub{})

	if err := boards.Create(t.Context(), port.Board{
		ID: "legacy", Name: "移行前のボード", Scene: annotatedScene,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}, ""); err != nil {
		t.Fatalf("seed legacy board: %v", err)
	}

	rec := do(t, r, http.MethodPost, itemsPath("legacy", "annot-1"),
		createBody(currentHash(t, r, "legacy")))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}
}

func TestListRepositories(t *testing.T) {
	t.Parallel()

	gh := &stubGitHub{repos: []port.Repository{{Owner: "acme", Name: "web", Description: "フロント"}}}
	r, _ := newCreateRouter(t, gh)

	got := decode[[]map[string]any](t, do(t, r, http.MethodGet, "/api/github/repositories", nil))
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0]["owner"] != "acme" || got[0]["name"] != "web" {
		t.Errorf("repositories[0] = %+v", got[0])
	}
}

// 0 件でも null ではなく配列を返す。
func TestListRepositories_EmptyIsArray(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{})

	rec := do(t, r, http.MethodGet, "/api/github/repositories", nil)
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Errorf("body = %q, want []", body)
	}
}

func TestListRepositoryProjects(t *testing.T) {
	t.Parallel()

	gh := &stubGitHub{projects: []port.Project{{ID: "PVT_9", Number: 3, Title: "ロードマップ"}}}
	r, _ := newCreateRouter(t, gh)

	got := decode[[]map[string]any](t, do(t, r,
		http.MethodGet, "/api/github/repositories/acme/web/projects", nil))
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (%+v)", len(got), got)
	}
	if got[0]["id"] != "PVT_9" || got[0]["title"] != "ロードマップ" {
		t.Errorf("projects[0] = %+v", got[0])
	}
}

// GitHub 側の失敗は 502。500 にすると etoki の不具合と読めてしまい、
// トークンの権限不足やレート制限であることが伝わらない。
func TestListRepositories_GitHubFailureIsBadGateway(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, &stubGitHub{listErr: errors.New("github api: 401: Bad credentials")})

	rec := do(t, r, http.MethodGet, "/api/github/repositories", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (%s)", rec.Code, rec.Body)
	}
}

// GitHub 未設定でもサーバーは起動する。一覧だけが「設定されていない」と返す。
func TestListRepositories_UnavailableWithoutGitHub(t *testing.T) {
	t.Parallel()

	r, _ := newCreateRouter(t, nil)

	rec := do(t, r, http.MethodGet, "/api/github/repositories", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
}
