package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yusuke0610/etoki/port"
)

// connectMCP は r を実際の HTTP サーバーに載せ、MCP のクライアントで繋ぐ。
//
// ハンドラを直に叩かず SDK のクライアントを通すのは、Streamable HTTP の
// 往復（initialize → tools/call）まで含めて確かめたいため。httptest の
// サーバーは 127.0.0.1 で待つので、Host の検証（origin.go）も実物の形で通る。
func connectMCP(t *testing.T, r *gin.Engine) *mcp.ClientSession {
	t.Helper()

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "etoki-test", Version: "0"}, nil)
	session, err := client.Connect(t.Context(),
		&mcp.StreamableClientTransport{Endpoint: srv.URL + "/mcp"}, nil)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// callTool は道具を呼び、成功していることを確かめてから構造化された結果を返す。
//
// **IsError を見ずに読まない。** 失敗も Content に JSON 以外の文で載るので、
// 確かめずに進むと「キーが無い」という見当違いの失敗になる（#104 と同じ）。
func callTool(t *testing.T, s *mcp.ClientSession, name string, args any) map[string]any {
	t.Helper()

	res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	if res.IsError {
		t.Fatalf("CallTool(%s) は失敗した: %s", name, toolText(res))
	}

	out, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("CallTool(%s): structuredContent = %T, want object", name, res.StructuredContent)
	}
	return out
}

func toolText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// apiJSON は /api の応答を JSON の値に戻す。MCP の結果と同じ土俵で比べるため。
func apiJSON(t *testing.T, r *gin.Engine, path string) any {
	t.Helper()

	rec := do(t, r, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d (%s)", path, rec.Code, rec.Body)
	}

	var out any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return out
}

// seedCreatedBoard は、注釈 1 つに作成済みの run があり、シーンから消えた注釈にも
// GitHub 側のものが残っているボードを作る。
//
// **3 状態の items と detached の両方を空にしない。** 片方が空の fixture では、
// その詰め替えを MCP 側だけ落としても比較が通る（.claude/rules/test-effectiveness.md）。
func seedCreatedBoard(t *testing.T, r *gin.Engine, mappings port.MappingRepository) string {
	t.Helper()

	id := createBoard(t, r, "決済の見直し")
	saveAnnotatedScene(t, r, id)

	parent := "e1"
	runs := []port.SyncRun{
		{
			BoardID: id, AnnotationID: "annot-1", ContentHash: currentHash(t, r, id),
			CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{{
				ItemID: "PVTI_e1", ItemDatabaseID: 101, Kind: port.KindEpic, Title: "決済API",
				LocalID: "e1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
			}},
		},
		{
			BoardID: id, AnnotationID: "annot-1", ContentHash: currentHash(t, r, id),
			CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{{
				ItemID: "PVTI_i1", ItemDatabaseID: 102, Kind: port.KindIssue, Title: "SDK を上げる",
				LocalID: "i1", ParentLocalID: &parent, Action: port.ActionCreated,
				Confirmed: true, CreatedAt: fixedTime,
			}},
		},
		{
			BoardID: id, AnnotationID: "annot-gone", ContentHash: "h-gone",
			CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
			Items: []port.SyncItem{{
				ItemID: "PVTI_g1", ItemDatabaseID: 201, Kind: port.KindIssue, Title: "消した囲み",
				LocalID: "g1", Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
			}},
		},
	}
	for _, run := range runs {
		if _, err := mappings.SaveRun(t.Context(), run); err != nil {
			t.Fatalf("SaveRun: %v", err)
		}
	}

	return id
}

// 何かを動かす道具（解釈・作成）を出さないことを固定する（ADR 0071）。足すと
// 人の確認を挟む場所を決めないまま、エージェントが取り消せない操作に届く。
func TestMCP_ListsOnlyReadOnlyTools(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	session := connectMCP(t, r)

	res, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: readOnlyHint が立っていない", tool.Name)
		}
		if tool.Annotations != nil &&
			(tool.Annotations.OpenWorldHint == nil || *tool.Annotations.OpenWorldHint) {
			t.Errorf("%s: openWorldHint が false でない。読むのは etoki の DB だけ", tool.Name)
		}
	}
	slices.Sort(names)

	want := []string{"list_annotation_runs", "list_annotations", "list_boards"}
	if !slices.Equal(names, want) {
		t.Errorf("道具 = %v, want %v", names, want)
	}
}

// 返す JSON は /api と同じ形にする（ADR 0071）。**詰め替えを別に書くと、契約に
// 足したフィールドが片方にだけ載る。** 生成型が同じでもゼロ値で通るので、
// 実際の応答どうしを比べる。
func TestMCP_ListBoards_MatchesAPI(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	seedCreatedBoard(t, r, mappings)
	createBoard(t, r, "まだ何も無い")

	got := callTool(t, connectMCP(t, r), "list_boards", map[string]any{})

	want := apiJSON(t, r, "/api/boards")
	if list, _ := want.([]any); len(list) != 2 {
		t.Fatalf("前提: /api/boards が 2 件でない: %v", want)
	}
	if !reflect.DeepEqual(got["boards"], want) {
		t.Errorf("list_boards.boards = %v\nwant (GET /api/boards) %v", got["boards"], want)
	}
}

func TestMCP_ListAnnotations_MatchesAPI(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := seedCreatedBoard(t, r, mappings)

	got := callTool(t, connectMCP(t, r), "list_annotations", map[string]any{"boardId": id})

	want, _ := apiJSON(t, r, "/api/boards/"+id+"/annotations").(map[string]any)
	annotations, _ := want["annotations"].([]any)
	detached, _ := want["detached"].([]any)
	if len(annotations) != 1 || len(detached) != 1 {
		t.Fatalf("前提: annotations と detached が 1 件ずつでない: %v", want)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list_annotations = %v\nwant (GET .../annotations) %v", got, want)
	}
}

func TestMCP_ListAnnotationRuns_MatchesAPI(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := seedCreatedBoard(t, r, mappings)

	got := callTool(t, connectMCP(t, r), "list_annotation_runs",
		map[string]any{"boardId": id, "annotationId": "annot-1"})

	want := apiJSON(t, r, "/api/boards/"+id+"/annotations/annot-1/runs")
	if list, _ := want.([]any); len(list) != 2 {
		t.Fatalf("前提: run が 2 件でない: %v", want)
	}
	if !reflect.DeepEqual(got["runs"], want) {
		t.Errorf("list_annotation_runs.runs = %v\nwant (GET .../runs) %v", got["runs"], want)
	}
}

// 見つからないものは /api と同じ code で返し、**本文に理由を載せない**
// （ADR 0016 / 0017）。認証ありの構成で開けるようになったとき（#185）、
// 他人のボードの存在を確かめる口にしないため。
func TestMCP_NotFound_IsToolErrorWithCode(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	session := connectMCP(t, r)

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"list_annotations", map[string]any{"boardId": "no-such-board"}},
		{"list_annotation_runs", map[string]any{"boardId": "no-such-board", "annotationId": "annot-1"}},
	}
	for _, tc := range cases {
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err != nil {
			t.Fatalf("%s %v: CallTool: %v", tc.tool, tc.args, err)
		}
		if !res.IsError {
			t.Errorf("%s %v: IsError = false, want true", tc.tool, tc.args)
			continue
		}
		if got, want := toolText(res), "not_found: not found"; got != want {
			t.Errorf("%s %v: text = %q, want %q", tc.tool, tc.args, got, want)
		}
	}
}

// ボードがあって注釈が無いときは、/api と同じく空の履歴を返す（ListRuns は
// シーンを見ずに run を引く）。MCP だけ not_found にすると、同じ問いに口ごとに
// 違う答えを返す。
func TestMCP_ListAnnotationRuns_UnknownAnnotationIsEmptyLikeAPI(t *testing.T) {
	t.Parallel()

	r, mappings := newRouter(t)
	id := seedCreatedBoard(t, r, mappings)

	got := callTool(t, connectMCP(t, r), "list_annotation_runs",
		map[string]any{"boardId": id, "annotationId": "no-such-annotation"})

	want := apiJSON(t, r, "/api/boards/"+id+"/annotations/no-such-annotation/runs")
	if !reflect.DeepEqual(got["runs"], want) || len(want.([]any)) != 0 {
		t.Errorf("runs = %v, want %v（空）", got["runs"], want)
	}
}

// 引数の欠けは道具に届く前に止まる。空の boardId で一覧を引き、全ボードの
// 何かが返る、という形にしない。
func TestMCP_MissingArguments_AreRejected(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	session := connectMCP(t, r)

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"list_annotations", map[string]any{}},
		{"list_annotation_runs", map[string]any{"boardId": "board-1"}},
	}
	for _, tc := range cases {
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err == nil && !res.IsError {
			t.Errorf("%s %v: 通った。引数の欠けは弾く", tc.tool, tc.args)
		}
	}
}

// 認証ありの構成で認可サーバー（ADR 0076）を組み立てていなければ開かない
// （ADR 0071）。利用者を決められないまま開くと、ログインせずに全ボードが読める。
func TestMCP_UnavailableWhenAuthIsConfigured(t *testing.T) {
	t.Parallel()

	r, _ := newAuthRouter(t, &stubProvider{})

	rec := request(t, r, http.MethodPost, "/mcp", loopbackHost,
		map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream"},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusServiceUnavailable, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "authentication") {
		t.Errorf("本文 = %q。認証ありの構成だから開いていないことを言う", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "list_boards") {
		t.Errorf("本文に道具の一覧が出ている: %q", rec.Body)
	}
}

// /mcp も ADR 0013 の検証の内側にある。SDK 自身の守り（DisableLocalhostProtection）は
// 外してあるので、ここが外れると DNS リバインディングで読める。
func TestMCP_RejectsCrossSite(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	headers := func(extra map[string]string) map[string]string {
		h := map[string]string{
			"Content-Type": "application/json",
			"Accept":       "application/json, text/event-stream",
		}
		for k, v := range extra {
			h[k] = v
		}
		return h
	}

	cases := map[string]struct {
		host    string
		headers map[string]string
	}{
		"外部のページからの fetch": {loopbackHost, headers(map[string]string{"Origin": "https://evil.example"})},
		"DNS リバインディング":    {"evil.example:8080", headers(nil)},
	}
	for name, tc := range cases {
		rec := request(t, r, http.MethodPost, "/mcp", tc.host, tc.headers, body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status = %d, want %d (%s)", name, rec.Code, http.StatusForbidden, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "list_boards") {
			t.Errorf("%s: 弾いたのに道具の一覧が出ている", name)
		}
	}
}
