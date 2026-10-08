package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/yusuke0610/etoki/port"
)

// fieldsPageSize は一度に取得するカスタムフィールドの数。
const fieldsPageSize = 100

// CanWriteProject は現在の利用者がその Project に書けるかを返す。
//
// **これで作成を弾かない。表示にだけ使う**（ADR 0017）。実際の可否は作成時に
// GitHub が返したものが正しく、その間に権限が変わることもある。ここが返すのは
// 「いまの状態」であって判定ではない。
func (c *Client) CanWriteProject(ctx context.Context, projectID string) (bool, error) {
	if projectID == "" {
		return false, errors.New("github: project id is required")
	}

	var resp projectPermissionResponse
	if err := c.do(ctx, queryProjectPermission,
		map[string]any{"projectId": projectID}, &resp); err != nil {
		return false, err
	}

	return resp.Node.ViewerCanUpdate, nil
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

// CreateDraftIssue は draft issue を作成し、その ProjectV2Item を指す手掛かりを返す。
//
// 返す ItemID は ProjectV2Item の ID であって DraftIssue content の ID ではない。
// 後続の SetItemFieldValue が前者を要求する。
func (c *Client) CreateDraftIssue(
	ctx context.Context, projectID string, item port.DraftIssue,
) (port.ProjectItemRef, error) {
	if item.Title == "" {
		return port.ProjectItemRef{}, errors.New("github: draft issue title is required")
	}

	var resp createDraftIssueResponse
	vars := map[string]any{"projectId": projectID, "title": item.Title, "body": item.Body}
	if err := c.do(ctx, mutationCreateDraftIssue, vars, &resp); err != nil {
		return port.ProjectItemRef{}, err
	}

	projectItem := resp.AddProjectV2DraftIssue.ProjectItem
	if projectItem.ID == "" {
		return port.ProjectItemRef{}, errors.New("github: draft issue created but no item id returned")
	}

	return port.ProjectItemRef{
		ItemID:     projectItem.ID,
		DatabaseID: parseDatabaseID(projectItem.FullDatabaseID),
	}, nil
}

// parseDatabaseID は GraphQL の BigInt を読む。読めなければ 0（知らない）。
//
// **BigInt は文字列で届く**（スキーマの定義にそう書いてある）。ここで整数に
// してしまえば、URL に差し込む側で値を検査し直さずに済む（ADR 0057）。
//
// **読めなくてもエラーにしない。** 呼ぶのは取り消せない書き込みの直後で、
// 確認のためのリンクが組めないことを理由に作成を失敗扱いにしない。負の値や 0 も
// 「知らない」に倒す。識別子として意味を持たないので、リンクにしても開けない。
func parseDatabaseID(raw json.RawMessage) int64 {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// UpdateDraftIssue は既存の draft issue の title と body を書き換える。
//
// **2 往復する。** 受け取るのは ProjectV2Item の ID だが、GitHub の更新
// mutation が要求するのは DraftIssue content の ID で、この 2 つは別物。
// sync_items が控えているのは前者だけなので（ADR 0007）、後者はここで引き直す。
//
// 控える側を content の ID に変える手もあるが、そうすると列を足す前に作った
// run だけが更新できないまま残る。引き直せば古い run も救える。
//
// item の数値の識別子もこの引き直しで取る。更新の mutation が返すのは
// DraftIssue で item ではないため（ADR 0057）。往復は増やさない。
func (c *Client) UpdateDraftIssue(
	ctx context.Context, itemID string, item port.DraftIssue,
) (port.ProjectItemRef, error) {
	if itemID == "" {
		return port.ProjectItemRef{}, errors.New("github: item id is required")
	}
	if item.Title == "" {
		return port.ProjectItemRef{}, errors.New("github: draft issue title is required")
	}

	draftID, databaseID, err := c.draftIssueContentID(ctx, itemID)
	if err != nil {
		return port.ProjectItemRef{}, err
	}

	var resp updateDraftIssueResponse
	vars := map[string]any{"draftIssueId": draftID, "title": item.Title, "body": item.Body}

	if err := c.do(ctx, mutationUpdateDraftIssue, vars, &resp); err != nil {
		return port.ProjectItemRef{}, err
	}

	return port.ProjectItemRef{ItemID: itemID, DatabaseID: databaseID}, nil
}

// draftIssueContentID は ProjectV2Item の ID から DraftIssue content の ID と、
// item の数値の識別子を引く。識別子は読めなければ 0。
//
// **draft issue でなければ書き換えない。** Project には本物の issue や PR も
// 並ぶ。etoki が作った item が誰かの手で置き換わっている可能性もあるので、
// 中身を確かめずに更新を投げると、他人の issue を書き換えうる。
func (c *Client) draftIssueContentID(ctx context.Context, itemID string) (string, int64, error) {
	var resp itemContentResponse
	if err := c.do(ctx, queryItemContent, map[string]any{"itemId": itemID}, &resp); err != nil {
		return "", 0, err
	}

	content := resp.Node.Content
	if content.ID == "" {
		// __typename は DraftIssue 以外のときに何であるかを示す。空なら item
		// そのものが無い（消された、別の Project を指している）。
		kind := content.Typename
		if kind == "" {
			kind = "not found"
		}
		return "", 0, fmt.Errorf("github: item %s is not a draft issue (%s)", itemID, kind)
	}

	return content.ID, parseDatabaseID(resp.Node.FullDatabaseID), nil
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

// queryProjectPermission は現在の利用者がその Project に書けるかを取る。
//
// projectsV2(minPermissionLevel: WRITE) で候補を絞る形（ADR 0014）は
// 「リポジトリ経由で辿れるか」しか見ない。招待されただけの利用者はその
// リポジトリを辿れないので、Project を直接引いて訊く。
const queryProjectPermission = `query($projectId: ID!) {
  node(id: $projectId) {
    ... on ProjectV2 { viewerCanUpdate }
  }
}`

type projectPermissionResponse struct {
	Node struct {
		ViewerCanUpdate bool `json:"viewerCanUpdate"`
	} `json:"node"`
}

// mutationCreateDraftIssue は draft issue を作る。
//
// **識別子は `databaseId` ではなく `fullDatabaseId` を取る。** 前者は 64 ビットに
// 収まらないとして削除が告知されている（ADR 0057）。
const mutationCreateDraftIssue = `mutation($projectId: ID!, $title: String!, $body: String) {
  addProjectV2DraftIssue(input: {projectId: $projectId, title: $title, body: $body}) {
    projectItem { id fullDatabaseId }
  }
}`

type createDraftIssueResponse struct {
	AddProjectV2DraftIssue struct {
		ProjectItem struct {
			ID string `json:"id"`
			// FullDatabaseID は BigInt で、文字列で届く。形が違っても作成を
			// 失敗させないよう、生のまま受けて parseDatabaseID で読む。
			FullDatabaseID json.RawMessage `json:"fullDatabaseId"`
		} `json:"projectItem"`
	} `json:"addProjectV2DraftIssue"`
}

// queryItemContent は ProjectV2Item の中身を取る。
//
// **`... on DraftIssue` を通らなければ id は返らない。** 本物の issue や PR が
// 紐づいた item では `__typename` だけが返り、それが「draft issue ではない」の
// 判定材料になる。
const queryItemContent = `query($itemId: ID!) {
  node(id: $itemId) {
    ... on ProjectV2Item {
      fullDatabaseId
      content {
        __typename
        ... on DraftIssue { id }
      }
    }
  }
}`

type itemContentResponse struct {
	Node struct {
		// FullDatabaseID は createDraftIssueResponse と同じく生のまま受ける。
		FullDatabaseID json.RawMessage `json:"fullDatabaseId"`
		Content        struct {
			Typename string `json:"__typename"`
			ID       string `json:"id"`
		} `json:"content"`
	} `json:"node"`
}

// mutationUpdateDraftIssue は draft issue の title と body を書き換える。
//
// 要求するのは DraftIssue content の ID であって ProjectV2Item の ID ではない。
const mutationUpdateDraftIssue = `mutation($draftIssueId: ID!, $title: String!, $body: String) {
  updateProjectV2DraftIssue(input: {draftIssueId: $draftIssueId, title: $title, body: $body}) {
    draftIssue { id }
  }
}`

type updateDraftIssueResponse struct {
	UpdateProjectV2DraftIssue struct {
		DraftIssue struct {
			ID string `json:"id"`
		} `json:"draftIssue"`
	} `json:"updateProjectV2DraftIssue"`
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
