package httpapi_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// ---------------------------------------------------------------------------
// 貼った画像（ADR 0074、#102）
// ---------------------------------------------------------------------------

// sceneWithImage は fileID の画像を 1 枚貼った、画像の実体を持たないシーン。
func sceneWithImage(fileID string) string {
	return `{"type":"excalidraw","elements":[` +
		`{"id":"img-1","type":"image","fileId":"` + fileID + `"}],"files":{}}`
}

// imageFile は id を名乗る Excalidraw の画像データ。
func imageFile(id, content string) string {
	return `{"id":"` + id + `","mimeType":"image/png","dataURL":"data:image/png;base64,` +
		content + `"}`
}

// 送った画像は開く口で返り、2 回目の保存で送らなくても残る。保存の応答は
// 持っている画像の ID を返す。
func TestSaveScene_StoresFiles(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	data := imageFile("file-1", "AAAA")
	first := decodeOK[apitypes.SaveSceneResponse](t, do(t, r, http.MethodPut,
		"/api/boards/"+id+"/scene", map[string]any{
			"scene":         sceneWithImage("file-1"),
			"files":         map[string]string{"file-1": data},
			"baseUpdatedAt": fixedTime,
		}))
	if want := []string{"file-1"}; !slices.Equal(first.FileIds, want) {
		t.Errorf("fileIds = %v, want %v", first.FileIds, want)
	}

	// 図形を動かしただけの保存。画像は送らない。
	second := decodeOK[apitypes.SaveSceneResponse](t, do(t, r, http.MethodPut,
		"/api/boards/"+id+"/scene", saveSceneBody(sceneWithImage("file-1"), first.UpdatedAt)))
	if want := []string{"file-1"}; !slices.Equal(second.FileIds, want) {
		t.Errorf("2 回目の fileIds = %v, want %v", second.FileIds, want)
	}

	got := decodeOK[apitypes.BoardWithFiles](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if want := map[string]string{"file-1": data}; !reflect.DeepEqual(got.Files, want) {
		t.Errorf("files = %v, want %v", got.Files, want)
	}
	if got.Scene != sceneWithImage("file-1") {
		t.Errorf("scene = %s", got.Scene)
	}
}

// 画像の無いボードは files を空のオブジェクトで返す。null にすると契約の
// 「ID → 画像」が読めない。fileIds も同じで、空の配列で返す。
func TestBoardFiles_EmptyIsNotNull(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	opened := decodeOK[map[string]json.RawMessage](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got := string(opened["files"]); got != "{}" {
		t.Errorf("files = %s, want {}", got)
	}

	saved := decodeOK[map[string]json.RawMessage](t, do(t, r, http.MethodPut,
		"/api/boards/"+id+"/scene", saveSceneBody(`{"type":"excalidraw","elements":[]}`, fixedTime)))
	if got := string(saved["fileIds"]); got != "[]" {
		t.Errorf("fileIds = %s, want []", got)
	}
}

// 画像を返すのは開く口だけ。改名や作成先の設定の応答は画像を運ばない。
//
// 運ぶと、名前を直しただけで画像を全部送り返すことになる（#102 で分けた理由の
// 1 つ）。
func TestBoardDetailResponses_DoNotCarryFiles(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	decodeOK[apitypes.SaveSceneResponse](t, do(t, r, http.MethodPut,
		"/api/boards/"+id+"/scene", map[string]any{
			"scene":         sceneWithImage("file-1"),
			"files":         map[string]string{"file-1": imageFile("file-1", "AAAA")},
			"baseUpdatedAt": fixedTime,
		}))

	responses := map[string]*map[string]json.RawMessage{
		"改名": ptr(decodeOK[map[string]json.RawMessage](t, do(t, r, http.MethodPatch,
			"/api/boards/"+id, map[string]string{"name": "新しい名前"}))),
		"作成先の設定": ptr(decodeOK[map[string]json.RawMessage](t, do(t, r, http.MethodPut,
			"/api/boards/"+id+"/target", map[string]any{
				"repositoryOwner": "acme", "repositoryName": "api", "projectId": "PVT_2",
			}))),
	}
	for name, body := range responses {
		if _, ok := (*body)["files"]; ok {
			t.Errorf("%s の応答に files が載っている", name)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// 開く口は、改名の応答（BoardDetail）が持つものをすべて同じ値で持つ。
//
// **写し漏れを落とす。** BoardWithFiles は生成後に平坦な struct になるので、
// toBoardWithFiles が BoardDetail のフィールドを 1 つ写し忘れても、ゼロ値のまま
// コンパイルが通る。真偽値はゼロ値（false）のままだと写し漏れと区別できないので、
// 作成先を固定し、上限も超えさせて true にしてから比べる。
func TestGetBoard_CarriesEveryDetailField(t *testing.T) {
	t.Parallel()

	r, mappings, db := newRouterWithDB(t)
	id := createBoard(t, r, "ボード")

	// targetLocked を true にする。run が 1 つあれば固定される（ADR 0014）。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-1", ContentHash: "h", CreatedAt: fixedTime,
		Outcome: port.OutcomeComplete,
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	// sceneOverLimit を true にする。API からは上限を超えるシーンを保存できない
	// ので、上限を入れる前の行と同じ形で DB に直接書く。
	if _, err := db.ExecContext(t.Context(), `UPDATE boards SET scene = ? WHERE id = ?`,
		strings.Repeat("x", usecase.MaxSceneBytes+1), id); err != nil {
		t.Fatalf("update scene: %v", err)
	}

	detail := decodeOK[map[string]any](t, do(t, r, http.MethodPatch,
		"/api/boards/"+id, map[string]string{"name": "名前"}))
	opened := decodeOK[map[string]any](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))

	for key, want := range detail {
		if isZeroJSON(want) && key != "projectNumber" && key != "projectTitle" && key != "projectUrl" {
			t.Errorf("detail の %s がゼロ値。写し漏れを見逃すので値を入れること", key)
			continue
		}
		got, ok := opened[key]
		if !ok {
			t.Errorf("開く口の応答に %s が無い。toBoardWithFiles が写していない", key)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

// 画像の合計が上限を超えていたら、開く口も改名の応答も sceneOverLimit を
// 立てる（ADR 0048 / 0074）。上限を引き下げたあとのボードに当たる。
//
// API からは上限を超えて保存できないので、行を DB に直接書く。
func TestBoardResponses_ReportFilesOverLimit(t *testing.T) {
	t.Parallel()

	r, _, db := newRouterWithDB(t)
	id := createBoard(t, r, "ボード")

	data := imageFile("file-1", strings.Repeat("A", usecase.MaxBoardFileBytes))
	if _, err := db.ExecContext(t.Context(),
		`INSERT INTO board_files (board_id, id, bytes, data) VALUES (?, 'file-1', ?, ?)`,
		id, len(data), data); err != nil {
		t.Fatalf("insert board_files: %v", err)
	}

	opened := decodeOK[apitypes.BoardWithFiles](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if !opened.SceneOverLimit {
		t.Error("開く口の sceneOverLimit = false, want true")
	}

	renamed := decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPatch,
		"/api/boards/"+id, map[string]string{"name": "名前"}))
	if !renamed.SceneOverLimit {
		t.Error("改名の応答の sceneOverLimit = false, want true")
	}
}

// 画像を受け取れない形は 400（code は invalid_input）。中身の誤りなので、
// 大きさの 413 とは分ける。
func TestSaveScene_RejectsUnreferencedFile(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene", map[string]any{
		"scene":         `{"type":"excalidraw","elements":[]}`,
		"files":         map[string]string{"file-1": imageFile("file-1", "AAAA")},
		"baseUpdatedAt": fixedTime,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusBadRequest, rec.Body)
	}
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodeInvalidInput {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodeInvalidInput)
	}

	// 弾いたのだから何も書いていない。エラーだけを見ていると、書いてから弾く
	// 実装でも緑になる。
	got := decodeOK[apitypes.BoardWithFiles](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if len(got.Files) != 0 {
		t.Errorf("400 を返したのに画像が残っている: %v", got.Files)
	}
}
