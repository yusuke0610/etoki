package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/github"
	"github.com/yusuke0610/etoki/port"
)

const testToken = "ghp_secret_must_not_leak"

func ptr[T any](v T) *T { return &v }

// capturedRequest は 1 回分の GraphQL リクエスト。
type capturedRequest struct {
	Header    http.Header
	Path      string
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// newClient は決められた応答を順に返すサーバーに向いたクライアントを返す。
// 応答を使い切ったら最後のものを返し続ける。
func newClient(t *testing.T, responses ...string) (*github.Client, *[]capturedRequest) {
	t.Helper()

	var got []capturedRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)

		req := capturedRequest{Header: r.Header.Clone(), Path: r.URL.Path}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("リクエストが JSON ではない: %v", err)
		}
		got = append(got, req)

		i := min(len(got)-1, len(responses)-1)
		_, _ = io.WriteString(w, responses[i])
	}))
	t.Cleanup(srv.Close)

	c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	return c, &got
}

func TestNew_RequiresToken(t *testing.T) {
	t.Parallel()

	_, err := github.New(github.Config{})
	if !errors.Is(err, github.ErrTokenRequired) {
		t.Fatalf("New() = %v, want ErrTokenRequired", err)
	}
}

func TestListProjectFields(t *testing.T) {
	t.Parallel()

	body := `{"data":{"node":{"fields":{
		"pageInfo":{"hasNextPage":false,"endCursor":"c1"},
		"nodes":[
			{"id":"F_title","name":"Title","dataType":"TITLE"},
			{"id":"F_parent","name":"Parent","dataType":"TEXT"},
			{"id":"F_kind","name":"Kind","dataType":"SINGLE_SELECT",
			 "options":[{"id":"O_epic","name":"epic"},{"id":"O_issue","name":"issue"}]}
		]}}}}`

	c, got := newClient(t, body)

	fields, err := c.ListProjectFields(t.Context(), "PVT_1")
	if err != nil {
		t.Fatalf("ListProjectFields() = %v", err)
	}

	if len(fields) != 3 {
		t.Fatalf("len(fields) = %d, want 3", len(fields))
	}

	kind := fields[2]
	if kind.ID != "F_kind" || kind.DataType != "SINGLE_SELECT" {
		t.Errorf("fields[2] = %+v", kind)
	}
	if len(kind.Options) != 2 {
		t.Fatalf("len(Options) = %d, want 2", len(kind.Options))
	}
	if kind.Options[0].ID != "O_epic" || kind.Options[0].Name != "epic" {
		t.Errorf("Options[0] = %+v", kind.Options[0])
	}
	// 単一選択でないフィールドに選択肢は付かない。
	if len(fields[1].Options) != 0 {
		t.Errorf("TEXT フィールドに選択肢が付いている: %+v", fields[1].Options)
	}

	req := (*got)[0]
	if req.Path != "/graphql" {
		t.Errorf("path = %q, want /graphql", req.Path)
	}
	if want := "Bearer " + testToken; req.Header.Get("authorization") != want {
		t.Errorf("authorization = %q", req.Header.Get("authorization"))
	}
	if req.Variables["projectId"] != "PVT_1" {
		t.Errorf("projectId = %v", req.Variables["projectId"])
	}
	// 単一選択の選択肢まで取らないと、種別フィールドの値を決められない。
	if !strings.Contains(req.Query, "ProjectV2SingleSelectField") ||
		!strings.Contains(req.Query, "options") {
		t.Errorf("クエリが選択肢を取っていない:\n%s", req.Query)
	}
}

// 途中で打ち切ると、後ろのフィールドが「存在しない」ことになる。
func TestListProjectFields_FollowsPagination(t *testing.T) {
	t.Parallel()

	page1 := `{"data":{"node":{"fields":{
		"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"},
		"nodes":[{"id":"F_1","name":"One","dataType":"TEXT"}]}}}}`
	page2 := `{"data":{"node":{"fields":{
		"pageInfo":{"hasNextPage":false,"endCursor":"cursor-2"},
		"nodes":[{"id":"F_2","name":"Two","dataType":"TEXT"}]}}}}`

	c, got := newClient(t, page1, page2)

	fields, err := c.ListProjectFields(t.Context(), "PVT_1")
	if err != nil {
		t.Fatalf("ListProjectFields() = %v", err)
	}

	if len(fields) != 2 {
		t.Fatalf("len(fields) = %d, want 2", len(fields))
	}
	if fields[0].ID != "F_1" || fields[1].ID != "F_2" {
		t.Errorf("fields = %+v", fields)
	}

	if len(*got) != 2 {
		t.Fatalf("呼び出し回数 = %d, want 2", len(*got))
	}
	if (*got)[0].Variables["after"] != nil {
		t.Errorf("1 回目の after = %v, want null", (*got)[0].Variables["after"])
	}
	if (*got)[1].Variables["after"] != "cursor-1" {
		t.Errorf("2 回目の after = %v, want cursor-1", (*got)[1].Variables["after"])
	}
}

// カーソルが進まないのに次があると言われたら、辿り続けても終わらない。
func TestListProjectFields_StopsWhenPaginationDoesNotAdvance(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		responses []string
		wantCalls int
	}{
		"カーソルが空": {
			responses: []string{`{"data":{"node":{"fields":{
				"pageInfo":{"hasNextPage":true,"endCursor":""},
				"nodes":[{"id":"F_1","name":"One","dataType":"TEXT"}]}}}}`},
			wantCalls: 1,
		},
		"カーソルが同じまま": {
			responses: []string{`{"data":{"node":{"fields":{
				"pageInfo":{"hasNextPage":true,"endCursor":"stuck"},
				"nodes":[{"id":"F_1","name":"One","dataType":"TEXT"}]}}}}`},
			// 1 回目でカーソルを受け取り、2 回目で進んでいないと分かる。
			wantCalls: 2,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, got := newClient(t, tt.responses...)

			if _, err := c.ListProjectFields(t.Context(), "PVT_1"); err == nil {
				t.Fatal("ListProjectFields() = nil, want error")
			}
			if len(*got) != tt.wantCalls {
				t.Errorf("呼び出し回数 = %d, want %d", len(*got), tt.wantCalls)
			}
		})
	}
}

// ctx を切ったら呼び出しを打ち切る。長い処理を止められないと困る。
func TestHonorsContextCancellation(t *testing.T) {
	t.Parallel()

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

	c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err = c.ListProjectFields(ctx, "PVT_1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ListProjectFields() = %v, want context.Canceled", err)
	}
}

func TestCreateDraftIssue(t *testing.T) {
	t.Parallel()

	body := `{"data":{"addProjectV2DraftIssue":{"projectItem":{"id":"PVTI_item1"}}}}`
	c, got := newClient(t, body)

	itemID, err := c.CreateDraftIssue(t.Context(), "PVT_1",
		port.DraftIssue{Title: "決済フローの見直し", Body: "全体の方針"})
	if err != nil {
		t.Fatalf("CreateDraftIssue() = %v", err)
	}

	// 返すのは ProjectV2Item の ID。DraftIssue content の ID ではない。
	if itemID != "PVTI_item1" {
		t.Errorf("itemID = %q, want PVTI_item1", itemID)
	}

	req := (*got)[0]
	if !strings.Contains(req.Query, "addProjectV2DraftIssue") {
		t.Errorf("クエリが違う:\n%s", req.Query)
	}
	if !strings.Contains(req.Query, "projectItem") {
		t.Errorf("projectItem の id を取っていない:\n%s", req.Query)
	}
	if req.Variables["projectId"] != "PVT_1" {
		t.Errorf("projectId = %v", req.Variables["projectId"])
	}
	if req.Variables["title"] != "決済フローの見直し" {
		t.Errorf("title = %v", req.Variables["title"])
	}
	if req.Variables["body"] != "全体の方針" {
		t.Errorf("body = %v", req.Variables["body"])
	}
}

func TestCreateDraftIssue_Errors(t *testing.T) {
	t.Parallel()

	t.Run("タイトルが空", func(t *testing.T) {
		t.Parallel()

		c, got := newClient(t, `{"data":{}}`)

		if _, err := c.CreateDraftIssue(t.Context(), "PVT_1", port.DraftIssue{}); err == nil {
			t.Fatal("CreateDraftIssue() = nil, want error")
		}
		if len(*got) != 0 {
			t.Error("タイトルが空なのに送信している")
		}
	})

	t.Run("item id が返らない", func(t *testing.T) {
		t.Parallel()

		c, _ := newClient(t, `{"data":{"addProjectV2DraftIssue":{"projectItem":{"id":""}}}}`)

		if _, err := c.CreateDraftIssue(t.Context(), "PVT_1", port.DraftIssue{Title: "t"}); err == nil {
			t.Fatal("CreateDraftIssue() = nil, want error")
		}
	})
}

func TestSetItemFieldValue(t *testing.T) {
	t.Parallel()

	body := `{"data":{"updateProjectV2ItemFieldValue":{"projectV2Item":{"id":"PVTI_item1"}}}}`

	tests := []struct {
		name  string
		value port.FieldValue
		want  map[string]any
	}{
		{
			name:  "テキスト",
			value: port.FieldValue{FieldID: "F_parent", Text: ptr("e1")},
			want:  map[string]any{"text": "e1"},
		},
		{
			name:  "単一選択",
			value: port.FieldValue{FieldID: "F_kind", OptionID: ptr("O_epic")},
			want:  map[string]any{"singleSelectOptionId": "O_epic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, got := newClient(t, body)

			if err := c.SetItemFieldValue(t.Context(), "PVT_1", "PVTI_item1", tt.value); err != nil {
				t.Fatalf("SetItemFieldValue() = %v", err)
			}

			req := (*got)[0]
			if !strings.Contains(req.Query, "updateProjectV2ItemFieldValue") {
				t.Errorf("クエリが違う:\n%s", req.Query)
			}
			if req.Variables["itemId"] != "PVTI_item1" {
				t.Errorf("itemId = %v", req.Variables["itemId"])
			}
			if req.Variables["fieldId"] != tt.value.FieldID {
				t.Errorf("fieldId = %v", req.Variables["fieldId"])
			}

			value, _ := req.Variables["value"].(map[string]any)
			if len(value) != len(tt.want) {
				t.Fatalf("value = %v, want %v", value, tt.want)
			}
			for k, want := range tt.want {
				if value[k] != want {
					t.Errorf("value[%q] = %v, want %v", k, value[k], want)
				}
			}
		})
	}
}

// 黙って通すと、意図と違うフィールドが更新される。
func TestSetItemFieldValue_RejectsInvalidValue(t *testing.T) {
	t.Parallel()

	tests := map[string]port.FieldValue{
		"両方指定":        {FieldID: "F_1", Text: ptr("a"), OptionID: ptr("O_1")},
		"どちらも指定なし":    {FieldID: "F_1"},
		"フィールド ID なし": {Text: ptr("a")},
	}

	for name, v := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, got := newClient(t, `{"data":{}}`)

			if err := c.SetItemFieldValue(t.Context(), "PVT_1", "PVTI_1", v); err == nil {
				t.Fatal("SetItemFieldValue() = nil, want error")
			}
			if len(*got) != 0 {
				t.Error("不正な値なのに送信している")
			}
		})
	}
}

// GraphQL は HTTP 200 でもボディに errors を返す。ステータスコードだけを見て
// 成功と判断すると、何も作られていないのに成功として進んでしまう。
func TestGraphQLErrorsOnHTTP200(t *testing.T) {
	t.Parallel()

	body := `{"data":null,"errors":[
		{"type":"NOT_FOUND","message":"Could not resolve to a node with the global id of 'PVT_x'"},
		{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`

	c, _ := newClient(t, body)

	_, err := c.CreateDraftIssue(t.Context(), "PVT_x", port.DraftIssue{Title: "t"})
	if err == nil {
		t.Fatal("CreateDraftIssue() = nil, want error")
	}
	for _, want := range []string{"NOT_FOUND", "Could not resolve", "FORBIDDEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラーに %q が含まれない: %v", want, err)
		}
	}
}

func TestHTTPErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		headers map[string]string
		body    string
		want    []string
	}{
		{
			name:   "401 認証エラー",
			status: http.StatusUnauthorized,
			body:   `{"message":"Bad credentials"}`,
			want:   []string{"401", "Bad credentials"},
		},
		{
			name:    "403 レート制限",
			status:  http.StatusForbidden,
			headers: map[string]string{"x-ratelimit-remaining": "0", "x-ratelimit-reset": "1780000000"},
			body:    `{"message":"API rate limit exceeded"}`,
			want:    []string{"403", "rate limit resets at", "1780000000"},
		},
		{
			// 二次レート制限。残数は残ったまま retry-after だけが付く。
			name:    "403 二次レート制限",
			status:  http.StatusForbidden,
			headers: map[string]string{"x-ratelimit-remaining": "4999", "retry-after": "60"},
			body:    `{"message":"You have exceeded a secondary rate limit"}`,
			want:    []string{"403", "retry-after: 60"},
		},
		{
			// レート制限ではない 403。招待されただけでリポジトリに権限が無い
			// 利用者はここに来る（ADR 0017）。
			name:   "403 権限不足",
			status: http.StatusForbidden,
			body:   `{"message":"Resource not accessible by integration"}`,
			want:   []string{"403", "Resource not accessible"},
		},
		{
			name:   "502 JSON ではない応答",
			status: http.StatusBadGateway,
			body:   `<html>502 Bad Gateway</html>`,
			want:   []string{"502", "Bad Gateway"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tt.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)

			c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}

			_, err = c.ListProjectFields(t.Context(), "PVT_1")
			if err == nil {
				t.Fatal("ListProjectFields() = nil, want error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("エラーに %q が含まれない: %v", want, err)
				}
			}

			// 401 だけは sentinel に寄せる。文字列で判定させると、UI が
			// 「再ログインが要る」を見分けられない（ADR 0015）。
			if got := errors.Is(err, port.ErrNotAuthenticated); got != (tt.status == http.StatusUnauthorized) {
				t.Errorf("errors.Is(err, ErrNotAuthenticated) = %v (status %d): %v",
					got, tt.status, err)
			}

			// 403 も sentinel に寄せるが、レート制限は含めない。待てば通るものを
			// 「権限がありません」と見せない（ADR 0017）。
			wantForbidden := tt.status == http.StatusForbidden &&
				tt.headers["x-ratelimit-remaining"] != "0" &&
				tt.headers["retry-after"] == ""
			if got := errors.Is(err, port.ErrForbidden); got != wantForbidden {
				t.Errorf("errors.Is(err, ErrForbidden) = %v, want %v: %v",
					got, wantForbidden, err)
			}
		})
	}
}

// トークンがエラーに混ざると、ログや画面に出た時点で漏れる。
func TestDoesNotLeakToken(t *testing.T) {
	t.Parallel()

	tests := map[string]http.HandlerFunc{
		"認証エラー": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Bad credentials"}`)
		},
		"GraphQL エラー": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"data":null,"errors":[{"message":"nope"}]}`)
		},
		"壊れた応答": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `not json`)
		},
	}

	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(handler)
			t.Cleanup(srv.Close)

			c, err := github.New(github.Config{BaseURL: srv.URL, Token: testToken})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}

			_, err = c.CreateDraftIssue(t.Context(), "PVT_1", port.DraftIssue{Title: "t"})
			if err == nil {
				t.Fatal("CreateDraftIssue() = nil, want error")
			}
			if strings.Contains(err.Error(), testToken) {
				t.Errorf("エラーにトークンが含まれている: %v", err)
			}
		})
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("ETOKI_GITHUB_TOKEN", "ghp_from_env")

	if got := github.ConfigFromEnv(); got.Token != "ghp_from_env" {
		t.Errorf("Token = %q", got.Token)
	}
}

// Client が port.GitHubClient を満たすことを固定する。
var _ port.GitHubClient = (*github.Client)(nil)

// ---------------------------------------------------------------------------
// 作成先の候補一覧（ADR 0014）
// ---------------------------------------------------------------------------

func TestListRepositories(t *testing.T) {
	t.Parallel()

	body := `{"data":{"viewer":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":"c1"},
		"nodes":[
			{"name":"web","description":"フロント","owner":{"login":"acme"}},
			{"name":"api","description":"","owner":{"login":"other"}}
		]}}}}`

	c, got := newClient(t, body)

	repos, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}

	want := []port.Repository{
		{Owner: "acme", Name: "web", Description: "フロント"},
		{Owner: "other", Name: "api"},
	}
	if len(repos) != len(want) {
		t.Fatalf("len(repos) = %d, want %d (%+v)", len(repos), len(want), repos)
	}
	for i := range want {
		if repos[i] != want[i] {
			t.Errorf("repos[%d] = %+v, want %+v", i, repos[i], want[i])
		}
	}

	if (*got)[0].Path != "/graphql" {
		t.Errorf("path = %q, want /graphql", (*got)[0].Path)
	}
}

// アーカイブ済みを落とすのはクエリ側の仕事。取ってから捨てると、1 ページの枠を
// 選択肢にならないもので埋めてしまう。応答に isArchived が入らないので、
// 絞り込みが消えたことは取得結果からは分からない。クエリを直に見る。
func TestListRepositories_FiltersArchivedInQuery(t *testing.T) {
	t.Parallel()

	body := `{"data":{"viewer":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":"c1"},
		"nodes":[]}}}}`

	c, got := newClient(t, body)

	if _, err := c.ListRepositories(t.Context()); err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}

	if q := (*got)[0].Query; !strings.Contains(q, "isArchived: false") {
		t.Errorf("クエリがアーカイブ済みを除外していない:\n%s", q)
	}
}

func TestListRepositories_FollowsPagination(t *testing.T) {
	t.Parallel()

	page1 := `{"data":{"viewer":{"repositories":{
		"pageInfo":{"hasNextPage":true,"endCursor":"cursor-1"},
		"nodes":[{"name":"one","owner":{"login":"acme"}}]}}}}`
	page2 := `{"data":{"viewer":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":"cursor-2"},
		"nodes":[{"name":"two","owner":{"login":"acme"}}]}}}}`

	c, got := newClient(t, page1, page2)

	repos, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	if (*got)[1].Variables["after"] != "cursor-1" {
		t.Errorf("2 回目の after = %v, want cursor-1", (*got)[1].Variables["after"])
	}
}

// カーソルが進まないと、辿り続けても同じページを取り直すだけで終わらない。
func TestListRepositories_StopsWhenCursorDoesNotAdvance(t *testing.T) {
	t.Parallel()

	stuck := `{"data":{"viewer":{"repositories":{
		"pageInfo":{"hasNextPage":true,"endCursor":""},
		"nodes":[{"name":"one","owner":{"login":"acme"}}]}}}}`

	c, _ := newClient(t, stuck)

	if _, err := c.ListRepositories(t.Context()); err == nil {
		t.Fatal("ListRepositories() = nil, want error")
	}
}

func TestListRepositoryProjects(t *testing.T) {
	t.Parallel()

	body := `{"data":{"repository":{"projectsV2":{
		"pageInfo":{"hasNextPage":false,"endCursor":"c1"},
		"nodes":[
			{"id":"PVT_1","number":1,"title":"ロードマップ","closed":false},
			{"id":"PVT_2","number":2,"title":"終わったやつ","closed":true}
		]}}}}`

	c, got := newClient(t, body)

	projects, err := c.ListRepositoryProjects(t.Context(), "acme", "web")
	if err != nil {
		t.Fatalf("ListRepositoryProjects() = %v", err)
	}

	// 閉じた Project は落とす。draft issue の置き場所として選ばせる意味が無い。
	if len(projects) != 1 {
		t.Fatalf("len(projects) = %d, want 1 (%+v)", len(projects), projects)
	}
	want := port.Project{ID: "PVT_1", Number: 1, Title: "ロードマップ"}
	if projects[0] != want {
		t.Errorf("projects[0] = %+v, want %+v", projects[0], want)
	}

	vars := (*got)[0].Variables
	if vars["owner"] != "acme" || vars["name"] != "web" {
		t.Errorf("variables = %+v, want owner=acme name=web", vars)
	}

	// 書けない Project を候補に出さないのはクエリ側の仕事。応答には権限が
	// 入らないので、絞り込みが消えたことは取得結果からは分からない。
	if q := (*got)[0].Query; !strings.Contains(q, "minPermissionLevel: WRITE") {
		t.Errorf("クエリが書き込み権限で絞っていない:\n%s", q)
	}
}

func TestListRepositoryProjects_RequiresOwnerAndName(t *testing.T) {
	t.Parallel()

	c, got := newClient(t, `{"data":{}}`)

	if _, err := c.ListRepositoryProjects(t.Context(), "acme", ""); err == nil {
		t.Fatal("ListRepositoryProjects() = nil, want error")
	}
	// 入口で弾く。空のまま投げると GitHub 側のエラーとして返り、原因が遠くなる。
	if len(*got) != 0 {
		t.Errorf("リクエストを送っている: %d 回", len(*got))
	}
}

// ---------------------------------------------------------------------------
// GitHub App モードのリポジトリ一覧（ADR 0015）
// ---------------------------------------------------------------------------

// countingTokens は呼ばれた回数を数える GitHubTokenSource。
//
// 利用者ごとにトークンが変わる構成では、リクエストのたびに引き直す必要がある。
// 1 回だけ引いて使い回すと、更新後も古いトークンを送り続ける。
type countingTokens struct {
	mu    sync.Mutex
	token string
	calls int
}

func (t *countingTokens) Token(context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.calls++
	return t.token, nil
}

func (t *countingTokens) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.calls
}

// restPage は all のうち page 番目のページを返す。GitHub の per_page / page と
// 同じ切り方にしないと、辿り方の誤りをテストが検知できない。
func restPage[T any](all []T, per, num int) []T {
	start := (num - 1) * per
	if start >= len(all) {
		return nil
	}
	return all[start:min(start+per, len(all))]
}

// installedRepo はインストール配下のリポジトリ 1 件分の応答。
type installedRepo struct {
	name     string
	owner    string
	archived bool
}

// newAppClient は installations と repositories を返すサーバーに向いた
// ModeApp のクライアントを返す。
//
// repos はインストール ID をキーにする。ページングは per_page と page を見て
// サーバー側で切る。実物と同じ切り方にしないと、辿り方の誤りを検知できない。
func newAppClient(
	t *testing.T, installIDs []int64, repos map[int64][]installedRepo,
) (*github.Client, *countingTokens) {
	t.Helper()

	tokens := &countingTokens{token: testToken}

	page := func(r *http.Request) (per, num int) {
		per, num = 100, 1
		if v := r.URL.Query().Get("per_page"); v != "" {
			per, _ = strconv.Atoi(v)
		}
		if v := r.URL.Query().Get("page"); v != "" {
			num, _ = strconv.Atoi(v)
		}
		return per, num
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := "Bearer " + testToken; r.Header.Get("authorization") != want {
			t.Errorf("authorization = %q", r.Header.Get("authorization"))
		}

		per, num := page(r)

		if r.URL.Path == "/user/installations" {
			var out []map[string]any
			for _, id := range restPage(installIDs, per, num) {
				out = append(out, map[string]any{"id": id})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"installations": out})
			return
		}

		var id int64
		if _, err := fmt.Sscanf(r.URL.Path, "/user/installations/%d/repositories", &id); err != nil {
			t.Errorf("想定しないパス: %s", r.URL.Path)
		}

		var out []map[string]any
		for _, rp := range restPage(repos[id], per, num) {
			out = append(out, map[string]any{
				"name":     rp.name,
				"archived": rp.archived,
				"owner":    map[string]any{"login": rp.owner},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": out})
	}))
	t.Cleanup(srv.Close)

	c, err := github.New(github.Config{
		BaseURL: srv.URL, TokenSource: tokens, Mode: github.ModeApp,
	})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}

	return c, tokens
}

// インストールが 1 ページに収まらない利用者がいる。1 ページ目で打ち切ると、
// 超えた分のインストールのリポジトリが黙って候補から消える。
func TestListRepositories_AppModeFollowsInstallationPages(t *testing.T) {
	t.Parallel()

	// 100 で 1 ページが埋まり、101 個目が 2 ページ目に来る。
	const installs = 101

	ids := make([]int64, installs)
	repos := make(map[int64][]installedRepo, installs)
	for i := range installs {
		id := int64(i + 1)
		ids[i] = id
		repos[id] = []installedRepo{{name: fmt.Sprintf("repo-%d", id), owner: "acme"}}
	}

	c, tokens := newAppClient(t, ids, repos)

	got, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(got) != installs {
		t.Fatalf("len(repos) = %d, want %d", len(got), installs)
	}
	// 2 ページ目のインストールが含まれること。ここが落ちるなら一覧を
	// 切り詰めている。
	if got[installs-1].Name != "repo-101" {
		t.Errorf("最後の要素 = %+v, want repo-101", got[installs-1])
	}

	// トークンはリクエストのたびに引く。1 回で使い回すと、更新後も古い
	// トークンを送り続ける。
	if tokens.count() < installs {
		t.Errorf("Token() = %d 回, want >= %d", tokens.count(), installs)
	}
}

func TestListRepositories_AppModeSkipsArchived(t *testing.T) {
	t.Parallel()

	c, _ := newAppClient(t, []int64{7}, map[int64][]installedRepo{
		7: {
			{name: "web", owner: "acme"},
			{name: "old", owner: "acme", archived: true},
			{name: "api", owner: "other"},
		},
	})

	got, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}

	// この REST にはアーカイブ済みを除くパラメータが無いので、取ってから捨てる。
	want := []port.Repository{{Owner: "acme", Name: "web"}, {Owner: "other", Name: "api"}}
	if len(got) != len(want) {
		t.Fatalf("len(repos) = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repos[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 選択肢として見せるものなので、全部を取り切らずに打ち切る。
func TestListRepositories_AppModeStopsAtMaxRepositories(t *testing.T) {
	t.Parallel()

	// **インストールをまたいで数える。** 1 つに 700 件入れると、上限が
	// インストールごとにリセットする実装でも通ってしまう。3 つに分けて
	// 合計で 700 件にすれば、通算で止まることまで縛れる。
	ids := []int64{1, 2, 3}
	repos := make(map[int64][]installedRepo, len(ids))
	for _, id := range ids {
		batch := make([]installedRepo, 300)
		for i := range batch {
			batch[i] = installedRepo{name: fmt.Sprintf("repo-%d-%d", id, i), owner: "acme"}
		}
		repos[id] = batch
	}

	c, _ := newAppClient(t, ids, repos)

	got, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(got) != 500 {
		t.Errorf("len(repos) = %d, want 500", len(got))
	}
}

// GraphQL は権限で拒むとき HTTP 200 のボディに errors を載せる。ステータスだけを
// 見ていると、権限の問題が 500 に落ちる（ADR 0017）。
func TestGraphQLForbiddenIsSentinel(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t,
		`{"errors":[{"type":"FORBIDDEN","message":"Resource not accessible"}]}`)

	_, err := c.CanWriteProject(t.Context(), "PVT_1")
	if !errors.Is(err, port.ErrForbidden) {
		t.Fatalf("CanWriteProject() = %v, want port.ErrForbidden", err)
	}
}

// 作成できるかは表示のために引く。判定には使わない（ADR 0017）。
func TestCanWriteProject(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		response string
		want     bool
	}{
		"書ける":  {`{"data":{"node":{"viewerCanUpdate":true}}}`, true},
		"書けない": {`{"data":{"node":{"viewerCanUpdate":false}}}`, false},
		// 辿れない ID は node が null で返る。招待されただけでリポジトリに
		// 権限が無い利用者はこれを受け取る（ADR 0017）。表示は「書けない」で
		// よい。エラーにすると、権限が無いことを障害として見せてしまう。
		"辿れない": {`{"data":{"node":null}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, _ := newClient(t, tt.response)

			got, err := c.CanWriteProject(t.Context(), "PVT_1")
			if err != nil {
				t.Fatalf("CanWriteProject() = %v", err)
			}
			if got != tt.want {
				t.Errorf("CanWriteProject() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanWriteProject_RequiresProjectID(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t)

	if _, err := c.CanWriteProject(t.Context(), ""); err == nil {
		t.Fatal("CanWriteProject(\"\") = nil, want error")
	}
}
