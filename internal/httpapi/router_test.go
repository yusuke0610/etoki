package httpapi_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/httpapi"
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

	boards, mappings := newRepos(t)

	seq := 0
	boardSvc := usecase.NewBoardService(boards, mappings,
		usecase.WithClock(func() time.Time { return fixedTime }),
		usecase.WithIDGenerator(func() string {
			seq++
			return "board-" + string(rune('0'+seq))
		}),
	)

	return httpapi.NewRouter(httpapi.Deps{
		Boards:      boardSvc,
		Annotations: usecase.NewAnnotationService(boards, mappings),
	}), mappings
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

	rec := do(t, r, http.MethodPost, "/api/boards", map[string]string{"name": "決済まわり"})
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

	rec := do(t, r, http.MethodPost, "/api/boards", map[string]string{"name": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// 壊れたシーンは入口で弾く。保存できてしまうとボードごと開けなくなる。
func TestCreateBoard_RejectsBrokenScene(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPost, "/api/boards",
		map[string]string{"name": "壊れたボード", "scene": "{"})
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

func TestSaveScene(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	scene := `{"type":"excalidraw","elements":[{"id":"t1","type":"text","text":"付箋"}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		map[string]string{"scene": scene}); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNoContent, rec.Body)
	}

	got := decode[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got["scene"] != scene {
		t.Errorf("scene = %v", got["scene"])
	}
}

func TestSaveScene_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPut, "/api/boards/no-such-id/scene",
		map[string]string{"scene": `{"elements":[]}`})
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
}

// 実行記録があってハッシュが一致すれば created、ボードを変えれば changed。
func TestListAnnotations_CreatedThenChanged(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "ボード")
	saveAnnotatedScene(t, r, id)

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
		Items: []port.SyncItem{
			{ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API", LocalID: "e1", CreatedAt: fixedTime},
			{ItemID: "PVTI_i1", Kind: port.KindIssue, Title: "SDK更新", LocalID: "i1", ParentLocalID: &parent, CreatedAt: fixedTime},
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
		t.Errorf("items = %d 件, want 2", len(items))
	}

	// 付箋の文言を変えると changed になる。
	changed := `{"type":"excalidraw","elements":[
		{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":"epic"}}},
		{"id":"t1","type":"text","text":"Stripe の SDK が古い（急ぎ）","frameId":"annot-1"}]}`
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		map[string]string{"scene": changed}); rec.Code != http.StatusNoContent {
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

	rec := do(t, r, http.MethodPost, "/api/boards", map[string]string{"name": name})
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

func saveAnnotatedScene(t *testing.T, r *gin.Engine, boardID string) {
	t.Helper()

	rec := do(t, r, http.MethodPut, "/api/boards/"+boardID+"/scene",
		map[string]string{"scene": annotatedScene})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("save scene: %d %s", rec.Code, rec.Body)
	}
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
