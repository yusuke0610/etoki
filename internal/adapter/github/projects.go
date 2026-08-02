// Package github は port.GitHubClient の実装を提供する。
//
// GraphQL を net/http で直接叩く。GitHub SDK を go.mod に持ち込まないのは、
// コアが特定の基盤を意識しないという方針の帰結（ADR 0001）。
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// DefaultBaseURL は GitHub API のホスト。GHE では差し替える。
const DefaultBaseURL = "https://api.github.com"

// envToken はトークンを読む環境変数。
const envToken = "ETOKI_GITHUB_TOKEN"

// defaultTimeout は 1 回の呼び出しを待つ上限。
const defaultTimeout = 30 * time.Second

// maxResponseBytes は応答ボディを読む上限。
const maxResponseBytes = 10 << 20 // 10 MiB

// fieldsPageSize は一度に取得するカスタムフィールドの数。
const fieldsPageSize = 100

// ErrTokenRequired はトークンが設定されていないことを表す。
var ErrTokenRequired = errors.New("etoki: github token is required")

// Config は Client の設定。
type Config struct {
	// BaseURL は GitHub API のホスト。空なら DefaultBaseURL。
	BaseURL string
	// Token は Authorization ヘッダに載せるトークン。必須。
	Token string
	// HTTPClient は差し替え用。nil なら既定のタイムアウトを持つものを作る。
	HTTPClient *http.Client
}

// ConfigFromEnv は ETOKI_GITHUB_TOKEN から設定を読む。
func ConfigFromEnv() Config {
	return Config{Token: os.Getenv(envToken)}
}

// Client は GitHub Projects v2 を GraphQL で操作する。
type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

// New は Config を検証して Client を作る。
func New(cfg Config) (*Client, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("%w: set Config.Token or %s", ErrTokenRequired, envToken)
	}

	base := cfg.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}

	c := &Client{
		endpoint: strings.TrimRight(base, "/") + "/graphql",
		token:    cfg.Token,
		http:     cfg.HTTPClient,
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: defaultTimeout}
	}

	return c, nil
}

// ListProjectFields はプロジェクトのカスタムフィールド定義を返す。
//
// ページングを辿る。途中で打ち切ると、後ろにあるフィールドが「存在しない」
// ことになり、値の設定時に理由の分からない失敗になる。
func (c *Client) ListProjectFields(ctx context.Context, projectID string) ([]port.ProjectField, error) {
	var (
		fields []port.ProjectField
		after  *string
	)

	for {
		var resp fieldsResponse
		vars := map[string]any{"projectId": projectID, "first": fieldsPageSize, "after": after}
		if err := c.do(ctx, queryProjectFields, vars, &resp); err != nil {
			return nil, err
		}

		for _, n := range resp.Node.Fields.Nodes {
			// フィールド以外のノードは id が空で返る。読み飛ばす。
			if n.ID == "" {
				continue
			}

			f := port.ProjectField{ID: n.ID, Name: n.Name, DataType: n.DataType}
			for _, o := range n.Options {
				f.Options = append(f.Options, port.ProjectFieldOption{ID: o.ID, Name: o.Name})
			}
			fields = append(fields, f)
		}

		if !resp.Node.Fields.PageInfo.HasNextPage {
			return fields, nil
		}

		// カーソルが空、あるいは前回から進んでいないなら、辿り続けても同じ
		// ページを取り直すだけで終わらない。打ち切ってエラーにする。
		cursor := resp.Node.Fields.PageInfo.EndCursor
		if cursor == "" || (after != nil && cursor == *after) {
			return nil, fmt.Errorf("github: fields pagination did not advance (cursor %q)", cursor)
		}
		after = &cursor
	}
}

// CreateDraftIssue は draft issue を作成し、その ProjectV2Item ID を返す。
//
// 返すのは ProjectV2Item の ID であって DraftIssue content の ID ではない。
// 後続の SetItemFieldValue が前者を要求する。
func (c *Client) CreateDraftIssue(ctx context.Context, projectID string, item port.DraftIssue) (string, error) {
	if item.Title == "" {
		return "", errors.New("github: draft issue title is required")
	}

	var resp createDraftIssueResponse
	vars := map[string]any{"projectId": projectID, "title": item.Title, "body": item.Body}
	if err := c.do(ctx, mutationCreateDraftIssue, vars, &resp); err != nil {
		return "", err
	}

	id := resp.AddProjectV2DraftIssue.ProjectItem.ID
	if id == "" {
		return "", errors.New("github: draft issue created but no item id returned")
	}

	return id, nil
}

// SetItemFieldValue はアイテムのカスタムフィールドに値を設定する。
func (c *Client) SetItemFieldValue(ctx context.Context, projectID, itemID string, v port.FieldValue) error {
	value, err := fieldValue(v)
	if err != nil {
		return err
	}

	var resp setItemFieldValueResponse
	vars := map[string]any{
		"projectId": projectID,
		"itemId":    itemID,
		"fieldId":   v.FieldID,
		"value":     value,
	}

	return c.do(ctx, mutationSetItemFieldValue, vars, &resp)
}

// fieldValue は FieldValue を GraphQL の ProjectV2FieldValue に詰め替える。
//
// Text と OptionID はどちらか一方だけを設定する約束（port.FieldValue）。
// 両方や片方も無い状態を黙って通すと、意図と違うフィールドが更新される。
func fieldValue(v port.FieldValue) (map[string]any, error) {
	switch {
	case v.FieldID == "":
		return nil, errors.New("github: field id is required")
	case v.Text != nil && v.OptionID != nil:
		return nil, errors.New("github: set either Text or OptionID, not both")
	case v.Text != nil:
		return map[string]any{"text": *v.Text}, nil
	case v.OptionID != nil:
		return map[string]any{"singleSelectOptionId": *v.OptionID}, nil
	default:
		return nil, errors.New("github: either Text or OptionID is required")
	}
}

// do は GraphQL を 1 回叩き、data を out に詰める。
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: vars})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// url.Error は URL を含むが、トークンはヘッダにしか無いので漏れない。
		return fmt.Errorf("call github graphql: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return statusError(resp, raw)
	}

	var envelope graphQLResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// GraphQL は HTTP 200 でもボディに errors を返す。ステータスコードだけを
	// 見て成功と判断すると、何も作られていないのに成功として進んでしまう。
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("github graphql: %s", joinErrors(envelope.Errors))
	}
	if len(envelope.Data) == 0 {
		return errors.New("github graphql: response had no data")
	}

	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}

	return nil
}

// statusError は 2xx 以外の応答をエラーにする。
//
// レート制限は原因が分かるようリセット時刻を添える。トークンはヘッダにしか
// 載せていないので、応答をそのまま含めても漏れない。
func statusError(resp *http.Response, raw []byte) error {
	var msg struct {
		Message string `json:"message"`
	}
	detail := truncate(string(raw), 200)
	if err := json.Unmarshal(raw, &msg); err == nil && msg.Message != "" {
		detail = msg.Message
	}

	if remaining := resp.Header.Get("x-ratelimit-remaining"); remaining == "0" {
		return fmt.Errorf("github api: %d: %s (rate limit resets at %s)",
			resp.StatusCode, detail, resp.Header.Get("x-ratelimit-reset"))
	}

	return fmt.Errorf("github api: %d: %s", resp.StatusCode, detail)
}

// joinErrors は GraphQL のエラーを 1 行にまとめる。
func joinErrors(errs []graphQLError) string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		if e.Type == "" {
			msgs[i] = e.Message
			continue
		}
		msgs[i] = e.Type + ": " + e.Message
	}
	return strings.Join(msgs, "; ")
}

// truncate は s を n ルーンまでに切る。
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}

// graphQLRequest は GraphQL のリクエストボディ。
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// graphQLResponse は GraphQL の応答エンベロープ。
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// queryProjectFields はカスタムフィールド定義を取る。
//
// ProjectV2FieldCommon は id / name / dataType を持つインターフェース。
// options は単一選択フィールドにしか無いので、そちらだけ別に展開する。
const queryProjectFields = `query($projectId: ID!, $first: Int!, $after: String) {
  node(id: $projectId) {
    ... on ProjectV2 {
      fields(first: $first, after: $after) {
        pageInfo { hasNextPage endCursor }
        nodes {
          ... on ProjectV2FieldCommon { id name dataType }
          ... on ProjectV2SingleSelectField { options { id name } }
        }
      }
    }
  }
}`

type fieldsResponse struct {
	Node struct {
		Fields struct {
			PageInfo struct {
				HasNextPage bool   `json:"hasNextPage"`
				EndCursor   string `json:"endCursor"`
			} `json:"pageInfo"`
			Nodes []struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				DataType string `json:"dataType"`
				Options  []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"options"`
			} `json:"nodes"`
		} `json:"fields"`
	} `json:"node"`
}

// mutationCreateDraftIssue は draft issue を作る。
const mutationCreateDraftIssue = `mutation($projectId: ID!, $title: String!, $body: String) {
  addProjectV2DraftIssue(input: {projectId: $projectId, title: $title, body: $body}) {
    projectItem { id }
  }
}`

type createDraftIssueResponse struct {
	AddProjectV2DraftIssue struct {
		ProjectItem struct {
			ID string `json:"id"`
		} `json:"projectItem"`
	} `json:"addProjectV2DraftIssue"`
}

// mutationSetItemFieldValue はアイテムのフィールドに値を設定する。
const mutationSetItemFieldValue = `mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $value: ProjectV2FieldValue!) {
  updateProjectV2ItemFieldValue(input: {projectId: $projectId, itemId: $itemId, fieldId: $fieldId, value: $value}) {
    projectV2Item { id }
  }
}`

type setItemFieldValueResponse struct {
	UpdateProjectV2ItemFieldValue struct {
		ProjectV2Item struct {
			ID string `json:"id"`
		} `json:"projectV2Item"`
	} `json:"updateProjectV2ItemFieldValue"`
}
