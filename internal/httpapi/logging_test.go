package httpapi_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/usecase"
)

// captureLogs はログ出力を受け取るバッファとロガーを返す。
func captureLogs() (*bytes.Buffer, *slog.Logger) {
	var buf bytes.Buffer
	return &buf, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func TestRequestLogging(t *testing.T) {
	t.Parallel()

	buf, logger := captureLogs()

	boards, mappings := newRepos(t)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings),
		Annotations: usecase.NewAnnotationService(boards, mappings),
		Logger:      logger,
	})

	do(t, r, http.MethodGet, "/api/boards", nil)

	out := buf.String()
	if !strings.Contains(out, "msg=request") {
		t.Errorf("リクエストログが出ていない: %q", out)
	}
	if !strings.Contains(out, "path=/api/boards") || !strings.Contains(out, "status=200") {
		t.Errorf("ログに method/path/status が含まれていない: %q", out)
	}
}

// ヘルスチェックは高頻度で叩かれうるうえ情報量がないので落とす。
func TestRequestLogging_SkipsHealthz(t *testing.T) {
	t.Parallel()

	buf, logger := captureLogs()

	boards, mappings := newRepos(t)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings),
		Annotations: usecase.NewAnnotationService(boards, mappings),
		Logger:      logger,
	})

	do(t, r, http.MethodGet, "/healthz", nil)

	if out := buf.String(); strings.Contains(out, "msg=request") {
		t.Errorf("healthz がログに出ている: %q", out)
	}
}

// 4xx は呼び出し側の問題なので warn に留める。
func TestRequestLogging_ClientErrorIsWarn(t *testing.T) {
	t.Parallel()

	buf, logger := captureLogs()

	boards, mappings := newRepos(t)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings),
		Annotations: usecase.NewAnnotationService(boards, mappings),
		Logger:      logger,
	})

	do(t, r, http.MethodGet, "/api/boards/no-such-id", nil)

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("4xx が WARN になっていない: %q", out)
	}
}

// 500 を返すときは原因をサーバー側に残す。
// レスポンスに内部情報は載せないが、手元で調べられないと困る。
func TestUnhandledErrorIsLogged(t *testing.T) {
	t.Parallel()

	buf, logger := captureLogs()

	// マイグレーションしていない DB を渡すと、全クエリが失敗する。
	// 未初期化のまま起動してしまった状況を再現している。
	boards, mappings := newUnmigratedRepos(t)
	r := httpapi.NewRouter(httpapi.Deps{
		Boards:      usecase.NewBoardService(boards, mappings),
		Annotations: usecase.NewAnnotationService(boards, mappings),
		Logger:      logger,
	})

	rec := do(t, r, http.MethodGet, "/api/boards", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	out := buf.String()
	if !strings.Contains(out, "unhandled error") {
		t.Errorf("エラーログが出ていない: %q", out)
	}
	if !strings.Contains(out, "no such table") {
		t.Errorf("ログに原因が含まれていない: %q", out)
	}
	// レスポンス側には内部情報を出さない。
	if strings.Contains(rec.Body.String(), "no such table") {
		t.Errorf("レスポンスに内部情報が漏れている: %q", rec.Body.String())
	}
}
