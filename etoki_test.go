package etoki_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki"
	githubauth "github.com/yusuke0610/etoki/internal/adapter/auth/github"
	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/internal/secret"
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

// 何も送ってこないキープアライブの接続は、期限が来たらサーバーが切る（#64）。
// 切らないと、開いたままの接続が数の上限なく積み上がる。
func TestRunClosesIdleConnections(t *testing.T) {
	t.Parallel()

	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := etoki.NewServerForTest(freeAddr(t), h, time.Second, time.Second)
	srv.SetConnTimeoutsForTest(time.Second, 100*time.Millisecond)
	runForTest(t, srv)

	conn := dialForTest(t, srv.Addr())
	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 応答の 1 つ目を読み切る（ステータス行と空行まで。本文は 204 なので無い）。
	if _, err := readUntil(conn, "\r\n\r\n"); err != nil {
		t.Fatalf("read response: %v", err)
	}

	// アイドルの期限を過ぎるまで黙る。切られていれば EOF、切られていなければ
	// こちらの読み込みの期限が先に来る。
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}
	_, err := conn.Read(make([]byte, 1))
	if ne := net.Error(nil); errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("idle connection was kept open")
	}
	if err == nil {
		t.Fatal("read returned data on an idle connection")
	}
}

// 本文を送り切らない接続も、読み込みの期限で切る（#64）。本文の大きさの上限
// （#147）は送られてきた量を縛るだけで、送り終えるまでの長さは縛らない。
func TestRunCutsSlowRequestBodies(t *testing.T) {
	t.Parallel()

	readErr := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		readErr <- err
	})
	srv := etoki.NewServerForTest(freeAddr(t), h, time.Second, time.Second)
	srv.SetConnTimeoutsForTest(200*time.Millisecond, time.Second)
	runForTest(t, srv)

	conn := dialForTest(t, srv.Addr())
	// 100 バイトあると言って 10 バイトだけ送り、あとは黙る。
	if _, err := conn.Write([]byte("POST / HTTP/1.1\r\nHost: 127.0.0.1\r\n" +
		"Content-Length: 100\r\n\r\n0123456789")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case err := <-readErr:
		if err == nil {
			t.Fatal("handler read the whole body, want a timeout")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slow body was not cut by the read timeout")
	}
}

// **読み込みの期限は、本文を読み終えたあとのハンドラを切らない。** 解釈は分単位で
// 待ち、作成は接続が切れたかどうかを ctx で見て止まる（ADR 0051）。期限で ctx まで
// 切れると、接続は生きているのに作成が途中で止まる。net/http は本文を読み終えると
// 読み込みの期限を外すので、ここで固定する。
func TestRunReadTimeoutDoesNotCancelLongHandlers(t *testing.T) {
	t.Parallel()

	const readTimeout = 100 * time.Millisecond

	canceled := make(chan bool, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			canceled <- true
		case <-time.After(4 * readTimeout):
			canceled <- false
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := etoki.NewServerForTest(freeAddr(t), h, time.Second, time.Second)
	srv.SetConnTimeoutsForTest(readTimeout, time.Second)
	runForTest(t, srv)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+srv.Addr()+"/", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_ = resp.Body.Close()

	if <-canceled {
		t.Error("request ctx was canceled by the read timeout while the handler was running")
	}
}

// runForTest は srv を走らせ、テストの終わりに止める。
func runForTest(t *testing.T, srv *etoki.Server) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	waitForListener(t, srv.Addr())
}

// dialForTest は addr に生の TCP でつなぐ。HTTP クライアントを通すと、接続の
// 使い回しと読み込みが隠れて、切られたかどうかを見られない。
func dialForTest(t *testing.T, addr string) net.Conn {
	t.Helper()

	var d net.Dialer
	conn, err := d.DialContext(t.Context(), "tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// readUntil は sep が現れるまで読む。
func readUntil(conn net.Conn, sep string) (string, error) {
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return "", err
	}
	var got []byte
	buf := make([]byte, 256)
	for !strings.Contains(string(got), sep) {
		n, err := conn.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			return string(got), err
		}
	}
	return string(got), nil
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

// 認証なしでループバック以外にバインドしたら、起動時に知らせる（issue #148）。
//
// 止めはしない。広げるのは利用者が明示的に選んだ設定（ADR 0016）で、拒むと
// その選択を後から覆すことになる。代わりに、どういう構成になっているかを
// 見せる（中核思想 3）。
//
// **警告が出ないほうも固定する。** 常に出す実装でも「出る」側だけなら通る。
func TestNewWarnsWhenExposedWithoutAuth(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		addr string
		auth bool
		want bool
	}{
		"既定（ループバック）":    {addr: "", want: false},
		"127.0.0.1 を明示": {addr: "127.0.0.1:8080", want: false},
		"localhost":     {addr: "localhost:8080", want: false},
		"IPv6 のループバック":  {addr: "[::1]:8080", want: false},
		"0.0.0.0":       {addr: "0.0.0.0:8080", want: true},
		"ホストを省いた全インターフェース": {addr: ":8080", want: true},
		"LAN のアドレス":        {addr: "192.168.1.10:8080", want: true},
		"公開しても認証があれば言わない":  {addr: "0.0.0.0:8080", auth: true, want: false},
		"ループバック + 認証も言わない": {addr: "", auth: true, want: false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			opts := options(t, tc.addr)
			opts.Logger = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelWarn,
			}))
			if tc.auth {
				opts.Auth = fakeAuthenticator(t)
			}

			if _, err := etoki.New(opts); err != nil {
				t.Fatalf("New: %v", err)
			}

			got := strings.Contains(buf.String(), "listening beyond loopback without authentication")
			if got != tc.want {
				t.Errorf("warned = %t, want %t (log: %q)", got, tc.want, buf.String())
			}
		})
	}
}

// fakeAuthenticator は「認証を設定した」状態を作るためだけの Authenticator。
//
// 中身は呼ばない。New が見るのは nil かどうかだけ。
func fakeAuthenticator(t *testing.T) *etoki.Authenticator {
	t.Helper()

	provider, err := githubauth.New(githubauth.Config{ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("githubauth.New: %v", err)
	}

	box, err := secret.New(make([]byte, secret.KeySize))
	if err != nil {
		t.Fatalf("secret.New: %v", err)
	}

	db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	auth, err := etoki.NewAuthenticator(provider, sqlite.NewSessionRepository(db, box))
	if err != nil {
		t.Fatalf("NewAuthenticator: %v", err)
	}
	return auth
}

// 認証ありの構成で `/mcp` を開くのは、許可の保存先を渡したときだけ（ADR 0076）。
// 渡さなければこれまでどおり 503（ADR 0071）。渡せば、トークンを求める 401 に
// なり、MCP のクライアントは認可の流れを始められる。
func TestNew_OpensMCPWithOAuthGrants(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		withGrants bool
		want       int
	}{
		"保存先なし": {withGrants: false, want: http.StatusServiceUnavailable},
		"保存先あり": {withGrants: true, want: http.StatusUnauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			opts := options(t, "")
			opts.Auth = fakeAuthenticator(t)
			if tc.withGrants {
				db, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "grants.db"))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				t.Cleanup(func() { _ = db.Close() })
				opts.OAuthGrants = sqlite.NewOAuthGrantRepository(db)
			}
			srv, err := etoki.New(opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/mcp",
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Host = "127.0.0.1:8080"
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}
