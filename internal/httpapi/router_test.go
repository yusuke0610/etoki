package httpapi_test

import (
	"bytes"
	"context"
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

	return doWithContext(t, t.Context(), r, method, path, body)
}

// doWithContext はリクエストの ctx を渡せる do。途中で接続が切れた経路を
// 再現するときだけ使う。
func doWithContext(
	t *testing.T, ctx context.Context, r *gin.Engine, method, path string, body any,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequestWithContext(ctx, method, path, reader)
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

// 認証済み API の応答はキャッシュされない（CWE-525）。同じブラウザ
// プロファイルで利用者を切り替えても、前の利用者の draft issue の内容が
// 再利用されない。
func TestAPIResponses_AreNotCached(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/boards", nil)
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

// `/api` の入口で本文の読み込みに既定の上限が掛かる（issue #147）。
//
// **口ごとに置かず入口で掛ける。** シーン・解釈・図のドラフトに歯止めを入れた
// あとも、改名・作成先・作成・招待の 6 つは素通しのままだった。
//
// **歯止めに当たった失敗も契約の code に写す**（`.claude/rules/api-contract.md`）。
// 400 に落とすと、同じ「大きすぎる」がボディの大きさしだいで 400 と 413 に割れ、
// 画面が同じ原因を 2 通りに案内することになる。
//
// 共有は、設定されていないと本文を読む前に 503 で返るので、そちらのルーターを
// 持つ member_test.go に置いてある。作成は既定ではなく項目の上限から導いた
// 上限で読む（create_test.go）。
func TestAPIBodies_AreLimitedAtTheEntrance(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "設計会")

	// 既定の上限（64 KiB）を確実に超える値。**contentHash のような 1 フィールドに
	// 載せる。** 項目数で超えさせると、件数の上限（domain.MaxItems）のほうに
	// 先に当たり、歯止めを見ていない実装でも 4xx になる。
	huge := strings.Repeat("あ", 64<<10)

	cases := map[string]struct {
		method string
		path   string
		body   map[string]any
	}{
		"改名": {http.MethodPatch, "/api/boards/" + id,
			map[string]any{"name": huge}},
		"作成先": {http.MethodPut, "/api/boards/" + id + "/target",
			map[string]any{"repositoryOwner": huge, "repositoryName": "web", "projectId": "PVT_1"}},
		"作成先の表示": {http.MethodPut, "/api/boards/" + id + "/target/display",
			map[string]any{"projectId": "PVT_1", "projectTitle": huge}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, r, tc.method, tc.path, tc.body)
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d (%s)",
					rec.Code, http.StatusRequestEntityTooLarge, rec.Body)
			}
			if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeRequestTooLarge {
				t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeRequestTooLarge)
			}
		})
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	if rec := do(t, r, http.MethodGet, "/no-such-route", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func saveSceneBody(scene string, base any) map[string]any {
	return map[string]any{"scene": scene, "baseUpdatedAt": base}
}

// sceneOfSize は指定したバイト数ちょうどの、読めるシーン JSON を作る。
//
// 貼った画像はシーンに乗らない（ADR 0074）ので、大きさはテキスト要素の本文で
// 埋める。
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

// annotationsBody は注釈の一覧の応答。
//
// **シーンに在るものと、消えたのに GitHub 側には残っているものは別のリスト**
// （#111）。混ぜて数えると、frame を消したボードで件数が合わなくなる。
type annotationsBody struct {
	Annotations []map[string]any `json:"annotations"`
	Detached    []map[string]any `json:"detached"`
}

// listAnnotations は注釈の一覧を引く。200 でなければその場で止める。
func listAnnotations(t *testing.T, r *gin.Engine, boardID string) annotationsBody {
	t.Helper()

	rec := do(t, r, http.MethodGet, "/api/boards/"+boardID+"/annotations", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
	return decode[annotationsBody](t, rec)
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
