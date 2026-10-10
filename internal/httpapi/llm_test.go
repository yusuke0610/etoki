package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki/internal/usecase"
)

// LLM を叩く実行の失敗は、解釈と図の生成で記録の出し分けが同じ。違うのは
// 「出力を受け付けられなかった」側の sentinel だけ（#156）。
//
// **相手の sentinel では Warn を出さない**ことまで見る。取り違えて渡すと、
// 出力を弾いた失敗が記録から消えるが、応答は errors.go の表が返すので
// ステータスの断言だけでは気づけない。
func TestFailLLM_LogsByCause(t *testing.T) {
	cases := map[string]struct {
		err      error
		rejected error
		status   int
		wantLog  string
		wantNone bool
	}{
		"解釈: 接続の失敗は Error": {
			err:      fmt.Errorf("wrap: %w", usecase.ErrLLMUnavailable),
			rejected: usecase.ErrInterpretationFailed,
			status:   http.StatusBadGateway,
			wantLog:  `level=ERROR msg="llm call failed"`,
		},
		"解釈: 出力を弾いたら Warn": {
			err:      fmt.Errorf("wrap: %w", usecase.ErrInterpretationFailed),
			rejected: usecase.ErrInterpretationFailed,
			status:   http.StatusBadGateway,
			wantLog:  `level=WARN msg="llm output rejected"`,
		},
		"図: 出力を弾いたら Warn": {
			err:      fmt.Errorf("wrap: %w", usecase.ErrDiagramFailed),
			rejected: usecase.ErrDiagramFailed,
			status:   http.StatusBadGateway,
			wantLog:  `level=WARN msg="llm output rejected"`,
		},
		"図: 解釈の sentinel は図の失敗として記録しない": {
			err:      fmt.Errorf("wrap: %w", usecase.ErrInterpretationFailed),
			rejected: usecase.ErrDiagramFailed,
			status:   http.StatusBadGateway,
			wantNone: true,
		},
		"それ以外は記録しない（表が応答を決める）": {
			err:      errors.New("boom"),
			rejected: usecase.ErrDiagramFailed,
			status:   http.StatusInternalServerError,
			wantNone: true,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// captureLogs は外部パッケージのテストにあるので、ここで作る。
			var buf bytes.Buffer
			h := &handlers{logger: slog.New(slog.NewTextHandler(&buf, nil))}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/boards/b1/diagram", nil)

			h.failLLM(c, tc.err, tc.rejected)

			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			got := buf.String()
			if tc.wantNone {
				if strings.Contains(got, "llm call failed") || strings.Contains(got, "llm output rejected") {
					t.Errorf("記録しないはずが出た: %s", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantLog) {
				t.Errorf("log = %q, want to contain %q", got, tc.wantLog)
			}
		})
	}
}

// 解釈の途中でページをリロードすると、リクエストの ctx が切れて LLM 呼び出しが
// context canceled で失敗する。これは上流の障害ではなく、待っていた相手が
// いなくなっただけ。502 と Error を出すと、利用者が止めただけのものが障害として
// 記録に残り、LLM や設定を疑って遠回りする（issue #253）。
//
// 切れたのがリクエストの ctx であることを条件にする。ctx が生きているのに
// 接続が失敗したものは、今までどおり障害として記録する。
func TestFailLLM_ClientGoneIsNotAnUpstreamFailure(t *testing.T) {
	cases := map[string]struct {
		cancel     bool
		err        error
		wantStatus int
		wantLog    string
		notWantLog string
	}{
		"リクエストが切れていて呼び出しも切れた": {
			cancel: true,
			err: fmt.Errorf("%w (attempt 2): %w", usecase.ErrLLMUnavailable,
				fmt.Errorf("call messages api: %w", context.Canceled)),
			wantStatus: statusClientClosedRequest,
			wantLog:    `level=INFO msg="llm call cancelled by client"`,
			notWantLog: "llm call failed",
		},
		"リクエストは生きているのに呼び出しが切れた": {
			cancel: false,
			err: fmt.Errorf("%w (attempt 2): %w", usecase.ErrLLMUnavailable,
				fmt.Errorf("call messages api: %w", context.Canceled)),
			wantStatus: http.StatusBadGateway,
			wantLog:    `level=ERROR msg="llm call failed"`,
			notWantLog: "cancelled by client",
		},
		"リクエストが切れたあとの出力の拒否は拒否のまま": {
			cancel:     true,
			err:        fmt.Errorf("wrap: %w", usecase.ErrInterpretationFailed),
			wantStatus: http.StatusBadGateway,
			wantLog:    `level=WARN msg="llm output rejected"`,
			notWantLog: "cancelled by client",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			h := &handlers{logger: slog.New(slog.NewTextHandler(&buf, nil))}

			ctx, cancel := context.WithCancel(t.Context())
			if tc.cancel {
				cancel()
			}
			defer cancel()

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/boards/b1/annotations/a1/interpret", nil)

			h.failLLM(c, tc.err, usecase.ErrInterpretationFailed)

			if c.Writer.Status() != tc.wantStatus {
				t.Fatalf("status = %d, want %d", c.Writer.Status(), tc.wantStatus)
			}
			got := buf.String()
			if !strings.Contains(got, tc.wantLog) {
				t.Errorf("log = %q, want to contain %q", got, tc.wantLog)
			}
			if strings.Contains(got, tc.notWantLog) {
				t.Errorf("log = %q, must not contain %q", got, tc.notWantLog)
			}
		})
	}
}
