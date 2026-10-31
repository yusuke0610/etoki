package httpapi_test

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// TestToDetail_CarriesEverySummaryField は BoardDetail が BoardSummary の
// フィールドを 1 つ残らず同じ値で載せていることを固定する。
//
// **回帰止め。切れると何が起きるか。** 契約の allOf は生成後に平坦な struct に
// なるので、BoardSummary と BoardDetail は Go の型としては無関係になる
// （ADR 0011）。詰め替えを 2 度書くと、契約にフィールドを 1 つ足したとき
// 片方だけに足すことができ、**生成型なのでコンパイルは通る。** 落ちるのは
// 実行時で、詳細だけがゼロ値を返す。
//
// 名前で突き合わせているので、toSummary にだけ足しても toDetail にだけ
// 足しても落ちる。
func TestToDetail_CarriesEverySummaryField(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	// **全フィールドを非ゼロで作る。** ゼロ値のまま比べると、写し忘れた
	// フィールドも「どちらもゼロ値」で一致してしまい、このテストが素通りする。
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

	id, ok := decode[map[string]any](t, rec)["id"].(string)
	if !ok {
		t.Fatalf("作成の応答に id が無い: %s", rec.Body)
	}

	// **decode の前に応答コードを見る。** エラー本文（ErrorResponse）も
	// map[string]any に decode できてしまうので、確かめずに進むと 403 や 500 が
	// 「toDetail が写していない」という見当違いの失敗になる。
	listRec := do(t, r, http.MethodGet, "/api/boards", nil)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d (%s)", listRec.Code, http.StatusOK, listRec.Body)
	}
	list := decode[[]map[string]any](t, listRec)
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	summary := list[0]

	detailRec := do(t, r, http.MethodGet, "/api/boards/"+id, nil)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d, want %d (%s)", detailRec.Code, http.StatusOK, detailRec.Body)
	}
	detail := decode[map[string]any](t, detailRec)

	for key, want := range summary {
		// 件数は一覧にしか載せない（`BoardListEntry`）。詳細には無いのが正しい。
		if key == "annotationCounts" {
			continue
		}
		if isZeroJSON(want) {
			t.Errorf("summary の %s がゼロ値。写し漏れを見逃すので、"+
				"作成のボディで値を入れること", key)
			continue
		}

		got, ok := detail[key]
		if !ok {
			t.Errorf("detail に %s が無い。toDetail が写していない", key)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: detail = %v, summary = %v", key, got, want)
		}
	}
}

// 一覧は注釈の 3 状態の件数を載せる（#200）。**シーンを読めないボードは null**
// で返し、一覧全体は失敗させない。
//
// 0 件（注釈なし）と null（読めなかった）を取り違えると、画面は壊れたボードを
// 「注釈なし」と見せる。**null はキーごと落とさずに返す**ことまで見る。契約で
// required にしてあり、落とすと画面は「書き忘れ」と区別できない。
func TestListBoards_CarriesAnnotationCounts(t *testing.T) {
	t.Parallel()

	r, mappings, db := newRouterWithDB(t)

	created := createBoard(t, r, "作成済み")
	saveAnnotatedScene(t, r, created)
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: created, AnnotationID: "annot-1", ContentHash: currentHash(t, r, created),
		CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API",
			LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	uncreated := createBoard(t, r, "未作成")
	saveAnnotatedScene(t, r, uncreated)

	// API 経由では壊れたシーンを保存できないので、DB に直接書く。
	broken := createBoard(t, r, "壊れたシーン")
	if _, err := db.ExecContext(t.Context(),
		`UPDATE boards SET scene = ? WHERE id = ?`, "{", broken); err != nil {
		t.Fatalf("update scene: %v", err)
	}

	list := decodeOK[[]map[string]any](t, do(t, r, http.MethodGet, "/api/boards", nil))
	if len(list) != 3 {
		t.Fatalf("件数 = %d, want 3（壊れたボードも一覧には残す）", len(list))
	}

	want := map[string]any{
		created:   map[string]any{"uncreated": float64(0), "created": float64(1), "changed": float64(0)},
		uncreated: map[string]any{"uncreated": float64(1), "created": float64(0), "changed": float64(0)},
		broken:    nil,
	}
	for _, e := range list {
		id, _ := e["id"].(string)
		got, ok := e["annotationCounts"]
		if !ok {
			t.Errorf("%s: annotationCounts のキーが無い。null でもキーは返す", id)
			continue
		}
		if !reflect.DeepEqual(got, want[id]) {
			t.Errorf("%s: annotationCounts = %v, want %v", id, got, want[id])
		}
	}
}

// isZeroJSON は JSON から戻した値が「入っていない」形かを見る。
func isZeroJSON(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case float64:
		return t == 0
	case bool:
		return !t
	default:
		return false
	}
}

// 削除は 204 を返し、そのボードは以後 404 になる。
//
// **消えたことまで見る。** 204 だけを見ると、何も書かずに 204 を返す実装でも
// 通る。
func TestDeleteBoard(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "打ち間違えたボード")
	other := createBoard(t, r, "残すボード")

	rec := do(t, r, http.MethodDelete, "/api/boards/"+id, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNoContent, rec.Body)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 に本文が載っている: %s", rec.Body)
	}

	getRec := do(t, r, http.MethodGet, "/api/boards/"+id, nil)
	if getRec.Code != http.StatusNotFound {
		t.Errorf("削除後の取得 = %d, want %d (%s)", getRec.Code, http.StatusNotFound, getRec.Body)
	}

	// 残したほうまで消えていないことも見る。絞りを書き損なうと全部消える。
	if code := do(t, r, http.MethodGet, "/api/boards/"+other, nil).Code; code != http.StatusOK {
		t.Errorf("残すはずのボード = %d, want %d", code, http.StatusOK)
	}
}

func TestDeleteBoard_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodDelete, "/api/boards/no-such-board", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNotFound, rec.Body)
	}
}

// 削除の前に、GitHub 側に残るものの件数を引ける（ADR 0042）。
//
// **数え方は注釈のカードに出している畳み込みと同じ**（ADR 0026）。同じ item を
// 書き換えた run は 1 件に吸収される。ここが run ごとの合計に変わると、画面が
// 同じボードについて 2 つの数を出すことになる。
func TestGetBoardDeletion(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := createBoard(t, r, "作成済みのボード")

	// まだ何も作っていないうちは 0 件。
	rec := do(t, r, http.MethodGet, "/api/boards/"+id+"/deletion", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
	if got := decode[apitypes.BoardDeletion](t, rec); got.RecordedItemCount != 0 ||
		got.UnconfirmedWriteCount != 0 {
		t.Fatalf("作成前の deletion = %+v, want どちらも 0", got)
	}

	for _, action := range []port.SyncAction{port.ActionCreated, port.ActionUpdated} {
		items := []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API",
			LocalID: "e1", Action: action, Confirmed: true, CreatedAt: fixedTime,
		}}
		if action == port.ActionCreated {
			items = append(items, port.SyncItem{
				ItemID: "PVTI_i1", Kind: port.KindIssue, Title: "SDK更新",
				LocalID: "i1", Action: action, Confirmed: true, CreatedAt: fixedTime,
			})
		}
		if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
			BoardID: id, AnnotationID: "annot-1", ContentHash: "hash-1",
			CreatedAt: fixedTime, Outcome: port.OutcomeComplete, Items: items,
		}); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	rec = do(t, r, http.MethodGet, "/api/boards/"+id+"/deletion", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
	// run は 2 件、items は延べ 3 件だが、畳み込むと 2 件。
	if got := decode[apitypes.BoardDeletion](t, rec); got.RecordedItemCount != 2 {
		t.Errorf("recordedItemCount = %d, want 2", got.RecordedItemCount)
	}

	// 応答を失った作成は、記録の件数に混ぜずに別に数える（ADR 0042 の追記）。
	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-2", ContentHash: "hash-2",
		CreatedAt: fixedTime, Outcome: port.OutcomeIncomplete, Error: "Post ...: EOF",
		Items: []port.SyncItem{{
			Kind: port.KindIssue, Title: "応答を失った作成", LocalID: "i1",
			Action: port.ActionCreated, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	rec = do(t, r, http.MethodGet, "/api/boards/"+id+"/deletion", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
	got := decode[apitypes.BoardDeletion](t, rec)
	if got.RecordedItemCount != 2 || got.UnconfirmedWriteCount != 1 {
		t.Errorf("deletion = %+v, want recordedItemCount 2 / unconfirmedWriteCount 1", got)
	}
}

func TestGetBoardDeletion_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/boards/no-such-board/deletion", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNotFound, rec.Body)
	}
}

// 上限以内のボードは sceneOverLimit: false を返す。
func TestGetBoard_SceneWithinLimitIsNotOverLimit(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	got := decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got.SceneOverLimit {
		t.Error("SceneOverLimit = true, want false")
	}
}

// 上限を導入する前に保存された(と想定する)大きいシーンは、開いた時点で
// sceneOverLimit: true を返す（issue #103）。API 経由では上限を超えるシーンを
// 保存できない（ADR 0038）ので、既存行を再現するのと同じ形で DB に直接書く。
func TestGetBoard_ReportsSceneOverLimit(t *testing.T) {
	t.Parallel()

	r, _, db := newRouterWithDB(t)
	id := createBoard(t, r, "ボード")

	overLimitScene := strings.Repeat("x", usecase.MaxSceneBytes+1)
	if _, err := db.ExecContext(t.Context(),
		`UPDATE boards SET scene = ? WHERE id = ?`, overLimitScene, id); err != nil {
		t.Fatalf("update scene: %v", err)
	}

	got := decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if !got.SceneOverLimit {
		t.Error("SceneOverLimit = false, want true")
	}
}

// 既定の上限は、広い本文を受ける口では置き換わる（issue #147）。
//
// **これが無いと、入口の歯止めがシーンの保存を巻き込んでいても緑のまま通る。**
// http.MaxBytesReader は重ねると内側が先に切るので、置き換えではなく重ねる形に
// 戻すとここが落ちる。
func TestSceneBody_IsNotCutByTheDefaultLimit(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "設計会")

	// 既定（64 KiB）を超えるが、シーンの上限には収まる大きさ。
	scene := `{"type":"excalidraw","version":2,"source":"test","elements":[],` +
		`"appState":{"note":"` + strings.Repeat("x", 128<<10) + `"},"files":{}}`

	rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		map[string]any{"scene": scene, "baseUpdatedAt": fixedTime})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
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
//
// **作成の口で当てる。** 写し替えは作成と保存で共有している（bindSceneBody）。
// 保存の歯止めは貼った画像のぶんまで広げてあり（maxSaveBody、ADR 0074）、
// 当てるには数百 MiB を送ることになる。
func TestCreateBoard_RejectsOversizedBody(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	// 歯止めは上限の 6 倍に取ってある（エスケープでシーンが膨らむため）。
	// 7 倍を送れば、エスケープの要らない文字で埋めても必ず当たる。
	rec := do(t, r, http.MethodPost, "/api/boards",
		withScene(newBoardBody("ボード"), sceneOfSize(t, usecase.MaxSceneBytes*7)))
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

func TestSaveScene_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPut, "/api/boards/no-such-id/scene",
		saveSceneBody(`{"elements":[]}`, fixedTime))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
