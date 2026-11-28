package github_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/port"
)

func TestListRepositories(t *testing.T) {
	t.Parallel()

	body := `{"data":{"viewer":{"repositories":{
		"pageInfo":{"hasNextPage":false,"endCursor":"c1"},
		"nodes":[
			{"name":"web","description":"フロント","owner":{"login":"acme"}},
			{"name":"api","description":"","owner":{"login":"other"}}
		]}}}}`

	c, got := newClient(t, body)

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}

	want := []port.Repository{
		{Owner: "acme", Name: "web", Description: "フロント"},
		{Owner: "other", Name: "api"},
	}
	repos := list.Repositories
	if len(repos) != len(want) {
		t.Fatalf("len(repos) = %d, want %d (%+v)", len(repos), len(want), repos)
	}
	for i := range want {
		if repos[i] != want[i] {
			t.Errorf("repos[%d] = %+v, want %+v", i, repos[i], want[i])
		}
	}
	// 取り切ったので打ち切っていない。**ここを見ないと、常に true を返す
	// 実装でも「打ち切りが出る」側のテストだけで緑になる**（ADR 0054）。
	if list.Truncated {
		t.Error("Truncated = true, want false（上限に当たっていない）")
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

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(list.Repositories) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(list.Repositories))
	}
	if (*got)[1].Variables["after"] != "cursor-1" {
		t.Errorf("2 回目の after = %v, want cursor-1", (*got)[1].Variables["after"])
	}
}

// 選択肢として見せるものなので、全部を取り切らずに打ち切る（ADR 0054）。
func TestListRepositories_StopsAtMaxRepositories(t *testing.T) {
	t.Parallel()

	// 上限（500）を超える件数を 1 ページで返す。ページの件数で打ち切る実装では
	// ないので、これで上限の判定に当たる。
	nodes := make([]string, 0, 600)
	for i := range 600 {
		nodes = append(nodes, fmt.Sprintf(`{"name":"repo-%d","owner":{"login":"acme"}}`, i))
	}
	body := `{"data":{"viewer":{"repositories":{` +
		`"pageInfo":{"hasNextPage":false,"endCursor":"c1"},` +
		`"nodes":[` + strings.Join(nodes, ",") + `]}}}}`

	c, _ := newClient(t, body)

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(list.Repositories) != 500 {
		t.Errorf("len(repos) = %d, want 500", len(list.Repositories))
	}
	if !list.Truncated {
		t.Error("Truncated = false, want true（上限に当たっている）")
	}
}

// **上限ちょうどで取り切ったのは打ち切りではない。** `truncated` は「まだある」
// ではなく「辿るのをやめた」の意味（ADR 0054）。上限の判定を次ページの有無より
// 先に置くと、ちょうど 500 件しか持たない利用者に毎回「打ち切っています」と
// 出る。**判定の順を戻すと落ちる。**
func TestListRepositories_ExactlyMaxAndFetchedAllIsNotTruncated(t *testing.T) {
	t.Parallel()

	nodes := make([]string, 0, 500)
	for i := range 500 {
		nodes = append(nodes, fmt.Sprintf(`{"name":"repo-%d","owner":{"login":"acme"}}`, i))
	}
	body := `{"data":{"viewer":{"repositories":{` +
		`"pageInfo":{"hasNextPage":false,"endCursor":"c1"},` +
		`"nodes":[` + strings.Join(nodes, ",") + `]}}}}`

	c, _ := newClient(t, body)

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(list.Repositories) != 500 {
		t.Errorf("len(repos) = %d, want 500", len(list.Repositories))
	}
	if list.Truncated {
		t.Error("Truncated = true, want false（取り切っている）")
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
			{"id":"PVT_1","number":1,"title":"ロードマップ","url":"https://github.com/orgs/acme/projects/1","closed":false},
			{"id":"PVT_2","number":2,"title":"終わったやつ","url":"https://github.com/orgs/acme/projects/2","closed":true}
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
	// URL は番号から組み立てず GitHub が返したものを運ぶ。owner が user か
	// org かで形が変わり、etoki はどちらなのかを知らない（ADR 0025）。
	want := port.Project{
		ID: "PVT_1", Number: 1, Title: "ロードマップ",
		URL: "https://github.com/orgs/acme/projects/1",
	}
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
