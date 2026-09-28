package fakeupstream_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/github"
	"github.com/yusuke0610/etoki/internal/fakeupstream"
	"github.com/yusuke0610/etoki/port"
)

func newGitHub(t *testing.T) (*github.Client, *fakeupstream.Server, string) {
	t.Helper()

	fake := fakeupstream.New()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	c, err := github.New(github.Config{BaseURL: srv.URL, Token: "fake"})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	return c, fake, srv.URL
}

func fieldByName(t *testing.T, fields []port.ProjectField, name string) port.ProjectField {
	t.Helper()
	for _, f := range fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("フィールド %q が無い: %+v", name, fields)
	return port.ProjectField{}
}

// 本物のアダプタを偽物に向け、通しの確認で踏む操作をすべて通す。
//
// 守っているのは「アダプタのクエリと偽物の答え方がずれていないこと」。
// アダプタのクエリを変えて偽物が答えられなくなると、手元の通しの確認が
// 最初の画面で止まる。それをここで先に落とす。
func TestGitHubAdapterRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, fake, base := newGitHub(t)

	list, err := c.ListRepositories(ctx)
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	// 打ち切りの有無も候補と一緒に返る（ADR 0054）。偽物は 1 件しか持たない
	// ので、取り切ったと言っているかもここで見る。
	repos := list.Repositories
	if len(repos) != 1 || repos[0].Owner != fakeupstream.RepositoryOwner || repos[0].Name != fakeupstream.RepositoryName || list.Truncated {
		t.Fatalf("ListRepositories() = %+v", list)
	}

	projects, err := c.ListRepositoryProjects(ctx, repos[0].Owner, repos[0].Name)
	if err != nil {
		t.Fatalf("ListRepositoryProjects() = %v", err)
	}
	if len(projects) != 1 || projects[0].ID != fakeupstream.ProjectID || projects[0].URL != fakeupstream.ProjectURL {
		t.Fatalf("ListRepositoryProjects() = %+v", projects)
	}

	if ok, err := c.CanWriteProject(ctx, fakeupstream.ProjectID); err != nil || !ok {
		t.Fatalf("CanWriteProject() = %v, %v", ok, err)
	}

	fields, err := c.ListProjectFields(ctx, fakeupstream.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectFields() = %v", err)
	}
	kind := fieldByName(t, fields, "Kind")
	parent := fieldByName(t, fields, "Parent")
	if kind.DataType != "SINGLE_SELECT" || len(kind.Options) != 2 || parent.DataType != "TEXT" {
		t.Fatalf("フィールド定義: Kind=%+v Parent=%+v", kind, parent)
	}
	optionID := map[string]string{}
	for _, o := range kind.Options {
		optionID[o.Name] = o.ID
	}

	epicID, err := c.CreateDraftIssue(ctx, fakeupstream.ProjectID, port.DraftIssue{Title: "epic", Body: "e"})
	if err != nil {
		t.Fatalf("CreateDraftIssue(epic) = %v", err)
	}
	issueID, err := c.CreateDraftIssue(ctx, fakeupstream.ProjectID, port.DraftIssue{Title: "issue", Body: "i"})
	if err != nil {
		t.Fatalf("CreateDraftIssue(issue) = %v", err)
	}

	set := func(itemID string, v port.FieldValue) {
		t.Helper()
		if err := c.SetItemFieldValue(ctx, fakeupstream.ProjectID, itemID, v); err != nil {
			t.Fatalf("SetItemFieldValue(%s, %s) = %v", itemID, v.FieldID, err)
		}
	}
	epicOpt, issueOpt, parentTitle := optionID["epic"], optionID["issue"], "epic"
	set(epicID, port.FieldValue{FieldID: kind.ID, OptionID: &epicOpt})
	set(issueID, port.FieldValue{FieldID: kind.ID, OptionID: &issueOpt})
	set(issueID, port.FieldValue{FieldID: parent.ID, Text: &parentTitle})

	if err := c.UpdateDraftIssue(ctx, issueID, port.DraftIssue{Title: "issue v2", Body: "i2"}); err != nil {
		t.Fatalf("UpdateDraftIssue() = %v", err)
	}

	want := []fakeupstream.Item{
		{ID: epicID, Title: "epic", Body: "e", Kind: "epic"},
		{ID: issueID, Title: "issue v2", Body: "i2", Kind: "issue", Parent: "epic", Updates: 1},
	}
	assertItems(t, fake.Items(), want)

	// 画面の外で確かめる口も同じものを返す。
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/_fake/items", nil)
	if err != nil {
		t.Fatalf("NewRequest = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /_fake/items = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /_fake/items status = %d", resp.StatusCode)
	}
	var got []fakeupstream.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode = %v", err)
	}
	assertItems(t, got, want)
}

func assertItems(t *testing.T, got, want []fakeupstream.Item) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("items = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("items[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 偽物に無い item の更新を黙って通すと、DB を使い回して記録と偽物が
// 食い違ったときに「更新できた」ように見える。失敗させ、何も積まない。
func TestGitHub_UpdateRejectsUnknownItem(t *testing.T) {
	t.Parallel()
	c, fake, _ := newGitHub(t)

	if err := c.UpdateDraftIssue(context.Background(), "PVTI_missing", port.DraftIssue{Title: "x"}); err == nil {
		t.Fatal("UpdateDraftIssue(存在しない item) = nil, want error")
	}
	if n := len(fake.Items()); n != 0 {
		t.Errorf("items = %d, want 0", n)
	}
}

// projectId を運ぶ操作は、どれも同じ Project だけを通す。片方だけ甘いと、
// 向き先を取り違えたアダプタにフィールド一覧が返り、フィールドの更新も
// 成功扱いになって、偽物が本物より甘くなる。
func TestGitHub_RejectsUnknownProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c, fake, _ := newGitHub(t)

	if _, err := c.ListProjectFields(ctx, "PVT_missing"); err == nil {
		t.Error("ListProjectFields(存在しない project) = nil, want error")
	}

	// フィールドの更新は item が実在していても、Project が違えば通さない。
	itemID, err := c.CreateDraftIssue(ctx, fakeupstream.ProjectID, port.DraftIssue{Title: "t", Body: "b"})
	if err != nil {
		t.Fatalf("CreateDraftIssue() = %v", err)
	}
	fields, err := c.ListProjectFields(ctx, fakeupstream.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectFields() = %v", err)
	}
	kind := fieldByName(t, fields, "Kind")
	optionID := kind.Options[0].ID
	if err := c.SetItemFieldValue(ctx, "PVT_missing", itemID,
		port.FieldValue{FieldID: kind.ID, OptionID: &optionID}); err == nil {
		t.Error("SetItemFieldValue(存在しない project) = nil, want error")
	}
	if items := fake.Items(); len(items) != 1 || items[0].Kind != "" {
		t.Errorf("items = %+v, want Kind 未設定の 1 件", items)
	}
}

// 知らないクエリを空の data で返すと、アダプタ側で「0 件」に化けて原因が
// 見えなくなる。errors で返すことを固定する。
func TestGitHub_UnknownQueryIsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(fakeupstream.New())
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/graphql",
		stringsReader(`{"query":"query { viewer { login } }"}`))
	if err != nil {
		t.Fatalf("NewRequest = %v", err)
	}
	req.Header.Set("content-type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// GraphQL の誤りは本物と同じく 200 の中の errors で返す。
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode = %v", err)
	}
	if len(body.Errors) == 0 || body.Data != nil {
		t.Fatalf("body = %+v, want errors only", body)
	}
}
