package httpapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/version"
)

// /healthz は、生きていることと動いている版を返す（ADR 0099）。版は
// internal/version と同じ値で、別の出どころから組まない。
func TestHealthz_ReturnsStatusAndVersion(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	rec := do(t, r, http.MethodGet, "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var got apitypes.HealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := apitypes.HealthResponse{Status: "ok", Version: version.String()}
	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}
