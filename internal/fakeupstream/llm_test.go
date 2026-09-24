package fakeupstream_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/llm"
	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/fakeupstream"
	"github.com/yusuke0610/etoki/port"
)

func stringsReader(s string) io.Reader { return strings.NewReader(s) }

// 本物の LLM アダプタを偽物に向け、返った本文が解釈結果として読めることを見る。
// Messages API の形（content / stop_reason）がずれると Complete が失敗する。
func TestLLMAdapterRoundTrip(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(fakeupstream.New())
	t.Cleanup(srv.Close)

	c, err := llm.New(llm.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	resp, err := c.Complete(context.Background(), port.VisionRequest{
		System: "system",
		Text:   "囲みに含まれるテキスト:\n- ログイン画面\n",
	})
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if resp.Usage.InputTokens == 0 || resp.Usage.OutputTokens == 0 {
		t.Errorf("Usage = %+v, want non-zero", resp.Usage)
	}

	in, err := domain.ParseInterpretation([]byte(resp.Text), nil)
	if err != nil {
		t.Fatalf("ParseInterpretation() = %v\n%s", err, resp.Text)
	}
	if err := in.Validate(domain.GranularityAuto); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
	if len(in.Items) != 3 || in.Items[0].Title != "ログイン画面 をまとめる" {
		t.Errorf("Items = %+v", in.Items)
	}
}
