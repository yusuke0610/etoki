// Package llm は port.LLMClient の既定実装を提供する。
//
// wire format は Anthropic Messages API 形状に固定してある（ADR 0005）。
//
// 差し替えの継ぎ目は 2 段ある（ADR 0008）。どちらもコア側に分岐を入れない。
//
//   - wire format は同じで認証やヘッダだけが違う（社内ゲートウェイ等）
//     … Config.HTTPClient に RoundTripper を差す。この実装をそのまま使える。
//   - wire format ごと違う（Bedrock / Vertex AI / OpenAI 互換等）
//     … port.LLMClient を自前で実装し etoki.New に渡す。この実装は使わない。
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// 既定値。ETOKI_LLM_* で差し替えられる。
const (
	// DefaultBaseURL は Messages API のホスト。
	DefaultBaseURL = "https://api.anthropic.com"
	// DefaultModel は既定のモデル（ADR 0005）。
	DefaultModel = "claude-opus-5"
	// DefaultMaxTokens は 1 回の応答で許す上限。
	//
	// claude-opus-5 は thinking が既定で有効で、max_tokens は思考と本文の
	// 合計に効く。注釈 1 つ分の JSON には十分だが、思考のぶんの余裕を見て
	// 本文の見積もりより大きく取っている。
	DefaultMaxTokens = 16000
)

// anthropicVersion は Messages API が要求するバージョンヘッダ。
const anthropicVersion = "2023-06-01"

// defaultTimeout は 1 回の呼び出しを待つ上限。
//
// thinking が有効なモデルは応答までに分単位かかることがある。短く切ると
// 正常な応答を取りこぼす。
const defaultTimeout = 5 * time.Minute

// maxResponseBytes は応答ボディを読む上限。
const maxResponseBytes = 10 << 20 // 10 MiB

// 環境変数名。
const (
	envBaseURL = "ETOKI_LLM_BASE_URL"
	envAPIKey  = "ETOKI_LLM_API_KEY"
	envModel   = "ETOKI_LLM_MODEL"
)

// Config は Client の設定。
type Config struct {
	// BaseURL は Messages API のホスト。空なら DefaultBaseURL。
	//
	// ローカルの Anthropic 互換プロキシに向ける場合はここを差し替える。
	BaseURL string

	// APIKey は x-api-key ヘッダに載せる鍵。
	//
	// 空でもよい。その場合ヘッダ自体を送らない。認証を要求しないローカルの
	// エンドポイントや、認証を HTTPClient 側で行う構成があるため。鍵が要る
	// のに空なら、呼び出したときに 401 として返る。
	APIKey string

	// Model はモデル ID。空なら DefaultModel。
	Model string

	// MaxTokens は 1 回の応答の上限。0 以下なら DefaultMaxTokens。
	MaxTokens int

	// HTTPClient は差し替え用。nil なら既定のタイムアウトを持つものを作る。
	//
	// x-api-key 以外の認証を使う基盤に載せ替えるときは、ここに RoundTripper を
	// 差してヘッダを付け替える。etoki 側のコードは触らずに済む（ADR 0008）。
	HTTPClient *http.Client
}

// ConfigFromEnv は ETOKI_LLM_* から設定を読む。
//
// 値の検証は行わない。未設定の項目は空のまま返し、既定への差し戻しは New が行う。
func ConfigFromEnv() Config {
	return Config{
		BaseURL: os.Getenv(envBaseURL),
		APIKey:  os.Getenv(envAPIKey),
		Model:   os.Getenv(envModel),
	}
}

// Client は Anthropic Messages API 形状のエンドポイントを 1 回叩く。
type Client struct {
	baseURL   string
	apiKey    string
	model     string
	maxTokens int
	http      *http.Client
}

// New は Config を検証して Client を作る。
//
// API キーの有無は検証しない。認証不要のエンドポイントも想定するため、鍵が
// 要るかどうかは向き先次第で、ここでは判断できない（ADR 0008）。
//
// BaseURL だけは起動時に検証する。綴り間違いを実行時まで持ち越すと、解釈の
// 失敗として現れて原因の切り分けが遠回りになる。
func New(cfg Config) (*Client, error) {
	base := orDefault(cfg.BaseURL, DefaultBaseURL)
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("etoki: invalid llm base url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		// Redacted は URL に埋め込まれた資格情報を伏せる。
		return nil, fmt.Errorf("etoki: invalid llm base url %q: scheme must be http or https", u.Redacted())
	}
	// url.Parse は "http://" や "https:gateway.example" も通す。スキームだけ見て
	// 通すと、ホストの無い URL が起動時の検証をすり抜け、呼び出したときに
	// "no Host in request URL" として初めて失敗する。検証を置いた意味がなくなる。
	if u.Hostname() == "" {
		return nil, fmt.Errorf("etoki: invalid llm base url %q: host is missing", u.Redacted())
	}

	c := &Client{
		baseURL:   strings.TrimRight(base, "/"),
		apiKey:    cfg.APIKey,
		model:     orDefault(cfg.Model, DefaultModel),
		maxTokens: cfg.MaxTokens,
		http:      cfg.HTTPClient,
	}
	if c.maxTokens <= 0 {
		c.maxTokens = DefaultMaxTokens
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: defaultTimeout}
	}

	return c, nil
}

// orDefault は s が空なら fallback を返す。
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// Complete は 1 回分のリクエストを送り、本文テキストを返す。
func (c *Client) Complete(ctx context.Context, req port.VisionRequest) (port.VisionResponse, error) {
	body, err := json.Marshal(c.buildRequest(req))
	if err != nil {
		return port.VisionResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return port.VisionResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	// 鍵が無いなら送らない。空の x-api-key を送ると、認証不要の相手が
	// かえって弾くことがある。
	if c.apiKey != "" {
		httpReq.Header.Set("x-api-key", c.apiKey)
	}

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		// url.Error は URL を含むが、鍵はヘッダにしか無いので漏れない。
		return port.VisionResponse{}, fmt.Errorf("call messages api: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	// 読み込む量に上限を設ける。BaseURL は利用者が差し替えられるので、
	// 設定を誤ると想定外に大きな応答を掴みうる。max_tokens ぶんの JSON に対して
	// 十分な余裕があり、正常な応答で当たることはない。
	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, maxResponseBytes))
	if err != nil {
		return port.VisionResponse{}, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return port.VisionResponse{}, statusError(httpResp.StatusCode, raw)
	}

	var resp apiResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return port.VisionResponse{}, fmt.Errorf("decode response: %w", err)
	}

	text, err := extractText(resp)
	if err != nil {
		return port.VisionResponse{}, err
	}

	return port.VisionResponse{
		Text: text,
		Raw:  raw,
		Usage: port.Usage{
			InputTokens:  resp.Usage.InputTokens,
			OutputTokens: resp.Usage.OutputTokens,
		},
	}, nil
}

// buildRequest は VisionRequest を Messages API のリクエストに詰め替える。
func (c *Client) buildRequest(req port.VisionRequest) apiRequest {
	// 画像はテキストより前に置く。Messages API はこの順を前提にしている。
	blocks := make([]contentBlock, 0, len(req.Images)+1)
	for _, img := range req.Images {
		blocks = append(blocks, contentBlock{
			Type: "image",
			Source: &imageSource{
				Type:      "base64",
				MediaType: img.MediaType,
				// base64 エンコードは実装側の責務（port.Image のコメント）。
				Data: base64.StdEncoding.EncodeToString(img.Data),
			},
		})
	}
	blocks = append(blocks, contentBlock{Type: "text", Text: req.Text})

	return apiRequest{
		Model:     c.model,
		MaxTokens: c.maxTokens,
		System:    req.System,
		Messages:  []apiMessage{{Role: "user", Content: blocks}},
	}
}

// extractText は応答から本文テキストを取り出す。
//
// content の先頭が本文とは限らない。thinking が有効なモデルでは thinking
// ブロックが先に並ぶため、type が text のものだけを拾って連結する。
func extractText(resp apiResponse) (string, error) {
	// refusal は HTTP 200 で返る。content を読む前に判定する。
	if resp.StopReason == "refusal" {
		if resp.StopDetails.Category == "" {
			return "", errors.New("model refused the request")
		}
		return "", fmt.Errorf("model refused the request: %s", resp.StopDetails.Category)
	}
	// 打ち切られた応答は JSON として壊れている。再送しても同じ結果になるので、
	// スキーマ違反ではなく呼び出しの失敗として返す。
	if resp.StopReason == "max_tokens" {
		return "", errors.New("response truncated: max_tokens reached")
	}

	var b strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("no text block in response (stop_reason=%q)", resp.StopReason)
	}

	return b.String(), nil
}

// statusError は 2xx 以外の応答をエラーにする。
//
// API キーはヘッダにしか載せていないので、応答をそのまま含めても漏れない。
// ただし長さは切る。本文全体を載せるとログが読めなくなる。
func statusError(status int, raw []byte) error {
	var resp apiError
	if err := json.Unmarshal(raw, &resp); err == nil && resp.Error.Message != "" {
		return fmt.Errorf("messages api: %d %s: %s", status, resp.Error.Type, resp.Error.Message)
	}
	return fmt.Errorf("messages api: %d: %s", status, truncate(string(raw), 200))
}

// truncate は s を n 文字（バイトではなくルーン）までに切る。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// apiRequest は POST /v1/messages のリクエストボディ。
//
// thinking は送らない。claude-opus-5 は既定で有効であり、明示的に無効化すると
// 本文に <thinking> タグが混ざることがある。JSON を読み取るこの用途では致命的。
type apiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	System    string       `json:"system,omitempty"`
	Messages  []apiMessage `json:"messages"`
}

type apiMessage struct {
	Role    string         `json:"role"`
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type   string       `json:"type"`
	Text   string       `json:"text,omitempty"`
	Source *imageSource `json:"source,omitempty"`
}

type imageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// apiResponse は必要なフィールドだけを読む。全スキーマは追わない。
type apiResponse struct {
	Content     []responseBlock `json:"content"`
	StopReason  string          `json:"stop_reason"`
	StopDetails struct {
		Category string `json:"category"`
	} `json:"stop_details"`
	// Usage は使ったトークン数（ADR 0031）。
	//
	// 打ち切りや refusal のぶんは呼び出し側に渡らない。Complete がそれらを
	// エラーとして返し、返り値の VisionResponse を捨てるため。**そこまで
	// 拾おうとすると「エラーでも中身を読め」という約束が port に増える。**
	// 自前実装する側の負担を増やさない方を採ってある。
	//
	// キャッシュ関連（cache_creation_input_tokens / cache_read_input_tokens）は
	// 読まない。cache_control を送っていないので常に 0 になる。送るように
	// したときは、input_tokens がキャッシュぶんを含まないことを踏まえて
	// 足し直す必要がある。
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type responseBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// apiError はエラー応答のボディ。
type apiError struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
