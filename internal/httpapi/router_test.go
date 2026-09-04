package httpapi_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

var fixedTime = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

// newRouter は一時 DB に紐づいたルーターを返す。
//
// フェイクではなく実際の SQLite を使う。ハンドラとリポジトリの間で
// 型や制約がずれていないことまで含めて確かめたいため。
func newRouter(t *testing.T) (*gin.Engine, port.MappingRepository) {
	t.Helper()

	r, mappings, _ := newRouterWithDB(t)

	return r, mappings
}

// newRouterWithDB は DB のハンドルも返す。**移行前の行を再現するときだけ使う。**
// port を通しては書けない値（結末を記録していない run など）を入れるため。
func newRouterWithDB(t *testing.T) (*gin.Engine, port.MappingRepository, *sql.DB) {
	t.Helper()

	db := openTempDB(t)
	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	boards, mappings := sqlite.NewBoardRepository(db), sqlite.NewMappingRepository(db)

	seq := 0
	boardSvc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks(),
		usecase.WithClock(func() time.Time { return fixedTime }),
		usecase.WithIDGenerator(func() string {
			seq++
			return "board-" + string(rune('0'+seq))
		}),
	)

	return httpapi.NewRouter(httpapi.Deps{
		Boards:      boardSvc,
		Annotations: usecase.NewAnnotationService(boards, mappings),
	}), mappings, db
}

// newBoardBody は作成先つきのボード作成ボディを返す。
//
// 作成先は必須（ADR 0017）。ここを省くと 400 になるので、ほとんどのテストは
// これを通す。
func newBoardBody(name string) map[string]string {
	return map[string]string{
		"name":            name,
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
	}
}

func withScene(body map[string]string, scene string) map[string]string {
	body["scene"] = scene
	return body
}

func do(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	// httptest の既定の Host は example.com。実際に届く形と揃えないと、
	// cross-site を弾くミドルウェアに引っかかる（origin.go）。
	req.Host = loopbackHost

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	return rec
}

// decodeOK は 200 を確かめてから読む。**status を見ずに decode しない。**
// ErrorResponse も多くの型に decode できるので、確かめずに進むと 403 や 500 が
// 「フィールドが写っていない」という見当違いの失敗になる（#104）。
func decodeOK[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	return decode[T](t, rec)
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	rec := do(t, r, http.MethodGet, "/healthz", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := decode[map[string]string](t, rec)["status"]; got != "ok" {
		t.Errorf("status = %q, want %q", got, "ok")
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	if rec := do(t, r, http.MethodGet, "/no-such-route", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateAndGetBoard(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPost, "/api/boards", newBoardBody("決済まわり"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body)
	}

	created := decode[map[string]any](t, rec)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("id が空: %v", created)
	}
	// scene を省略しても Excalidraw が読める空シーンで初期化される。
	if scene, _ := created["scene"].(string); scene == "" {
		t.Error("scene が空のまま作成されている")
	}

	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["name"] != "決済まわり" {
		t.Errorf("name = %v", got["name"])
	}
}

func TestCreateBoard_RejectsEmptyName(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	body := newBoardBody("")
	rec := do(t, r, http.MethodPost, "/api/boards", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// 壊れたシーンは入口で弾く。保存できてしまうとボードごと開けなくなる。
func TestCreateBoard_RejectsBrokenScene(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPost, "/api/boards",
		withScene(newBoardBody("壊れたボード"), "{"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGetBoard_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	if rec := do(t, r, http.MethodGet, "/api/boards/no-such-id", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestListBoards_EmptyIsArrayNotNull(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/boards", nil)
	if body := rec.Body.String(); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

// 一覧にも作成先が載る。サイドバーはこれでリポジトリと Project ごとに
// ボードをまとめる（ADR 0019）。詳細を 1 件ずつ引かせない。
func TestListBoards_IncludesTarget(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPost, "/api/boards", map[string]any{
		"name":            "決済まわり",
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
		"projectNumber":   3,
		"projectTitle":    "ロードマップ",
		"projectUrl":      "https://github.com/orgs/acme/projects/3",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body)
	}

	// 詰め替えは toDetail と toSummary の 2 経路あり、どちらも写し忘れても
	// コンパイルは通る。作成の応答と一覧の両方で確かめる。
	detail := decode[map[string]any](t, rec)
	if got := detail["projectUrl"]; got != "https://github.com/orgs/acme/projects/3" {
		t.Errorf("作成の応答の projectUrl = %v", got)
	}

	list := decode[[]map[string]any](t, do(t, r, http.MethodGet, "/api/boards", nil))
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}

	for key, want := range map[string]any{
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
		// JSON の数値は float64 で戻る。
		"projectNumber": float64(3),
		"projectTitle":  "ロードマップ",
		// URL は番号から組み立てず、送られたものをそのまま控える（ADR 0025）。
		"projectUrl": "https://github.com/orgs/acme/projects/3",
	} {
		if got := list[0][key]; got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}

	// targetLocked は一覧に載せない。run の照会がボード数だけ増える。
	if _, ok := list[0]["targetLocked"]; ok {
		t.Error("一覧に targetLocked が載っている")
	}
}

// 表示名だけを取り直す口（ADR 0037）。作成先そのものは動かないこと、
// 違う projectId は 409 になることを応答の形で固定する。
func TestRefreshBoardTargetDisplay(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPost, "/api/boards", map[string]any{
		"name":            "決済まわり",
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
		"projectNumber":   3,
		"projectTitle":    "ロードマップ",
		"projectUrl":      "https://github.com/orgs/acme/projects/3",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d (%s)", rec.Code, http.StatusCreated, rec.Body)
	}
	id := decode[map[string]any](t, rec)["id"].(string)

	rec = do(t, r, http.MethodPut, "/api/boards/"+id+"/target/display", map[string]any{
		"projectId":     "PVT_1",
		"projectNumber": 4,
		"projectTitle":  "改名後のロードマップ",
		"projectUrl":    "https://github.com/orgs/acme/projects/4",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	got := decode[map[string]any](t, rec)
	for key, want := range map[string]any{
		"projectTitle": "改名後のロードマップ",
		"projectUrl":   "https://github.com/orgs/acme/projects/4",
		// JSON の数値は float64 で戻る。
		"projectNumber": float64(4),
		// 作成先そのものは動かない。ここが変わると ADR 0014 の固定が意味を失う。
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
}

func TestRefreshBoardTargetDisplay_RejectsOtherProject(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPost, "/api/boards", map[string]any{
		"name":            "決済まわり",
		"repositoryOwner": "acme",
		"repositoryName":  "web",
		"projectId":       "PVT_1",
	})
	id := decode[map[string]any](t, rec)["id"].(string)

	rec = do(t, r, http.MethodPut, "/api/boards/"+id+"/target/display", map[string]any{
		"projectId":    "PVT_2",
		"projectTitle": "別の Project",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusConflict, rec.Body)
	}
	// 画面は code で打ち手を分ける（ADR 0034）。固定（target_locked）とは別物。
	if code := decode[map[string]any](t, rec)["code"]; code != "target_mismatch" {
		t.Errorf("code = %v, want target_mismatch", code)
	}
}

// saveSceneBody は保存のボディを組み立てる。
//
// 基準の版は必須（ADR 0020）。省くと 400 になるので、保存を通すテストは
// これを通す。base を any で受けるのは、取得したボードの updatedAt（文字列）を
// クライアントと同じようにそのまま送り返すテストがあるため。
// 改名は名前だけを変える。**版（updatedAt）は動かさない**（ADR 0020）。
// 動かすと、そのボードを開いている別のメンバーの次の保存が、誰もシーンを
// 触っていないのに 409 で断られる。
func TestRenameBoard(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "古い名前")
	before := decode[apitypes.BoardDetail](t,
		do(t, r, http.MethodGet, "/api/boards/"+id, nil))

	rec := do(t, r, http.MethodPatch, "/api/boards/"+id,
		map[string]string{"name": "新しい名前"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	got := decode[apitypes.BoardDetail](t, rec)
	if got.Name != "新しい名前" {
		t.Errorf("name = %q, want 新しい名前", got.Name)
	}
	if !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updatedAt = %v, want %v（改名で進めてはいけない）",
			got.UpdatedAt, before.UpdatedAt)
	}
	// 名前だけの口。作成先やシーンを一緒に書けると、固定（ADR 0014）が
	// 意味を失う。
	if got.ProjectID != before.ProjectID || got.Scene != before.Scene {
		t.Errorf("名前以外が変わっている: projectId = %q, scene = %q",
			got.ProjectID, got.Scene)
	}
}

// 改名したあとも、同じ基準で保存できる。**これが版を進めない理由そのもの。**
// 進める実装にすると、このテストが 409 で落ちる。
func TestRenameBoard_DoesNotBreakPendingSave(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	if rec := do(t, r, http.MethodPatch, "/api/boards/"+id,
		map[string]string{"name": "会議中に改名"}); rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body)
	}

	// 改名の前に開いていた人が持っている基準は、作成時の版のまま。
	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(annotatedScene, fixedTime))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d（改名で版を進めている） %s",
			rec.Code, http.StatusOK, rec.Body)
	}
}

func TestRenameBoard_RejectsBlank(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "元の名前")

	rec := do(t, r, http.MethodPatch, "/api/boards/"+id, map[string]string{"name": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeInvalidInput {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeInvalidInput)
	}

	// 弾いたのだから書かれていない。
	got := decode[apitypes.BoardDetail](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got.Name != "元の名前" {
		t.Errorf("name = %q, want 元の名前", got.Name)
	}
}

func TestRenameBoard_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPatch, "/api/boards/no-such-id",
		map[string]string{"name": "名前"})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// 履歴は畳まずに 1 回ずつ、新しい順で返す（ADR 0007 / 0026）。
func TestListAnnotationRuns(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	for _, title := range []string{"1 回目", "2 回目"} {
		if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
			BoardID:      id,
			AnnotationID: "annot-1",
			ContentHash:  currentHash(t, r, id),
			// 時刻は同じ。並びが時刻に依存していれば、ここで崩れる。
			CreatedAt: fixedTime,
			Outcome:   port.OutcomeComplete,
			Items: []port.SyncItem{{
				ItemID: "PVTI_" + title, Kind: port.KindEpic, Title: title,
				LocalID: "e1", Action: port.ActionCreated, CreatedAt: fixedTime,
			}},
		}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	rec := do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}

	got := decode[[]apitypes.SyncRun](t, rec)
	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2 (%+v)", len(got), got)
	}
	if len(got[0].Items) != 1 || got[0].Items[0].Title != "2 回目" {
		t.Errorf("先頭 = %+v, want 2 回目（新しい順）", got[0].Items)
	}
	if got[1].Items[0].Title != "1 回目" {
		t.Errorf("2 番目 = %+v, want 1 回目", got[1].Items)
	}
	if got[0].ID <= got[1].ID {
		t.Errorf("id = %d, %d, want 降順", got[0].ID, got[1].ID)
	}
}

// 途中で失敗した run は、契約にもそれと分かる形で出る（ADR 0043）。ここが
// 切れると、画面は「作れた件数が少ない run」しか受け取れない（#110）。
func TestListAnnotationRuns_ShowsIncomplete(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	hash := currentHash(t, r, id)
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-1", ContentHash: hash, CreatedAt: fixedTime,
		Outcome: port.OutcomeIncomplete, Error: "github graphql: rate limited",
		Items: []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "作れたほう",
			LocalID: "e1", Action: port.ActionCreated, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	runs := decodeOK[[]apitypes.SyncRun](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil))
	if len(runs) != 1 {
		t.Fatalf("件数 = %d, want 1", len(runs))
	}
	if runs[0].Outcome == nil || *runs[0].Outcome != apitypes.RunOutcomeIncomplete {
		t.Errorf("outcome = %v, want incomplete", runs[0].Outcome)
	}
	if runs[0].Error != "github graphql: rate limited" {
		t.Errorf("error = %q, want GitHub の本文", runs[0].Error)
	}

	// 一覧にも出す。履歴を開かないと気づけないのでは、見せたことにならない。
	// **状態は created のまま**（作れたぶんは記録されている、ADR 0009）。
	states := decodeOK[[]apitypes.AnnotationStatus](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations", nil))
	if len(states) != 1 {
		t.Fatalf("注釈 = %d 件, want 1", len(states))
	}
	if states[0].State != apitypes.SyncStateCreated {
		t.Errorf("state = %v, want created", states[0].State)
	}
	if states[0].LastRunOutcome == nil ||
		*states[0].LastRunOutcome != apitypes.RunOutcomeIncomplete {
		t.Errorf("lastRunOutcome = %v, want incomplete", states[0].LastRunOutcome)
	}
}

// 記録していなかった頃の run は「不明」として出す。**complete に倒さない。**
// 埋めると、列を足す前の途中失敗が成功として残る（ADR 0043）。
func TestListAnnotationRuns_OmitsOutcomeWhenNotRecorded(t *testing.T) {
	t.Parallel()

	r, mappings, db := newRouterWithDB(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	// 移行前の run を再現する。port 経由では書けないので DB に直接入れる。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-1", ContentHash: currentHash(t, r, id),
		CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "古い run",
			LocalID: "e1", Action: port.ActionCreated, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if _, err := db.ExecContext(t.Context(),
		`UPDATE sync_runs SET outcome = NULL`); err != nil {
		t.Fatalf("clear outcome: %v", err)
	}

	runs := decodeOK[[]apitypes.SyncRun](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil))
	if len(runs) != 1 {
		t.Fatalf("件数 = %d, want 1", len(runs))
	}
	if runs[0].Outcome != nil {
		t.Errorf("outcome = %v, want 省略（記録していない）", *runs[0].Outcome)
	}

	states := decodeOK[[]apitypes.AnnotationStatus](t, do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations", nil))
	if len(states) != 1 {
		t.Fatalf("注釈 = %d 件, want 1", len(states))
	}
	if states[0].LastRunOutcome != nil {
		t.Errorf("lastRunOutcome = %v, want 省略", *states[0].LastRunOutcome)
	}
}

func TestListAnnotationRuns_Empty(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	rec := do(t, r, http.MethodGet,
		"/api/boards/"+id+"/annotations/annot-1/runs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	// null ではなく空配列。一覧は常に配列で返す。
	if body := rec.Body.String(); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

func TestListAnnotationRuns_BoardNotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet,
		"/api/boards/no-such-id/annotations/annot-1/runs", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func saveSceneBody(scene string, base any) map[string]any {
	return map[string]any{"scene": scene, "baseUpdatedAt": base}
}

func TestSaveScene(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	scene := `{"type":"excalidraw","elements":[{"id":"t1","type":"text","text":"付箋"}]}`
	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(scene, fixedTime))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
	// 次の保存の基準を返す。返さないと、保存のたびにボードを取り直すことになる。
	if got := decode[map[string]any](t, rec)["updatedAt"]; got == nil || got == "" {
		t.Errorf("updatedAt = %v, want 保存後の版", got)
	}

	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["scene"] != scene {
		t.Errorf("scene = %v", got["scene"])
	}
}

// 共有ボードの後勝ちを拒む。上書きすると相手の作業がまるごと消える（ADR 0020）。
func TestSaveScene_RejectsStaleBase(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	before := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))

	// 開いたときの版がもう古い状態。相手が先に保存したのと同じことになる。
	stale := fixedTime.Add(-time.Hour)
	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(`{"type":"excalidraw","elements":[]}`, stale))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusConflict, rec.Body)
	}
	// 409 には 7 つの原因が同居する。画面が「開き直す」を案内できるよう、
	// どれなのかを code で返す。
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeSceneConflict {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeSceneConflict)
	}

	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["scene"] != before["scene"] {
		t.Errorf("409 を返したのに書き換わっている: %v", got["scene"])
	}
}

// 基準を省いた保存は通さない。通すと、照合を素通りする経路が残る。
func TestSaveScene_RequiresBase(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		map[string]any{"scene": `{"elements":[]}`})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body)
	}
}

// 大きすぎるシーンは 413。**400 に畳まない。** 打ち手が「送った内容を直す」では
// なく「貼った画像を減らす」になるので、画面が言い分けられる必要がある。
func TestSaveScene_RejectsSceneOverTheLimit(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	before := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))

	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(sceneOfSize(t, usecase.MaxSceneBytes+1), fixedTime))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (%s)",
			rec.Code, http.StatusRequestEntityTooLarge, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeSceneTooLarge {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeSceneTooLarge)
	}

	// 弾いたのだから書かれていない。エラーだけを見ていると、書いてから弾く
	// 実装でも緑になる。
	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["scene"] != before["scene"] {
		t.Error("413 を返したのに書き換わっている")
	}
}

// ボディの読み込みにも歯止めがある。**歯止めに当たった側も 413 で返す。**
// 同じ「大きすぎる」が、ボディの大きさしだいで 400 と 413 に割れないようにする。
//
// 歯止めが効いたことそのものはここからは見えない（歯止めが無くても
// validateScene が同じ 413 を返す）。このテストが落ちるのは、歯止めに当たった
// ボディを 400 に写す実装にしたとき。
func TestSaveScene_RejectsOversizedBody(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	// 歯止めは上限の 6 倍に取ってある（エスケープでシーンが膨らむため）。
	// 7 倍を送れば、エスケープの要らない文字で埋めても必ず当たる。
	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(sceneOfSize(t, usecase.MaxSceneBytes*7), fixedTime))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (%s)",
			rec.Code, http.StatusRequestEntityTooLarge, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeSceneTooLarge {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeSceneTooLarge)
	}
}

// 上限ちょうどのシーンは、JSON にすると何倍にも膨らむ文字で埋まっていても通る。
//
// **歯止めはユースケース層の判定より先に切ってはならない。** 先に切れると、
// 上限に収まっているシーンに「貼った画像を減らせ」と返すことになる。`<` は
// `\u003c` の 6 バイトになるので、歯止めを 2 倍に取るとこのテストが落ちる。
func TestSaveScene_AcceptsSceneAtTheLimitThatEscapesLong(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(sceneOfSizeFilledWith(t, usecase.MaxSceneBytes, "<"), fixedTime))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
}

// sceneOfSize は指定したバイト数ちょうどの、読めるシーン JSON を作る。
//
// 実際に大きさを押し上げるのは貼った画像（base64 でシーンに乗る）だが、ここで
// 要るのはバイト数だけなのでテキスト要素の本文で埋める。
func sceneOfSize(t *testing.T, size int) string {
	t.Helper()

	return sceneOfSizeFilledWith(t, size, "a")
}

// sceneOfSizeFilledWith は本文を埋める文字を選べる sceneOfSize。
//
// **JSON にしたときの大きさは、同じバイト数のシーンでも埋めた文字で変わる。**
// ボディの歯止めがそれを織り込めているかを見るために分けてある。
func sceneOfSizeFilledWith(t *testing.T, size int, fill string) string {
	t.Helper()

	const shell = `{"type":"excalidraw","version":2,"source":"etoki",` +
		`"elements":[{"id":"t1","type":"text","text":""}],"appState":{}}`
	if size < len(shell) {
		t.Fatalf("size = %d だが、包みだけで %d バイトある", size, len(shell))
	}

	scene := strings.Replace(shell, `"text":""`,
		`"text":"`+strings.Repeat(fill, size-len(shell))+`"`, 1)
	if len(scene) != size {
		t.Fatalf("len(scene) = %d, want %d", len(scene), size)
	}
	return scene
}

func TestSaveScene_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPut, "/api/boards/no-such-id/scene",
		saveSceneBody(`{"elements":[]}`, fixedTime))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// 注釈のないボードでは空配列を返す。
func TestListAnnotations_Empty(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	rec := do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

func TestListAnnotations_BoardNotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/boards/no-such-id/annotations", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// 一度も issue 化していない注釈は uncreated になる。
func TestListAnnotations_Uncreated(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

	got := decode[[]map[string]any](t,
		do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))

	if len(got) != 1 {
		t.Fatalf("注釈の件数 = %d, want 1 (%+v)", len(got), got)
	}
	if got[0]["id"] != "annot-1" {
		t.Errorf("id = %v", got[0]["id"])
	}
	if got[0]["state"] != "uncreated" {
		t.Errorf("state = %v, want uncreated", got[0]["state"])
	}
	if got[0]["granularity"] != "epic" {
		t.Errorf("granularity = %v, want epic", got[0]["granularity"])
	}
	if _, ok := got[0]["lastSyncedAt"]; ok {
		t.Error("未実行なのに lastSyncedAt が入っている")
	}
	// テンプレートから始めていない注釈には種別が無い。**空文字を載せない。**
	// 載せると DiagramKind の enum に無い値が契約の外から出る。
	if _, ok := got[0]["kind"]; ok {
		t.Errorf("種別を選んでいないのに kind が入っている: %v", got[0]["kind"])
	}
}

// テンプレートから始めた注釈は、選んだ種別が一覧にも出る。
//
// **出ないと画面が種別を見せられない。** 種別はハッシュの入力でもあるので、
// 何の図として解釈されるかを開発者が確かめられる必要がある（中核思想 3）。
func TestListAnnotations_ReturnsKind(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	scene := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり",
		 "customData":{"etoki":{"granularity":"","kind":"sequence"}}},
		{"id":"t1","type":"text","text":"注文する","frameId":"annot-1"}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(scene, fixedTime)); rec.Code != http.StatusOK {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	got := decode[[]map[string]any](t,
		do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))

	if len(got) != 1 {
		t.Fatalf("注釈の件数 = %d, want 1 (%+v)", len(got), got)
	}
	if got[0]["kind"] != "sequence" {
		t.Errorf("kind = %v, want sequence", got[0]["kind"])
	}
}

// 実行記録があってハッシュが一致すれば created、ボードを変えれば changed。
func TestListAnnotations_CreatedThenChanged(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	base := saveAnnotatedScene(t, r, id)

	// 現在のハッシュを API 経由では取れないので、いったん状態を引いてから
	// 同じ内容で run を記録する。ハッシュ自体は domain 側のテストで担保済み。
	states := decode[[]map[string]any](t,
		do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))
	if states[0]["state"] != "uncreated" {
		t.Fatalf("前提が崩れている: %v", states[0])
	}

	hash := currentHash(t, r, id)
	parent := "e1"
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID:      id,
		AnnotationID: "annot-1",
		ContentHash:  hash,
		CreatedAt:    fixedTime,
		Outcome:      port.OutcomeComplete,
		Items: []port.SyncItem{
			{ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API", Body: "決済まわりの入口", LocalID: "e1", Action: port.ActionCreated, CreatedAt: fixedTime},
			{ItemID: "PVTI_i1", Kind: port.KindIssue, Title: "SDK更新", Body: "SDK の更新内容", LocalID: "i1", ParentLocalID: &parent, Action: port.ActionCreated, CreatedAt: fixedTime},
		},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	got := decode[[]map[string]any](t,
		do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))
	if got[0]["state"] != "created" {
		t.Fatalf("state = %v, want created", got[0]["state"])
	}
	if _, ok := got[0]["lastSyncedAt"]; !ok {
		t.Error("lastSyncedAt が返っていない")
	}
	items, _ := got[0]["items"].([]any)
	if len(items) != 2 {
		// 以降で添字を引くので、ここで止める。
		t.Fatalf("items = %d 件, want 2", len(items))
	}
	// 何を作ったのかを追えるのは etoki の記録だけ。逆方向同期は実装しないので
	// GitHub からは取り直せない（ADR 0023）。epic と issue の両方を見る。
	for i, want := range []string{"決済まわりの入口", "SDK の更新内容"} {
		it, _ := items[i].(map[string]any)
		if it["body"] != want {
			t.Errorf("items[%d].body = %v, want %v", i, it["body"], want)
		}
	}

	// 付箋の文言を変えると changed になる。基準は 1 回目の保存が返した版。
	// 時計は止めてあるが版は進んでいるので、fixedTime を送り直すと 409 になる。
	changed := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"epic"}}},
		{"id":"t1","type":"text","text":"Stripe の SDK が古い（急ぎ）","frameId":"annot-1"}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(changed, base)); rec.Code != http.StatusOK {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	got = decode[[]map[string]any](t,
		do(t, r, http.MethodGet, "/api/boards/"+id+"/annotations", nil))
	if got[0]["state"] != "changed" {
		t.Errorf("state = %v, want changed", got[0]["state"])
	}
	// 「変更あり」でも前回の作成物は見せ続ける。開発者が判断するのに要る。
	if items, _ := got[0]["items"].([]any); len(items) != 2 {
		t.Errorf("changed でも前回の items を返すべき: %d 件", len(items))
	}
}

// --- ヘルパー ---

func createBoard(t *testing.T, r *gin.Engine, name string) string {
	t.Helper()

	rec := do(t, r, http.MethodPost, "/api/boards", newBoardBody(name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create board: %d %s", rec.Code, rec.Body)
	}

	id, _ := decode[map[string]any](t, rec)["id"].(string)
	if id == "" {
		t.Fatal("board id が空")
	}
	return id
}

const annotatedScene = `{"type":"excalidraw","elements":[
	{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"epic"}}},
	{"id":"t1","type":"text","text":"Stripe の SDK が古い","frameId":"annot-1"}]}`

// saveAnnotatedScene は注釈つきのシーンを保存し、保存後の版を返す。
//
// **返った版を次の保存に使う。** テストの時計は fixedTime に固定してあるので、
// 続けて保存すると同じ時刻の読みが 2 回続く。それでも版は進むので（ADR 0020）、
// 基準を送り直さないと 409 になる。実際のクライアントと同じ経路。
func saveAnnotatedScene(t *testing.T, r *gin.Engine, boardID string) any {
	t.Helper()

	// 作った直後の版は fixedTime。ボードの作成もこの時計を使っている。
	rec := do(t, r, http.MethodPut, "/api/boards/"+boardID+"/scene",
		saveSceneBody(annotatedScene, fixedTime))
	if rec.Code != http.StatusOK {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}

	return decode[map[string]any](t, rec)["updatedAt"]
}

// currentHash は保存済みシーンから現在のハッシュを求める。
// API は状態しか返さないため、テスト側で domain のロジックを通す。
func currentHash(t *testing.T, r *gin.Engine, boardID string) string {
	t.Helper()

	board := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+boardID, nil))
	scene, _ := board["scene"].(string)

	parsed, err := domain.ParseScene([]byte(scene))
	if err != nil {
		t.Fatalf("ParseScene: %v", err)
	}

	annotations := parsed.Annotations()
	if len(annotations) == 0 {
		t.Fatal("注釈が見つからない")
	}

	return string(parsed.AnnotationHash(annotations[0]))
}

// newRepos はマイグレーション済みの一時 DB に紐づくリポジトリを返す。
func newRepos(t *testing.T) (port.BoardRepository, port.MappingRepository) {
	t.Helper()

	db := openTempDB(t)
	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return sqlite.NewBoardRepository(db), sqlite.NewMappingRepository(db)
}

// newUnmigratedRepos はマイグレーションしていない DB に紐づくリポジトリを返す。
// 未初期化のまま起動してしまった状況を再現するために使う。
func newUnmigratedRepos(t *testing.T) (port.BoardRepository, port.MappingRepository) {
	t.Helper()

	db := openTempDB(t)

	return sqlite.NewBoardRepository(db), sqlite.NewMappingRepository(db)
}

func openTempDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// newLimiter は既定の上限（同時実行 1・回数は無制限）の limiter を返す。
//
// ハンドラのテストはリクエストを逐次に投げるので、同時実行 1 には当たらない。
// 上限そのものの挙動はユースケース層のテストが見る。
func newLimiter() *usecase.LLMLimiter {
	return usecase.NewLLMLimiter(usecase.LLMLimits{})
}
