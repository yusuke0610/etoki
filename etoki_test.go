package etoki_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki"
	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

// repos は一時 DB に紐づいたリポジトリ一式を返す。
func repos(t *testing.T) (port.BoardRepository, port.MappingRepository) {
	t.Helper()

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "etoki.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqlite.Migrate(t.Context(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	return sqlite.NewBoardRepository(db), sqlite.NewMappingRepository(db)
}

// options は Addr 以外を埋めた Options を返す。
func options(t *testing.T, addr string) etoki.Options {
	t.Helper()

	boards, mappings := repos(t)
	return etoki.Options{Addr: addr, Boards: boards, Mappings: mappings}
}

func TestNewDefaultsToLoopback(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(options(t, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Addr() != etoki.DefaultAddr {
		t.Errorf("Addr() = %q, want %q", srv.Addr(), etoki.DefaultAddr)
	}
}

func TestNewRejectsInvalidAddr(t *testing.T) {
	t.Parallel()

	if _, err := etoki.New(options(t, "not-an-address")); err == nil {
		t.Fatal("New: want error for malformed addr, got nil")
	}
}

// リポジトリは必須。外部リポジトリが差し込みを忘れたまま起動しないよう、
// 組み立て時点で弾く。
func TestNewRequiresRepositories(t *testing.T) {
	t.Parallel()

	boards, mappings := repos(t)

	if _, err := etoki.New(etoki.Options{Mappings: mappings}); err == nil {
		t.Error("New: want error when Boards is nil, got nil")
	}
	if _, err := etoki.New(etoki.Options{Boards: boards}); err == nil {
		t.Error("New: want error when Mappings is nil, got nil")
	}
}

func TestHandlerServesHealthz(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(options(t, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	// httptest の既定の Host は example.com。cross-site を弾くミドルウェアに
	// 引っかかるので、実際に届く形と揃える。
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// 組み立てたサーバーに API ルートが載っていること。
func TestHandlerServesBoardAPI(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(options(t, ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/boards", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
}

// 配ると決めたなら、組み立てたサーバーから画面が出ること。
func TestHandlerServesWebUI(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	const index = "<!doctype html><html lang=\"ja\"><body></body></html>\n"
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(index), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	opts := options(t, "")
	opts.WebDir = dir

	srv, err := etoki.New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusOK, rec.Body)
	}
	if rec.Body.String() != index {
		t.Errorf("body = %q, want %q", rec.Body.String(), index)
	}
}

// 指定したのに配れない構成では起動させない。bun run build を忘れた場合も、
// パスを打ち間違えた場合もここで落ちる。実行時に 404 が並ぶ形にすると、
// 原因が画面側にあるのかサーバー側にあるのかを切り分けられない。
func TestNewRejectsWebDirWithoutIndex(t *testing.T) {
	t.Parallel()

	opts := options(t, "")
	opts.WebDir = t.TempDir()

	if _, err := etoki.New(opts); err == nil {
		t.Fatal("New() = nil, want error")
	}
}

func TestRunShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(options(t, freeAddr(t)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	waitForListener(t, srv.Addr())
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}

func TestRunReportsListenFailure(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)

	// ポートを掴んだまま同じアドレスで起動させ、bind 失敗が
	// エラーとして返ることを確かめる。
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	srv, err := etoki.New(options(t, addr))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	switch err := srv.Run(t.Context()); {
	case err == nil:
		t.Error("Run: want error for occupied port, got nil")
	case errors.Is(err, http.ErrServerClosed):
		t.Errorf("Run: unexpected ErrServerClosed: %v", err)
	}
}

// freeAddr は空きポートを 1 つ確保して即座に解放し、そのアドレスを返す。
// 固定ポートを使うとテストの並行実行で衝突するため。
func freeAddr(t *testing.T) string {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr
}

// waitForListener は addr が接続を受け付けるまで待つ。
func waitForListener(t *testing.T, addr string) {
	t.Helper()

	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		conn, err := dialer.DialContext(t.Context(), "tcp", addr)
		if err == nil {
			if err := conn.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server did not start listening on %s", addr)
}
