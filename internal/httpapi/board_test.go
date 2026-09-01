package httpapi_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
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
	if got := decode[apitypes.BoardDeletion](t, rec); got.RecordedItemCount != 0 {
		t.Fatalf("作成前の recordedItemCount = %d, want 0", got.RecordedItemCount)
	}

	for _, action := range []port.SyncAction{port.ActionCreated, port.ActionUpdated} {
		items := []port.SyncItem{{
			ItemID: "PVTI_e1", Kind: port.KindEpic, Title: "決済API",
			LocalID: "e1", Action: action, CreatedAt: fixedTime,
		}}
		if action == port.ActionCreated {
			items = append(items, port.SyncItem{
				ItemID: "PVTI_i1", Kind: port.KindIssue, Title: "SDK更新",
				LocalID: "i1", Action: action, CreatedAt: fixedTime,
			})
		}
		if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
			BoardID: id, AnnotationID: "annot-1", ContentHash: "hash-1",
			CreatedAt: fixedTime, Items: items,
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
}

func TestGetBoardDeletion_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodGet, "/api/boards/no-such-board/deletion", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusNotFound, rec.Body)
	}
}
