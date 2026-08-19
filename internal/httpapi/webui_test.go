package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/usecase"
)

const (
	indexHTML  = "<!doctype html><html lang=\"ja\"><body><div id=\"root\"></div></body></html>\n"
	assetJS    = "console.log(\"etoki\");\n"
	secretText = "ETOKI_TOKEN_ENCRYPTION_KEY=do-not-serve-this\n"
)

// newWebDir はビルド済みフロントエンドを真似た一時ディレクトリを作る。
//
// 配る対象の**外側**にもファイルを 1 つ置く。dir の外に出られないことを
// 確かめる相手が要るため。
func newWebDir(t *testing.T) string {
	t.Helper()

	base := t.TempDir()
	dir := filepath.Join(base, "dist")

	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, "index.html"), indexHTML)
	writeFile(t, filepath.Join(dir, "assets", "app.js"), assetJS)
	writeFile(t, filepath.Join(base, "secret.txt"), secretText)

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// newWebRouter は画面を配る設定のルーターを返す。dir が空なら配らない設定。
func newWebRouter(t *testing.T, dir string) *gin.Engine {
	t.Helper()

	boards, mappings := newRepos(t)

	return httpapi.NewRouter(httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks()),
		Annotations: usecase.NewAnnotationService(boards, mappings),
		WebDir:      dir,
	})
}

func TestWebUI_ServesIndexAtRoot(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))
	rec := do(t, r, http.MethodGet, "/", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != indexHTML {
		t.Errorf("body = %q, want %q", rec.Body.String(), indexHTML)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	// index.html が指す asset 名はビルドごとに変わる。キャッシュされると、
	// ビルドを入れ替えても古い asset を指したまま動く。
	if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want %q", cc, "no-cache")
	}
}

func TestWebUI_ServesAssets(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))
	rec := do(t, r, http.MethodGet, "/assets/app.js", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != assetJS {
		t.Errorf("body = %q, want %q", rec.Body.String(), assetJS)
	}
	// ファイル名にハッシュが入る側なので、こちらは止めない。
	if cc := rec.Header().Get("Cache-Control"); cc != "" {
		t.Errorf("Cache-Control = %q, want empty", cc)
	}
}

// これが切れると、API の打ち間違いが 404 ではなく 200 + HTML で返る。フロント
// 側では「JSON のはずが HTML」という読みにくい失敗になる（ADR 0032）。
func TestWebUI_DoesNotServeHTMLForAPIPaths(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))

	for _, path := range []string{"/api", "/api/no-such-endpoint", "/api/boards/x/no-such"} {
		rec := do(t, r, http.MethodGet, path, nil)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s: Content-Type = %q, want application/json", path, ct)
		}
		if strings.Contains(rec.Body.String(), "<!doctype") {
			t.Errorf("%s: API のパスに画面を返している: %q", path, rec.Body.String())
		}
	}
}

// /healthz は配信より先にルートが当たる。覆われると、生存確認が HTML を返す。
func TestWebUI_KeepsHealthz(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))
	rec := do(t, r, http.MethodGet, "/healthz", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := decode[map[string]string](t, rec)["status"]; got != "ok" {
		t.Errorf("status = %q, want %q", got, "ok")
	}
}

// SPA の fallback は持たない。フロントに client-side routing が無いので、
// 実在しないパスは 404 のままにする（ADR 0032）。
func TestWebUI_UnknownPathIsNotFound(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))
	rec := do(t, r, http.MethodGet, "/no-such-page", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	if strings.Contains(rec.Body.String(), "<!doctype") {
		t.Errorf("実在しないパスに画面を返している: %q", rec.Body.String())
	}
}

func TestWebUI_DoesNotEscapeWebDir(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))

	paths := []string{
		"/../secret.txt",
		"/assets/../../secret.txt",
		"/%2e%2e/secret.txt",
		"/assets/%2e%2e/%2e%2e/secret.txt",
	}
	for _, path := range paths {
		rec := do(t, r, http.MethodGet, path, nil)

		if rec.Code == http.StatusOK {
			t.Errorf("%s: status = %d, want an error", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "do-not-serve-this") {
			t.Errorf("%s: 配る対象の外を返している: %q", path, rec.Body.String())
		}
	}
}

// ディレクトリは配らない。一覧を出すと、置いてあるものが読み取られる。
func TestWebUI_DoesNotListDirectories(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))

	for _, path := range []string{"/assets", "/assets/"} {
		rec := do(t, r, http.MethodGet, path, nil)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", path, rec.Code, http.StatusNotFound)
		}
		if strings.Contains(rec.Body.String(), "app.js") {
			t.Errorf("%s: 一覧を返している: %q", path, rec.Body.String())
		}
	}
}

// 書き込みのつもりで外したパスに、ページを返さない。
func TestWebUI_IgnoresWriteMethods(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, newWebDir(t))

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := do(t, r, method, "/", nil)

		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", method, rec.Code, http.StatusNotFound)
		}
		if strings.Contains(rec.Body.String(), "<!doctype") {
			t.Errorf("%s: 画面を返している: %q", method, rec.Body.String())
		}
	}
}

// 未設定なら配らない。make dev では Vite が同じものを持っているため。
func TestWebUI_NotConfiguredServesNothing(t *testing.T) {
	t.Parallel()

	r := newWebRouter(t, "")

	rec := do(t, r, http.MethodGet, "/", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	// 配っているかどうかで 404 の形が変わると、画面側の失敗の見え方が
	// 構成によって変わる。
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestCheckWebDir(t *testing.T) {
	t.Parallel()

	if err := httpapi.CheckWebDir(newWebDir(t)); err != nil {
		t.Errorf("CheckWebDir() = %v, want nil", err)
	}

	// bun run build の前に起動した場合がここに来る。実行時の 404 に
	// 持ち越さず、起動時に落とす。
	empty := t.TempDir()
	if err := httpapi.CheckWebDir(empty); err == nil {
		t.Error("CheckWebDir() = nil, want error")
	}
	if err := httpapi.CheckWebDir(filepath.Join(empty, "missing")); err == nil {
		t.Error("CheckWebDir() = nil, want error")
	}

	// 存在するだけでは足りない。ディレクトリでも os.Stat は成功するが、
	// 配信は 404 にするので、ここを通すと起動時に落とす意味が無くなる。
	asDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(asDir, "index.html"), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := httpapi.CheckWebDir(asDir); err == nil {
		t.Error("CheckWebDir() = nil, want error")
	}
}
