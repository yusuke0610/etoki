package httpapi

import (
	"bytes"
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
