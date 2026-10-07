package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/yusuke0610/etoki/port"
)

// listPageSize は一度に取得するリポジトリ / Project の数。
const listPageSize = 100

// maxRepositories は一覧で辿るリポジトリ数の上限。
//
// 選択肢として画面に出すものなので、全部を取り切ることに意味が無い。
// 上限を設けないと、所属組織の多い利用者で一覧の取得だけが延々と続く。
const maxRepositories = 500

// maxRestPages は 1 インストールあたりに辿る REST のページ数の上限。
//
// maxRepositories は「残った件数」しか数えない。アーカイブ済みばかりの
// ページが続くと候補が増えず、上限に達しないまま全ページを取り切ってしまう。
// 走査そのものにも歯止めを置く。
const maxRestPages = 20

// ListRepositories は作成先に選べるリポジトリを返す。
//
// アーカイブ済みは含めない。選択肢として出しても選ばせる意味が無い。
//
// 「使える」の定義が Mode で変わる（ADR 0015）。GitHub App では
// インストールしたリポジトリだけ、PAT では利用者が見えるものすべて。
// これは実装の都合ではなく、GitHub 側の権限モデルの違いそのもの。
func (c *Client) ListRepositories(ctx context.Context) (port.RepositoryList, error) {
	if c.mode == ModeApp {
		return c.listInstalledRepositories(ctx)
	}
	return c.listViewerRepositories(ctx)
}

// listViewerRepositories は PAT 向けに、利用者が見えるリポジトリを返す。
//
// アーカイブ済みはクエリ側で落としている。
//
// トークンに repo の read が無いと 0 件になるが、権限不足と「本当に 1 つも
// 無い」を GraphQL の応答からは区別できない。案内は呼び出し側に任せる。
func (c *Client) listViewerRepositories(ctx context.Context) (port.RepositoryList, error) {
	var (
		repos []port.Repository
		after *string
	)

	for {
		var resp repositoriesResponse
		vars := map[string]any{"first": listPageSize, "after": after}
		if err := c.do(ctx, queryViewerRepositories, vars, &resp); err != nil {
			return port.RepositoryList{}, err
		}

		for _, n := range resp.Viewer.Repositories.Nodes {
			repos = append(repos, port.Repository{
				Owner:       n.Owner.Login,
				Name:        n.Name,
				Description: n.Description,
			})
		}

		// **上限の判定より先に、次のページがあるかを見る。** 上限ちょうどで
		// 取り切ったのは「辿るのをやめた」ではないので、打ち切りにしない
		// （ADR 0054）。順を逆にすると、ちょうど 500 件の利用者に毎回
		// 「打ち切っています」と出る。
		page := resp.Viewer.Repositories.PageInfo
		next, err := nextCursor("repositories", page.HasNextPage, page.EndCursor, after)
		if err != nil {
			return port.RepositoryList{}, err
		}
		if next == nil {
			// 取り切った。返さないぶんがあるときだけ打ち切り。
			if len(repos) > maxRepositories {
				return port.RepositoryList{
					Repositories: repos[:maxRepositories], Truncated: true,
				}, nil
			}
			return port.RepositoryList{Repositories: repos}, nil
		}

		// まだ続くのに上限に達した。ここで辿るのをやめる。選択肢として見せる
		// ものなので全件を取り切る必要が無い。**やめたことは返り値に載せる。**
		// 黙って切ると、目当てが出ない利用者が権限を疑うことになる。
		if len(repos) >= maxRepositories {
			return port.RepositoryList{
				Repositories: repos[:maxRepositories], Truncated: true,
			}, nil
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
			projects = append(projects, port.Project{
				ID: n.ID, Number: n.Number, Title: n.Title, URL: n.URL,
			})
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

// listInstalledRepositories は GitHub App 向けに、アプリをインストールした
// リポジトリを返す。
//
// GraphQL の viewer.repositories は使わない。user-to-server トークンで
// それが返す範囲は仕様として保証されていない。インストール経由の REST が
// GitHub App における「使えるリポジトリ」の定義そのもの（ADR 0015）。
//
// 画面の意味も正しくなる。候補が「利用者が etoki に許可したリポジトリ」だけに
// なり、選んだのに Projects を作れない、が起きない。
func (c *Client) listInstalledRepositories(ctx context.Context) (port.RepositoryList, error) {
	// インストール一覧もページングする。1 ページ目で打ち切ると、超えた分の
	// インストールが黙って消え、そのリポジトリが候補に出ない。「インストール
	// 経由が使えるリポジトリの定義」という前提がそこで崩れる。
	var installIDs []int64

	for page := 1; ; page++ {
		var installations struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}

		path := fmt.Sprintf("/user/installations?per_page=%d&page=%d", listPageSize, page)
		if err := c.rest(ctx, path, &installations); err != nil {
			return port.RepositoryList{}, err
		}

		for _, inst := range installations.Installations {
			installIDs = append(installIDs, inst.ID)
		}

		// 埋まっていないページが返ったら終わり。リポジトリ側と同じ止め方に
		// 揃える。total_count は権限で絞られた件数と食い違うので信じない。
		if len(installations.Installations) < listPageSize {
			break
		}
	}

	var (
		repos []port.Repository
		// truncated は候補を取り切らずに辿るのをやめたこと。ページ数の上限で
		// 止めた場合も含める。**「まだある」ではなく「見るのをやめた」。**
		truncated bool
	)

	for i, instID := range installIDs {
		for page := 1; ; page++ {
			var body struct {
				Repositories []struct {
					Name        string `json:"name"`
					Description string `json:"description"`
					Archived    bool   `json:"archived"`
					Owner       struct {
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"repositories"`
			}

			path := fmt.Sprintf("/user/installations/%d/repositories?per_page=%d&page=%d",
				instID, listPageSize, page)
			if err := c.rest(ctx, path, &body); err != nil {
				return port.RepositoryList{}, err
			}

			for _, r := range body.Repositories {
				// GraphQL 側はクエリで落としているが、この REST には
				// アーカイブ済みを除くパラメータが無い。取ってから捨てるしかない。
				if r.Archived {
					continue
				}
				repos = append(repos, port.Repository{
					Owner:       r.Owner.Login,
					Name:        r.Name,
					Description: r.Description,
				})
			}

			// 埋まっていないページは、このインストールを取り切った印。
			// total_count は権限で絞られた件数と食い違うので信じない。
			done := len(body.Repositories) < listPageSize
			last := i == len(installIDs)-1

			// 選択肢として見せるものなので、全件を取り切る必要は無い。
			// **打ち切ったことは返り値に載せる**（ADR 0054）。ただし
			// **最後のインストールを取り切った上で上限ちょうどだったなら、
			// 辿るのをやめてはいない**ので打ち切りにしない。
			if len(repos) >= maxRepositories {
				// 最後のインストールを取り切ったなら、辿るのをやめていない。
				fetchedAll := done && last
				return port.RepositoryList{
					Repositories: repos[:maxRepositories],
					Truncated:    !fetchedAll || len(repos) > maxRepositories,
				}, nil
			}
			if done {
				break
			}
			// 上の判定は残った件数しか見ない。アーカイブ済みばかりのページが
			// 続くと repos が増えず、上限に達しないまま辿り続ける。走査した
			// ページ数にも上限を置く。**こちらも打ち切り。** インストールの
			// 途中で止めているので、残りのページに候補があっても出ない。
			// **短いページを見たあとに判定する。** 先に判定すると、ちょうど
			// 上限のページで取り切ったときまで打ち切り扱いになる。
			if page >= maxRestPages {
				truncated = true
				break
			}
		}
	}

	return port.RepositoryList{Repositories: repos, Truncated: truncated}, nil
}

// rest は GitHub の REST を 1 回叩き、JSON を out に詰める。
//
// Projects v2 は GraphQL にしか無いので、REST を使うのはインストール一覧だけ。
// そのためだけに別のクライアントを作らず、ここに小さく持つ。
func (c *Client) rest(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("accept", "application/vnd.github+json")

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call github rest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return statusError(resp, raw)
	}

	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

// queryViewerRepositories はトークンの持ち主が関われるリポジトリを取る。
//
// affiliations を絞らないと、star したリポジトリまで混ざる。並びは push が
// 新しい順。直近さわっているものほど選びたい対象である可能性が高い。
//
// **アーカイブ済みはクエリ側で落とす。** 取ってから捨てると、1 ページの枠を
// 選択肢にならないもので埋めてしまい、maxRepositories に達する前に候補が
// 尽きる。
const queryViewerRepositories = `query($first: Int!, $after: String) {
  viewer {
    repositories(
      first: $first
      after: $after
      isArchived: false
      affiliations: [OWNER, COLLABORATOR, ORGANIZATION_MEMBER]
      orderBy: {field: PUSHED_AT, direction: DESC}
    ) {
      pageInfo { hasNextPage endCursor }
      nodes { name description owner { login } }
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
//
// **url は自分で組まずに取る。** owner が user か org かで形が変わり、etoki は
// どちらなのかを知らない（ADR 0025）。
//
// **minPermissionLevel は WRITE。** 既定の READ だと、読めるだけで書けない
// Project まで候補に出る。選んだ時点では通り、最初の draft issue を作る
// ところで初めて GitHub 側の権限エラーになる。そこは固定の時点でもあるので、
// 作成先を変えて逃げることもできない（ADR 0014）。選ばせる前に落とす。
const queryRepositoryProjects = `query($owner: String!, $name: String!, $first: Int!, $after: String) {
  repository(owner: $owner, name: $name) {
    projectsV2(
      first: $first
      after: $after
      minPermissionLevel: WRITE
      orderBy: {field: NUMBER, direction: ASC}
    ) {
      pageInfo { hasNextPage endCursor }
      nodes { id number title url closed }
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
				URL    string `json:"url"`
				Closed bool   `json:"closed"`
			} `json:"nodes"`
		} `json:"projectsV2"`
	} `json:"repository"`
}
