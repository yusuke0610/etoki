package github_test

import (
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/port"
)

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

func TestCreateDraftIssue(t *testing.T) {
	t.Parallel()

	// fullDatabaseId は BigInt で、GitHub は文字列で返す。32 ビットを超える値で
	// 固定しておくと、int に落とす実装も落ちる。
	body := `{"data":{"addProjectV2DraftIssue":{"projectItem":{"id":"PVTI_item1","fullDatabaseId":"123456789012"}}}}`
	c, got := newClient(t, body)

	ref, err := c.CreateDraftIssue(t.Context(), "PVT_1",
		port.DraftIssue{Title: "決済フローの見直し", Body: "全体の方針"})
	if err != nil {
		t.Fatalf("CreateDraftIssue() = %v", err)
	}

	// 返すのは ProjectV2Item の ID。DraftIssue content の ID ではない。
	want := port.ProjectItemRef{ItemID: "PVTI_item1", DatabaseID: 123456789012}
	if ref != want {
		t.Errorf("ref = %+v, want %+v", ref, want)
	}

	req := (*got)[0]
	if !strings.Contains(req.Query, "addProjectV2DraftIssue") {
		t.Errorf("クエリが違う:\n%s", req.Query)
	}
	if !strings.Contains(req.Query, "projectItem { id fullDatabaseId }") {
		t.Errorf("projectItem の id と fullDatabaseId を取っていない:\n%s", req.Query)
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

// 識別子はリンクを組むためだけのもの。読めなくても作成は済んでいるので、
// 失敗にせず 0（知らない）で返す（ADR 0057）。失敗にすると、作ったのに
// 記録されない item が出る。
func TestCreateDraftIssue_DatabaseIDUnreadable(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"返らない":       `{"id":"PVTI_1"}`,
		"null":       `{"id":"PVTI_1","fullDatabaseId":null}`,
		"数値で届く":      `{"id":"PVTI_1","fullDatabaseId":42}`,
		"数字ではない":     `{"id":"PVTI_1","fullDatabaseId":"javascript:alert(1)"}`,
		"int64 を超える": `{"id":"PVTI_1","fullDatabaseId":"99999999999999999999"}`,
		"0":          `{"id":"PVTI_1","fullDatabaseId":"0"}`,
		"負":          `{"id":"PVTI_1","fullDatabaseId":"-5"}`,
	}
	for name, item := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c, _ := newClient(t, `{"data":{"addProjectV2DraftIssue":{"projectItem":`+item+`}}}`)

			ref, err := c.CreateDraftIssue(t.Context(), "PVT_1", port.DraftIssue{Title: "t"})
			if err != nil {
				t.Fatalf("CreateDraftIssue() = %v, want nil", err)
			}
			want := port.ProjectItemRef{ItemID: "PVTI_1", DatabaseID: 0}
			if ref != want {
				t.Errorf("ref = %+v, want %+v", ref, want)
			}
		})
	}
}

// 更新は 2 往復する。受け取るのは ProjectV2Item の ID だが、GitHub の更新が
// 要求するのは DraftIssue content の ID で、この 2 つは別物（ADR 0026）。
func TestUpdateDraftIssue(t *testing.T) {
	t.Parallel()

	c, got := newClient(t,
		`{"data":{"node":{"fullDatabaseId":"77","content":{"__typename":"DraftIssue","id":"DI_draft1"}}}}`,
		`{"data":{"updateProjectV2DraftIssue":{"draftIssue":{"id":"DI_draft1"}}}}`,
	)

	ref, err := c.UpdateDraftIssue(t.Context(), "PVTI_item1",
		port.DraftIssue{Title: "決済フローの見直し", Body: "方針を書き直した"})
	if err != nil {
		t.Fatalf("UpdateDraftIssue() = %v", err)
	}

	// 識別子は更新の payload ではなく、1 往復目の引き当てから取る（ADR 0057）。
	want := port.ProjectItemRef{ItemID: "PVTI_item1", DatabaseID: 77}
	if ref != want {
		t.Errorf("ref = %+v, want %+v", ref, want)
	}

	if len(*got) != 2 {
		t.Fatalf("リクエスト数 = %d, want 2", len(*got))
	}

	// 1 往復目は item から content を辿るだけ。ここで書き換えない。
	lookup := (*got)[0]
	if lookup.Variables["itemId"] != "PVTI_item1" {
		t.Errorf("itemId = %v", lookup.Variables["itemId"])
	}
	if strings.Contains(lookup.Query, "mutation") {
		t.Errorf("引き当てで書き換えている:\n%s", lookup.Query)
	}
	if !strings.Contains(lookup.Query, "fullDatabaseId") {
		t.Errorf("引き当てで fullDatabaseId を取っていない:\n%s", lookup.Query)
	}

	// 2 往復目が更新。送る ID は content のもので、item のものではない。
	update := (*got)[1]
	if !strings.Contains(update.Query, "updateProjectV2DraftIssue") {
		t.Errorf("クエリが違う:\n%s", update.Query)
	}
	if update.Variables["draftIssueId"] != "DI_draft1" {
		t.Errorf("draftIssueId = %v, want DI_draft1", update.Variables["draftIssueId"])
	}
	if update.Variables["title"] != "決済フローの見直し" {
		t.Errorf("title = %v", update.Variables["title"])
	}
	if update.Variables["body"] != "方針を書き直した" {
		t.Errorf("body = %v", update.Variables["body"])
	}
}

func TestUpdateDraftIssue_Errors(t *testing.T) {
	t.Parallel()

	// Project には本物の issue も並ぶ。etoki が作った item が誰かの手で
	// 置き換わっていることもあるので、中身を確かめずに更新を投げると
	// 他人の issue を書き換えうる。
	t.Run("draft issue ではない", func(t *testing.T) {
		t.Parallel()

		c, got := newClient(t, `{"data":{"node":{"content":{"__typename":"Issue"}}}}`)

		if _, err := c.UpdateDraftIssue(t.Context(), "PVTI_item1",
			port.DraftIssue{Title: "t"}); err == nil {
			t.Fatal("UpdateDraftIssue() = nil, want error")
		}
		// 引き当てだけで止まる。更新を投げていない。
		if len(*got) != 1 {
			t.Errorf("リクエスト数 = %d, want 1", len(*got))
		}
	})

	t.Run("item が無い", func(t *testing.T) {
		t.Parallel()

		c, got := newClient(t, `{"data":{"node":null}}`)

		if _, err := c.UpdateDraftIssue(t.Context(), "PVTI_gone",
			port.DraftIssue{Title: "t"}); err == nil {
			t.Fatal("UpdateDraftIssue() = nil, want error")
		}
		if len(*got) != 1 {
			t.Errorf("リクエスト数 = %d, want 1", len(*got))
		}
	})

	t.Run("入口で弾く", func(t *testing.T) {
		t.Parallel()

		c, got := newClient(t, `{"data":{}}`)

		if _, err := c.UpdateDraftIssue(t.Context(), "", port.DraftIssue{Title: "t"}); err == nil {
			t.Error("item id が空なのにエラーにならない")
		}
		if _, err := c.UpdateDraftIssue(t.Context(), "PVTI_1", port.DraftIssue{}); err == nil {
			t.Error("タイトルが空なのにエラーにならない")
		}
		if len(*got) != 0 {
			t.Errorf("入口で弾かずに送信している: %d 回", len(*got))
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
