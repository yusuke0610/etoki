package httpapi_test

import (
	"net/http"
	"testing"
)

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
