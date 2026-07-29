package etoki_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	m.Run()
}

func TestNewDefaultsToLoopback(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(etoki.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.Addr() != etoki.DefaultAddr {
		t.Errorf("Addr() = %q, want %q", srv.Addr(), etoki.DefaultAddr)
	}
}

func TestNewRejectsInvalidAddr(t *testing.T) {
	t.Parallel()

	if _, err := etoki.New(etoki.Options{Addr: "not-an-address"}); err == nil {
		t.Fatal("New: want error for malformed addr, got nil")
	}
}

func TestHandlerServesHealthz(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(etoki.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRunShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	srv, err := etoki.New(etoki.Options{Addr: freeAddr(t)})
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

	srv, err := etoki.New(etoki.Options{Addr: addr})
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
