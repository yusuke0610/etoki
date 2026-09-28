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
	"github.com/yusuke0610/etoki/internal/usecase"
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

// 停止の猶予は、作成が記録を終えるまでの最長を覆う（ADR 0056、#170）。
//
// **これが崩れると失われるのは 1 件ではなく run ごと。** 書き込み中の 1 件は
// 取り消しから切り離して待つので（ADR 0051）、猶予が先に尽きるとプロセスが
// 終わり、その run で先に作れていた項目の記録まで消える。GitHub には draft
// issue があるのに etoki は何も知らない、という ADR 0009 がいちばん避けたかった
// 状態に戻る。
//
// 実際に 75 秒待つわけにはいかないので、定数どうしの関係で固定する。猶予を
// 固定値（以前の 10 秒）に書き戻すと落ちる。
func TestShutdownBudgetCoversCreationDrain(t *testing.T) {
	t.Parallel()

	shutdown, cancelAfter := etoki.ShutdownBudgetForTest()

	if want := cancelAfter + usecase.MaxCreationDrain; shutdown < want {
		t.Errorf("shutdownTimeout = %v, want >= %v（切るまで %v + 後始末 %v）",
			shutdown, want, cancelAfter, usecase.MaxCreationDrain)
	}
	// 切るのは猶予の中でなければ意味が無い。等しくすると後始末の時間が残らない。
	if cancelAfter >= shutdown {
		t.Errorf("cancelRequestsAfter = %v, want < shutdownTimeout %v", cancelAfter, shutdown)
	}
}

// 取り消しのあとも走り続けるハンドラを、猶予の中なら待ち切る。
//
// **ctx を切ることと、ハンドラを打ち切ることは別。** `Shutdown` は処理中の
// ハンドラが返るのを待つので、切り離して書き込みを続ける作成（ADR 0051）は
// 最後まで進んで記録できる。猶予を超えて打ち切る実装に変えると落ちる。
func TestRunWaitsForHandlersThatOutliveTheCancel(t *testing.T) {
	t.Parallel()

	const (
		cancelAfter = 50 * time.Millisecond
		// 切られたあとも走り続ける「書き込み中の 1 件 + 記録」のぶん。
		drain    = 250 * time.Millisecond
		shutdown = cancelAfter + drain + 250*time.Millisecond
	)

	entered := make(chan struct{})
	recorded := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		// ctx は見ない。始めた 1 件は取り消しから切り離して待つ（ADR 0051）。
		time.Sleep(drain)
		close(recorded)
		w.WriteHeader(http.StatusNoContent)
	})

	srv := etoki.NewServerForTest(freeAddr(t), h, shutdown, cancelAfter)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	waitForListener(t, srv.Addr())

	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+srv.Addr()+"/", nil)
		if err != nil {
			return
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not entered")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}

	// **Run が返ったときには記録まで終わっている。** 先に返る実装だと、
	// この時点ではまだ書き込みの途中で、プロセスが終われば記録は残らない。
	select {
	case <-recorded:
	default:
		t.Error("Run が記録の前に返った（猶予を超えて打ち切っている）")
	}
}

// 停止の猶予が尽きる前に、処理中のリクエストの ctx を切る（ADR 0051、#140）。
//
// Shutdown は処理中のハンドラを待つだけなので、切らないと作成は猶予を超えて
// 走り続け、プロセスごと終わって記録が残らない。ctx が切れれば作成は次の 1 件に
// 手を付けずに止まり、残りの猶予で run を記録できる。
func TestRunCancelsInFlightRequestsBeforeShutdownTimeout(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	sawCancel := make(chan bool, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
			sawCancel <- true
		case <-time.After(10 * time.Second):
			sawCancel <- false
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// 猶予は長く、切るまでは短く取る。切らない実装だと猶予いっぱい待って
	// Shutdown がタイムアウトする。
	srv := etoki.NewServerForTest(freeAddr(t), h, 5*time.Second, 50*time.Millisecond)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	waitForListener(t, srv.Addr())

	go func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+srv.Addr()+"/", nil)
		if err != nil {
			return
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not entered")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run: %v, want 猶予内に停止する", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return before the shutdown timeout")
	}

	if !<-sawCancel {
		t.Error("処理中のリクエストの ctx が切れていない")
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

// 実行の上限は 0 を「無制限」と読ませない（ADR 0044）。未設定（既定 1）と
// 0（無制限）で意味が逆向きになるため、設定するなら 1 以上を要求する。
// 窓だけを設定した場合も落とす。効かない設定を黙って受けると、設定した
// つもりの上限が外れる。
func TestNewRejectsBrokenLLMLimits(t *testing.T) {
	t.Parallel()

	cases := map[string]etoki.LLMLimits{
		"同時実行が負":       {MaxConcurrent: -1},
		"回数が負":         {RateLimit: -1},
		"窓が負":          {RateLimit: 1, RateWindow: -time.Second},
		"窓だけで回数の上限が無い": {RateWindow: time.Hour},
	}

	for name, limits := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			opts := options(t, "")
			opts.LLMLimits = limits

			if _, err := etoki.New(opts); err == nil {
				t.Fatalf("New(%+v) = nil, want error", limits)
			}
		})
	}
}

// 未設定はそのまま通る。既定の構成では回数の上限が無く、同時実行だけが効く。
func TestNewAcceptsUnsetLLMLimits(t *testing.T) {
	t.Parallel()

	if _, err := etoki.New(options(t, "")); err != nil {
		t.Fatalf("New: %v", err)
	}
}
