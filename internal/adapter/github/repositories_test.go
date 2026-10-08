package github_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/github"
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
//
// **PAT と GitHub App で経路が別**（ADR 0015）なので、App 側のテストがあっても
// ここは要る。片方だけ打ち切りを載せ忘れても緑にならないようにする。
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

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	got := list.Repositories
	if len(got) != installs {
		t.Fatalf("len(repos) = %d, want %d", len(got), installs)
	}
	// 2 ページ目のインストールが含まれること。ここが落ちるなら一覧を
	// 切り詰めている。
	if got[installs-1].Name != "repo-101" {
		t.Errorf("最後の要素 = %+v, want repo-101", got[installs-1])
	}
	if list.Truncated {
		t.Error("Truncated = true, want false（上限に当たっていない）")
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

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	got := list.Repositories

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

	list, err := c.ListRepositories(t.Context())
	if err != nil {
		t.Fatalf("ListRepositories() = %v", err)
	}
	if len(list.Repositories) != 500 {
		t.Errorf("len(repos) = %d, want 500", len(list.Repositories))
	}
	// **打ち切ったことを返す**（ADR 0054）。黙って切ると、目当てが出ない
	// 利用者が権限やインストールを疑うことになる。
	if !list.Truncated {
		t.Error("Truncated = false, want true（上限に当たっている）")
	}
}

// App 経路でも同じ。**最後のインストールを取り切った上で上限ちょうど**なら
// 辿るのをやめていないので打ち切りではない（ADR 0054）。上限の判定を
// 「取り切ったか」より先に置くと、ここが true に倒れる。
func TestListRepositories_AppModeExactlyMaxAndFetchedAllIsNotTruncated(t *testing.T) {
	t.Parallel()

	// 2 つのインストールで合計ちょうど上限。どちらも埋まっていないページで
	// 終わるので、残りは無い。
	ids := []int64{1, 2}
	repos := make(map[int64][]installedRepo, len(ids))
	for n, id := range ids {
		batch := make([]installedRepo, 250)
		for i := range batch {
			batch[i] = installedRepo{name: fmt.Sprintf("repo-%d-%d", n, i), owner: "acme"}
		}
		repos[id] = batch
	}

	c, _ := newAppClient(t, ids, repos)

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
