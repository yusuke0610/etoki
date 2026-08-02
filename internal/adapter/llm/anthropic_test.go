package llm_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/llm"
	"github.com/yusuke0610/etoki/port"
)

const testAPIKey = "sk-ant-secret-must-not-leak"

// okResponse は本文だけを返す最小の応答。
const okResponse = `{"content":[{"type":"text","text":"{\"summary\":\"ok\"}"}],"stop_reason":"end_turn"}`

// newClient は t のサーバーに向いたクライアントを返す。
func newClient(t *testing.T, handler http.HandlerFunc) *llm.Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c, err := llm.New(llm.Config{BaseURL: srv.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return c
}

// 認証を要求しないローカルのエンドポイントを想定するため、鍵は必須ではない。
func TestComplete_OmitsAPIKeyHeaderWhenUnset(t *testing.T) {
	t.Parallel()

	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	c, err := llm.New(llm.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	// 空の x-api-key を送ると、認証不要の相手がかえって弾くことがある。
	if _, ok := gotHeaders["X-Api-Key"]; ok {
		t.Errorf("鍵が未設定なのに x-api-key を送っている: %q", gotHeaders.Get("x-api-key"))
	}
	if got := gotHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
}

// 綴り間違いを実行時まで持ち越すと、解釈の失敗として現れて切り分けが遠回りになる。
func TestNew_RejectsInvalidBaseURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"スキームが無い":   "api.anthropic.com",
		"対応しないスキーム": "ftp://example.test",
		"壊れた URL":   "http://[::1",
	}

	for name, base := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := llm.New(llm.Config{BaseURL: base, APIKey: testAPIKey}); err == nil {
				t.Fatalf("New(%q) = nil, want error", base)
			}
		})
	}
}

// 認証方式が違う基盤に載せ替えるときは、RoundTripper で付け替える（ADR 0008）。
func TestComplete_AllowsAuthViaRoundTripper(t *testing.T) {
	t.Parallel()

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	c, err := llm.New(llm.Config{
		BaseURL:    srv.URL,
		HTTPClient: &http.Client{Transport: bearerTransport{token: "gateway-token"}},
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	if gotAuth != "Bearer gateway-token" {
		t.Errorf("authorization = %q, want Bearer gateway-token", gotAuth)
	}
}

// bearerTransport は x-api-key 以外の認証を使う基盤の代役。
type bearerTransport struct{ token string }

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestComplete_SendsAnthropicShape(t *testing.T) {
	t.Parallel()

	var (
		gotPath    string
		gotHeaders http.Header
		gotBody    map[string]any
	)

	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()

		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("リクエストが JSON ではない: %v", err)
		}

		_, _ = io.WriteString(w, okResponse)
	})

	_, err := c.Complete(t.Context(), port.VisionRequest{
		System: "あなたは整理する担当です",
		Text:   "囲みに含まれるテキスト",
	})
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if got := gotHeaders.Get("x-api-key"); got != testAPIKey {
		t.Errorf("x-api-key = %q", got)
	}
	if got := gotHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := gotHeaders.Get("content-type"); got != "application/json" {
		t.Errorf("content-type = %q", got)
	}

	if gotBody["model"] != llm.DefaultModel {
		t.Errorf("model = %v, want %q", gotBody["model"], llm.DefaultModel)
	}
	if gotBody["max_tokens"] != float64(llm.DefaultMaxTokens) {
		t.Errorf("max_tokens = %v, want %d", gotBody["max_tokens"], llm.DefaultMaxTokens)
	}
	if gotBody["system"] != "あなたは整理する担当です" {
		t.Errorf("system = %v", gotBody["system"])
	}

	// thinking は送らない。明示的に無効化すると本文にタグが混ざることがある。
	if _, ok := gotBody["thinking"]; ok {
		t.Error("thinking を送っている")
	}

	messages, _ := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(messages))
	}
	msg, _ := messages[0].(map[string]any)
	if msg["role"] != "user" {
		t.Errorf("role = %v, want user", msg["role"])
	}

	content, _ := msg["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("len(content) = %d, want 1", len(content))
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "囲みに含まれるテキスト" {
		t.Errorf("content[0] = %v", first)
	}
}

// base64 エンコードは実装側の責務（port.Image のコメント）。
func TestComplete_EncodesImages(t *testing.T) {
	t.Parallel()

	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

	var gotBody map[string]any
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, okResponse)
	})

	_, err := c.Complete(t.Context(), port.VisionRequest{
		Text:   "この囲みを解釈して",
		Images: []port.Image{{MediaType: "image/png", Data: pngBytes}},
	})
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	messages, _ := gotBody["messages"].([]any)
	msg, _ := messages[0].(map[string]any)
	content, _ := msg["content"].([]any)

	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2", len(content))
	}

	// 画像はテキストより前。Messages API はこの順を前提にしている。
	image, _ := content[0].(map[string]any)
	if image["type"] != "image" {
		t.Fatalf("content[0].type = %v, want image", image["type"])
	}
	source, _ := image["source"].(map[string]any)
	if source["type"] != "base64" {
		t.Errorf("source.type = %v, want base64", source["type"])
	}
	if source["media_type"] != "image/png" {
		t.Errorf("source.media_type = %v", source["media_type"])
	}
	if want := base64.StdEncoding.EncodeToString(pngBytes); source["data"] != want {
		t.Errorf("source.data = %v, want %q", source["data"], want)
	}

	text, _ := content[1].(map[string]any)
	if text["type"] != "text" {
		t.Errorf("content[1].type = %v, want text", text["type"])
	}
}

// thinking が有効なモデルでは thinking ブロックが本文より前に並ぶ。
// content[0] を本文とみなすと空文字を掴む。
func TestComplete_SkipsNonTextBlocks(t *testing.T) {
	t.Parallel()

	body := `{"content":[
		{"type":"thinking","thinking":""},
		{"type":"text","text":"{\"summary\":"},
		{"type":"text","text":"\"決済まわり\"}"}
	],"stop_reason":"end_turn"}`

	c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	resp, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"})
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if want := `{"summary":"決済まわり"}`; resp.Text != want {
		t.Errorf("Text = %q, want %q", resp.Text, want)
	}
}

func TestComplete_StopReasons(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{
			// refusal は HTTP 200 で返る。content を読む前に判定する。
			name: "拒否",
			body: `{"content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"}}`,
			want: "refused",
		},
		{
			// category は常に付くとは限らない。
			name: "分類なしの拒否",
			body: `{"content":[],"stop_reason":"refusal"}`,
			want: "refused the request",
		},
		{
			// 打ち切られた応答は JSON として壊れており、再送しても直らない。
			name: "上限で打ち切り",
			body: `{"content":[{"type":"text","text":"{\"summary\":"}],"stop_reason":"max_tokens"}`,
			want: "truncated",
		},
		{
			name: "本文のブロックが無い",
			body: `{"content":[{"type":"thinking","thinking":""}],"stop_reason":"end_turn"}`,
			want: "no text block",
		},
		{
			name: "JSON ではない応答",
			body: `<html>502 Bad Gateway</html>`,
			want: "decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			})

			_, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"})
			if err == nil {
				t.Fatal("Complete() = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Complete() = %v, want to contain %q", err, tt.want)
			}
		})
	}
}

func TestComplete_HTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   []string
	}{
		{
			name:   "401 認証エラー",
			status: http.StatusUnauthorized,
			body:   `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`,
			want:   []string{"401", "authentication_error", "invalid x-api-key"},
		},
		{
			name:   "429 レート制限",
			status: http.StatusTooManyRequests,
			body:   `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
			want:   []string{"429", "rate_limit_error"},
		},
		{
			name:   "500 サーバーエラー",
			status: http.StatusInternalServerError,
			body:   `{"type":"error","error":{"type":"api_error","message":"internal"}}`,
			want:   []string{"500", "api_error"},
		},
		{
			// エラー形式でないボディでも、原因が分かる情報を残す。
			name:   "JSON ではないエラー応答",
			status: http.StatusBadGateway,
			body:   `<html>502 Bad Gateway</html>`,
			want:   []string{"502", "Bad Gateway"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})

			_, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"})
			if err == nil {
				t.Fatal("Complete() = nil, want error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Complete() = %v, want to contain %q", err, want)
				}
			}
		})
	}
}

// newBlockingServer は応答を返さないサーバーを返す。
//
// 後片付けで必ずハンドラを解放する。httptest.Server.Close は処理中の
// リクエストを待つので、解放しないとテストが終わらない。
func newBlockingServer(t *testing.T) *httptest.Server {
	t.Helper()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})

	return srv
}

func TestComplete_Timeout(t *testing.T) {
	t.Parallel()

	srv := newBlockingServer(t)

	c, err := llm.New(llm.Config{
		BaseURL:    srv.URL,
		APIKey:     testAPIKey,
		HTTPClient: &http.Client{Timeout: 50 * time.Millisecond},
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	if _, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"}); err == nil {
		t.Fatal("Complete() = nil, want error")
	}
}

func TestComplete_HonorsContextCancellation(t *testing.T) {
	t.Parallel()

	srv := newBlockingServer(t)

	c, err := llm.New(llm.Config{BaseURL: srv.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = c.Complete(ctx, port.VisionRequest{Text: "x"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete() = %v, want context.Canceled", err)
	}
}

// 鍵がエラーに混ざると、ログや画面に出た時点で漏れる。
func TestComplete_DoesNotLeakAPIKey(t *testing.T) {
	t.Parallel()

	tests := map[string]http.HandlerFunc{
		"エラー応答": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
		},
		"壊れた応答": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `not json`)
		},
		"拒否": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"content":[],"stop_reason":"refusal","stop_details":{"category":"cyber"}}`)
		},
	}

	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := newClient(t, handler)

			_, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"})
			if err == nil {
				t.Fatal("Complete() = nil, want error")
			}
			if strings.Contains(err.Error(), testAPIKey) {
				t.Errorf("エラーに API キーが含まれている: %v", err)
			}
		})
	}
}

func TestComplete_TrimsTrailingSlashInBaseURL(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	c, err := llm.New(llm.Config{BaseURL: srv.URL + "/", APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
}

func TestComplete_UsesConfiguredModelAndMaxTokens(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = io.WriteString(w, okResponse)
	}))
	t.Cleanup(srv.Close)

	c, err := llm.New(llm.Config{
		BaseURL:   srv.URL,
		APIKey:    testAPIKey,
		Model:     "claude-opus-4-8",
		MaxTokens: 2048,
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	if _, err := c.Complete(t.Context(), port.VisionRequest{Text: "x"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	if gotBody["model"] != "claude-opus-4-8" {
		t.Errorf("model = %v", gotBody["model"])
	}
	if gotBody["max_tokens"] != float64(2048) {
		t.Errorf("max_tokens = %v, want 2048", gotBody["max_tokens"])
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("設定を読む", func(t *testing.T) {
		t.Setenv("ETOKI_LLM_BASE_URL", "https://example.test")
		t.Setenv("ETOKI_LLM_API_KEY", "key-1")
		t.Setenv("ETOKI_LLM_MODEL", "claude-opus-4-8")

		got := llm.ConfigFromEnv()
		want := llm.Config{BaseURL: "https://example.test", APIKey: "key-1", Model: "claude-opus-4-8"}
		if got != want {
			t.Errorf("ConfigFromEnv() = %+v, want %+v", got, want)
		}
	})

	t.Run("未設定なら空のまま既定に倒れる", func(t *testing.T) {
		t.Setenv("ETOKI_LLM_BASE_URL", "")
		t.Setenv("ETOKI_LLM_API_KEY", "key-1")
		t.Setenv("ETOKI_LLM_MODEL", "")

		c, err := llm.New(llm.ConfigFromEnv())
		if err != nil {
			t.Fatalf("New() = %v", err)
		}
		if c == nil {
			t.Fatal("New() = nil")
		}
	})
}
