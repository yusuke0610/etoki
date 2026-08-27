package httpapi_test

import (
	"net/http"
	"reflect"
	"testing"
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
