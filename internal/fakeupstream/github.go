package fakeupstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// 偽の GitHub が持つ固定の作成先。
const (
	RepositoryOwner = "fake-owner"
	RepositoryName  = "fake-repo"
	ProjectID       = "PVT_fake1"
	ProjectURL      = "https://github.com/users/fake-owner/projects/1"

	kindFieldID     = "PVTF_kind"
	parentFieldID   = "PVTF_parent"
	epicOptionID    = "PVTSSO_epic"
	issueOptionID   = "PVTSSO_issue"
	kindFieldName   = "Kind"
	parentFieldName = "Parent"
)

// Item は偽の GitHub に積まれた draft issue 1 件。
type Item struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	Kind    string `json:"kind"`
	Parent  string `json:"parent"`
	Updates int    `json:"updates"`
}

// github は偽の GitHub の状態。メモリにだけ持つ。
type github struct {
	mu    sync.Mutex
	items []*Item
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// handleGraphQL は etoki の GitHub アダプタが送る操作だけに答える。
//
// 判定はクエリに含まれるルートフィールド名で行う。本物の GitHub に似せるのは
// アダプタが読むフィールドまでで、スキーマを追いかけない。アダプタのクエリと
// ずれたら往復テスト（github_test.go）が落ちる。**知らないクエリは errors で
// 返し、黙って空の data にしない。** 空にするとアダプタ側で「0 件」に化ける。
func (g *github) handleGraphQL(w http.ResponseWriter, r *http.Request) {
	var req graphQLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, gqlError(err.Error()))
		return
	}
	q, v := req.Query, req.Variables

	var data any
	var err error
	switch {
	case strings.Contains(q, "addProjectV2DraftIssue"):
		data, err = g.create(v)
	case strings.Contains(q, "updateProjectV2DraftIssue"):
		data, err = g.update(v)
	case strings.Contains(q, "updateProjectV2ItemFieldValue"):
		data, err = g.setField(v)
	case strings.Contains(q, "viewerCanUpdate"):
		data = map[string]any{"node": map[string]any{"viewerCanUpdate": v["projectId"] == ProjectID}}
	case strings.Contains(q, "fields("):
		data, err = fieldsData(v)
	case strings.Contains(q, "content {"):
		data = g.content(v)
	case strings.Contains(q, "projectsV2("):
		data = projectsData(v)
	case strings.Contains(q, "viewer {") && strings.Contains(q, "repositories("):
		data = repositoriesData()
	default:
		writeJSON(w, http.StatusOK, gqlError("fakeupstream: unsupported query"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, gqlError(err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func gqlError(msg string) map[string]any {
	return map[string]any{"errors": []map[string]string{{"message": msg}}}
}

func noMorePages() map[string]any {
	return map[string]any{"hasNextPage": false, "endCursor": ""}
}

func repositoriesData() map[string]any {
	return map[string]any{"viewer": map[string]any{"repositories": map[string]any{
		"pageInfo": noMorePages(),
		"nodes": []map[string]any{{
			"name":        RepositoryName,
			"description": "偽の GitHub が返す作成先",
			"owner":       map[string]string{"login": RepositoryOwner},
		}},
	}}}
}

func projectsData(v map[string]any) map[string]any {
	nodes := []map[string]any{}
	if v["owner"] == RepositoryOwner && v["name"] == RepositoryName {
		nodes = append(nodes, map[string]any{
			"id": ProjectID, "number": 1, "title": "Fake Project", "url": ProjectURL, "closed": false,
		})
	}
	return map[string]any{"repository": map[string]any{"projectsV2": map[string]any{
		"pageInfo": noMorePages(),
		"nodes":    nodes,
	}}}
}

// fieldsData は Project のフィールド一覧を返す。
//
// projectId を見るのは、本物では node(id:) が知らない ID から Project を
// 解決できないため。見ないと、向き先を取り違えたアダプタにも一覧が返り、
// 作成まで進んでから初めて落ちる。
func fieldsData(v map[string]any) (any, error) {
	if err := checkProject(v); err != nil {
		return nil, err
	}
	return map[string]any{"node": map[string]any{"fields": map[string]any{
		"pageInfo": noMorePages(),
		"nodes": []map[string]any{
			{"id": "PVTF_title", "name": "Title", "dataType": "TITLE"},
			{
				"id": kindFieldID, "name": kindFieldName, "dataType": "SINGLE_SELECT",
				"options": []map[string]string{
					{"id": epicOptionID, "name": "epic"},
					{"id": issueOptionID, "name": "issue"},
				},
			},
			{"id": parentFieldID, "name": parentFieldName, "dataType": "TEXT"},
		},
	}}}, nil
}

// checkProject は projectId が偽の GitHub が持つ 1 つの Project かを見る。
//
// projectId を運ぶ操作すべてで同じ判定にする。片方だけ通すと、向き先が
// 違ったままフィールドの更新が成功扱いになり、偽物が本物より甘くなる。
func checkProject(v map[string]any) error {
	if v["projectId"] != ProjectID {
		return fmt.Errorf("fakeupstream: unknown project %v", v["projectId"])
	}
	return nil
}

func (g *github) create(v map[string]any) (any, error) {
	if err := checkProject(v); err != nil {
		return nil, err
	}
	title, _ := v["title"].(string)
	body, _ := v["body"].(string)

	g.mu.Lock()
	defer g.mu.Unlock()
	item := &Item{ID: fmt.Sprintf("PVTI_fake%d", len(g.items)+1), Title: title, Body: body}
	g.items = append(g.items, item)

	return map[string]any{"addProjectV2DraftIssue": map[string]any{
		"projectItem": map[string]string{"id": item.ID},
	}}, nil
}

// draftIssueID は item ID から DraftIssue content の ID を作る。本物と同じく
// 2 つを別の値にしておき、取り違えたら更新が失敗するようにする。
func draftIssueID(itemID string) string {
	return "DI_" + strings.TrimPrefix(itemID, "PVTI_")
}

func (g *github) find(id string) *Item {
	for _, it := range g.items {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (g *github) content(v map[string]any) map[string]any {
	id, _ := v["itemId"].(string)

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.find(id) == nil {
		return map[string]any{"node": nil}
	}
	return map[string]any{"node": map[string]any{"content": map[string]string{
		"__typename": "DraftIssue", "id": draftIssueID(id),
	}}}
}

func (g *github) update(v map[string]any) (any, error) {
	did, _ := v["draftIssueId"].(string)

	g.mu.Lock()
	defer g.mu.Unlock()
	var item *Item
	for _, it := range g.items {
		if draftIssueID(it.ID) == did {
			item = it
		}
	}
	if item == nil {
		return nil, fmt.Errorf("fakeupstream: unknown draft issue %q", did)
	}
	item.Title, _ = v["title"].(string)
	item.Body, _ = v["body"].(string)
	item.Updates++

	return map[string]any{"updateProjectV2DraftIssue": map[string]any{
		"draftIssue": map[string]string{"id": did},
	}}, nil
}

func (g *github) setField(v map[string]any) (any, error) {
	if err := checkProject(v); err != nil {
		return nil, err
	}
	id, _ := v["itemId"].(string)
	value, _ := v["value"].(map[string]any)

	g.mu.Lock()
	defer g.mu.Unlock()
	item := g.find(id)
	if item == nil {
		return nil, fmt.Errorf("fakeupstream: unknown item %q", id)
	}

	switch v["fieldId"] {
	case kindFieldID:
		switch value["singleSelectOptionId"] {
		case epicOptionID:
			item.Kind = "epic"
		case issueOptionID:
			item.Kind = "issue"
		default:
			return nil, fmt.Errorf("fakeupstream: unknown option %v", value["singleSelectOptionId"])
		}
	case parentFieldID:
		text, ok := value["text"].(string)
		if !ok {
			return nil, fmt.Errorf("fakeupstream: %s needs text", parentFieldName)
		}
		item.Parent = text
	default:
		return nil, fmt.Errorf("fakeupstream: unknown field %v", v["fieldId"])
	}

	return map[string]any{"updateProjectV2ItemFieldValue": map[string]any{
		"projectV2Item": map[string]string{"id": id},
	}}, nil
}

// snapshot は積まれた draft issue の写しを作った順に返す。
func (g *github) snapshot() []Item {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Item, len(g.items))
	for i, it := range g.items {
		out[i] = *it
	}
	return out
}
