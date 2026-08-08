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

// listPageSize は一度に取得するリポジトリ / Project の数。
const listPageSize = 100

// maxRepositories は一覧で辿るリポジトリ数の上限。
//
// 選択肢として画面に出すものなので、全部を取り切ることに意味が無い。
// 上限を設けないと、所属組織の多い利用者で一覧の取得だけが延々と続く。
const maxRepositories = 500

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

// ListRepositories は利用者が関われるリポジトリを返す。
//
// アーカイブ済みは落とす。選択肢として出しても選ばせる意味が無い。
// トークンに repo の read が無いと 0 件になるが、権限不足と「本当に 1 つも
// 無い」を GraphQL の応答からは区別できない。案内は呼び出し側に任せる。
func (c *Client) ListRepositories(ctx context.Context) ([]port.Repository, error) {
	var (
		repos []port.Repository
		after *string
	)

	for {
		var resp repositoriesResponse
		vars := map[string]any{"first": listPageSize, "after": after}
		if err := c.do(ctx, queryViewerRepositories, vars, &resp); err != nil {
			return nil, err
		}

		for _, n := range resp.Viewer.Repositories.Nodes {
			if n.IsArchived {
				continue
			}
			repos = append(repos, port.Repository{
				Owner:       n.Owner.Login,
				Name:        n.Name,
				Description: n.Description,
			})
		}

		// 上限に達したらそこで返す。選択肢として見せるものなので、全件を
		// 取り切る必要が無い。
		if len(repos) >= maxRepositories {
			return repos[:maxRepositories], nil
		}

		page := resp.Viewer.Repositories.PageInfo
		next, err := nextCursor("repositories", page.HasNextPage, page.EndCursor, after)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return repos, nil
		}
		after = next
	}
}

// ListRepositoryProjects はリポジトリに紐づく Projects v2 を返す。
//
// 閉じた Project は落とす。draft issue を入れる先として選ばせる意味が無い。
func (c *Client) ListRepositoryProjects(
	ctx context.Context, owner, name string,
) ([]port.Project, error) {
	if owner == "" || name == "" {
		return nil, errors.New("github: repository owner and name are required")
	}

	var (
		projects []port.Project
		after    *string
	)

	for {
		var resp repositoryProjectsResponse
		vars := map[string]any{"owner": owner, "name": name, "first": listPageSize, "after": after}
		if err := c.do(ctx, queryRepositoryProjects, vars, &resp); err != nil {
			return nil, err
		}

		for _, n := range resp.Repository.ProjectsV2.Nodes {
			if n.Closed {
				continue
			}
			projects = append(projects, port.Project{ID: n.ID, Number: n.Number, Title: n.Title})
		}

		page := resp.Repository.ProjectsV2.PageInfo
		next, err := nextCursor("projects", page.HasNextPage, page.EndCursor, after)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return projects, nil
		}
		after = next
	}
}

// nextCursor は次のページのカーソルを返す。次が無ければ (nil, nil)。
//
// カーソルが空、あるいは前回から進んでいないなら、辿り続けても同じページを
// 取り直すだけで終わらない。打ち切ってエラーにする。
func nextCursor(what string, hasNext bool, endCursor string, after *string) (*string, error) {
	if !hasNext {
		return nil, nil
	}
	if endCursor == "" || (after != nil && endCursor == *after) {
		return nil, fmt.Errorf("github: %s pagination did not advance (cursor %q)", what, endCursor)
	}
	return &endCursor, nil
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

		page := resp.Node.Fields.PageInfo
		next, err := nextCursor("fields", page.HasNextPage, page.EndCursor, after)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return fields, nil
		}
		after = next
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

// queryViewerRepositories はトークンの持ち主が関われるリポジトリを取る。
//
// affiliations を絞らないと、star したリポジトリまで混ざる。並びは push が
// 新しい順。直近さわっているものほど選びたい対象である可能性が高い。
const queryViewerRepositories = `query($first: Int!, $after: String) {
  viewer {
    repositories(
      first: $first
      after: $after
      affiliations: [OWNER, COLLABORATOR, ORGANIZATION_MEMBER]
      orderBy: {field: PUSHED_AT, direction: DESC}
    ) {
      pageInfo { hasNextPage endCursor }
      nodes { name description isArchived owner { login } }
    }
  }
}`

type repositoriesResponse struct {
	Viewer struct {
		Repositories struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				IsArchived  bool   `json:"isArchived"`
				Owner       struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"nodes"`
		} `json:"repositories"`
	} `json:"viewer"`
}

// queryRepositoryProjects はリポジトリに紐づく Projects v2 を取る。
//
// draft issue の作成先はリポジトリではなく Project（ADR 0014）。番号順に
// 並べるのは、GitHub の URL に出る番号と一覧の並びを揃えるため。
const queryRepositoryProjects = `query($owner: String!, $name: String!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    projectsV2(first: $first, after: $after, orderBy: {field: NUMBER, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes { id number title closed }
    }
  }
}`

type repositoryProjectsResponse struct {
	Repository struct {
		ProjectsV2 struct {
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Title  string `json:"title"`
				Closed bool   `json:"closed"`
			} `json:"nodes"`
		} `json:"projectsV2"`
	} `json:"repository"`
}

// pageInfo は GraphQL の Relay ページング情報。
type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
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
			PageInfo pageInfo `json:"pageInfo"`
			Nodes    []struct {
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
