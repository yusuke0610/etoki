package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/yusuke0610/etoki/internal/httpapi/apitypes"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// ボードの背景（ADR 0093）の口のテスト。

func premisePath(boardID string) string {
	return "/api/boards/" + boardID + "/premise"
}

// 背景だけを書き、正規化した背景を返す。**版（updatedAt）は動かさない**
// （ADR 0092）。動かすと、そのボードを開いている別のメンバーの次の保存が
// 409 で断られる。
func TestSetBoardPremise(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	before := decode[apitypes.BoardDetail](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if before.Premise != "" {
		t.Fatalf("作った直後の premise = %q, want 空", before.Premise)
	}

	got := decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPut, premisePath(id),
		map[string]string{"premise": "  技術: Go\r\n用語: 絵  ", "basePremise": ""}))
	if got.Premise != "技術: Go\n用語: 絵" {
		t.Errorf("premise = %q, want 正規化した背景", got.Premise)
	}
	if !got.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updatedAt = %v, want %v（背景で進めてはいけない）", got.UpdatedAt, before.UpdatedAt)
	}
	if got.Name != before.Name || got.Scene != before.Scene || got.ProjectID != before.ProjectID {
		t.Errorf("背景以外が変わっている: %+v", got)
	}

	// 開く口（BoardWithFiles）にも載る。写し漏れると、開き直したときに背景が
	// 空に見え、次の書き換えが必ず 409 になる。
	opened := decodeOK[apitypes.BoardWithFiles](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if opened.Premise != "技術: Go\n用語: 絵" {
		t.Errorf("開いたボードの premise = %q", opened.Premise)
	}

	// 背景を書く前に開いていた人の保存は、同じ基準のまま通る。**版を進めない
	// 理由そのもの。** 進める実装にすると、ここが 409 で落ちる。
	if rec := do(t, r, http.MethodPut, "/api/boards/"+id+"/scene",
		saveSceneBody(annotatedScene, fixedTime)); rec.Code != http.StatusOK {
		t.Fatalf("背景のあとの保存: status = %d, want 200（背景で版を進めている） %s", rec.Code, rec.Body)
	}
}

// 編集を始めた時点の背景と食い違えば 409（premise_conflict）で、書かない。
func TestSetBoardPremise_Conflict(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPut, premisePath(id),
		map[string]string{"premise": "先に書いた背景", "basePremise": ""}))

	rec := do(t, r, http.MethodPut, premisePath(id),
		map[string]string{"premise": "古い本文から書いた背景", "basePremise": ""})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (%s)", rec.Code, http.StatusConflict, rec.Body)
	}
	// シーンの衝突（scene_conflict）とは打ち手が違う（読み直すのは背景）。
	if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodePremiseConflict {
		t.Errorf("code = %q, want %q", code, apitypes.ErrorCodePremiseConflict)
	}

	got := decodeOK[apitypes.BoardWithFiles](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
	if got.Premise != "先に書いた背景" {
		t.Errorf("premise = %q, want 先に書いた背景のまま", got.Premise)
	}
}

// 長すぎる背景は 413（premise_too_large）。本文が口の読み込みの上限に当たった
// ときも同じ code にする。**同じ「長すぎる」が本文の大きさしだいで 2 通りの
// code に割れない**（.claude/rules/api-contract.md）。
func TestSetBoardPremise_TooLarge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		premise string
	}{
		{name: "上限を 1 文字超える", premise: strings.Repeat("あ", usecase.MaxPremiseRunes+1)},
		// 口の読み込みの上限（数十万バイト規模）を超える本文。ユースケースまで届かない。
		{name: "読み込みの上限を超える本文", premise: strings.Repeat("a", 1<<20)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, _ := newRouter(t)
			id := createBoard(t, r, "ボード")

			rec := do(t, r, http.MethodPut, premisePath(id),
				map[string]string{"premise": tt.premise, "basePremise": ""})
			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d (%.200s)", rec.Code, http.StatusRequestEntityTooLarge, rec.Body)
			}
			if code := decode[apitypes.ErrorResponse](t, rec).Code; code != apitypes.ErrorCodePremiseTooLarge {
				t.Errorf("code = %q, want %q", code, apitypes.ErrorCodePremiseTooLarge)
			}

			got := decodeOK[apitypes.BoardWithFiles](t, do(t, r, http.MethodGet, "/api/boards/"+id, nil))
			if got.Premise != "" {
				t.Errorf("弾いたのに書かれている: premise の長さ = %d", len(got.Premise))
			}
		})
	}
}

// 上限ちょうどの背景は、エスケープの最悪の書き方で送っても読み込みの上限で
// 落ちない。BMP の外の文字をサロゲートペアで書くと 1 文字が 12 バイトになり、
// 照合用の編集前の背景も同じだけ載る。歯止めを小さく取ると、減らす必要の
// ないものを「長すぎる」と弾く（.claude/rules/validation-boundaries.md）。
func TestSetBoardPremise_AtLimitWithWorstEscapesPasses(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")

	// U+1F600 と U+1F601。どちらも BMP の外の文字で、JSON ではサロゲートペアの
	// エスケープ 2 つ（12 バイト）で書ける。
	first, second := rune(0x1F600), rune(0x1F601)
	escaped := strings.Repeat(jsonSurrogateEscape(first), usecase.MaxPremiseRunes)
	replaced := strings.Repeat(jsonSurrogateEscape(second), usecase.MaxPremiseRunes)
	if len(escaped) != 12*usecase.MaxPremiseRunes {
		t.Fatalf("エスケープ列の長さ = %d, want %d（最悪の書き方になっていない）",
			len(escaped), 12*usecase.MaxPremiseRunes)
	}

	// 1 回目で上限ちょうどの背景を置く。
	decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPut, premisePath(id),
		json.RawMessage(`{"premise":"`+escaped+`","basePremise":""}`)))

	// 2 回目は新しい背景も編集前の背景も、最悪の書き方で上限ちょうど。
	got := decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPut, premisePath(id),
		json.RawMessage(`{"premise":"`+replaced+`","basePremise":"`+escaped+`"}`)))
	if got.Premise != strings.Repeat(string(second), usecase.MaxPremiseRunes) {
		t.Errorf("書き換わっていない: premise の文字数 = %d", len([]rune(got.Premise)))
	}
}

// jsonSurrogateEscape は BMP の外の文字 1 つを、JSON のサロゲートペアの
// エスケープ（12 バイト）にする。エスケープの最悪の書き方を作るために使う。
func jsonSurrogateEscape(r rune) string {
	hi, lo := utf16.EncodeRune(r)
	backslash := string(rune(0x5C))
	return fmt.Sprintf("%su%04x%su%04x", backslash, hi, backslash, lo)
}

func TestSetBoardPremise_NotFound(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)

	rec := do(t, r, http.MethodPut, premisePath("no-such-id"),
		map[string]string{"premise": "背景", "basePremise": ""})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// 一覧（BoardSummary）には背景を載せない。一覧の画面は背景を出さないので、
// 全ボードぶんを返しても捨てるだけになる。
func TestListBoards_OmitsPremise(t *testing.T) {
	t.Parallel()

	r, _ := newRouter(t)
	id := createBoard(t, r, "ボード")
	decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPut, premisePath(id),
		map[string]string{"premise": "一覧には出ない背景", "basePremise": ""}))

	rec := do(t, r, http.MethodGet, "/api/boards", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "premise") {
		t.Errorf("一覧に premise が載っている: %s", rec.Body)
	}
}

// 解釈の結果に、渡した背景と別の絵から作ったものを載せる（ADR 0093）。
// 何を渡したかが見えないと、出力がなぜそうなったか読めない（中核思想 3）。
func TestInterpretAnnotation_ReturnsPassedContext(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	r := newInterpretRouter(t, llm)
	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)
	decodeOK[apitypes.BoardDetail](t, do(t, r, http.MethodPut, premisePath(id),
		map[string]string{"premise": "技術: Go", "basePremise": ""}))

	job := interpretAndWait(t, r, id, "annot-1", nil)
	if job.Status != apitypes.InterpretationJobStatusSucceeded || job.Result == nil {
		t.Fatalf("job = %+v, want succeeded with result", job)
	}

	ctx := job.Result.Context
	if ctx.Premise != "技術: Go" {
		t.Errorf("context.premise = %q, want 技術: Go", ctx.Premise)
	}
	// 別の絵から何も作っていなくても、一覧は null ではなく空の配列で返す。
	if ctx.OtherItems == nil || len(ctx.OtherItems) != 0 || ctx.OmittedOtherItems != 0 {
		t.Errorf("context の一覧 = %+v / %d, want 空の配列と 0", ctx.OtherItems, ctx.OmittedOtherItems)
	}
	if !strings.Contains(llm.last.Text, "> 技術: Go") {
		t.Errorf("LLM に背景が渡っていない:\n%s", llm.last.Text)
	}
}

// 別の絵から作ったものは、種別とタイトルで結果に載る。
func TestInterpretAnnotation_ReturnsOtherFramesItems(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{text: validInterpretation}
	boards, mappings := newRepos(t)
	r := newInterpretRouterOn(t, llm, boards, mappings)
	id := createBoard(t, r, "設計会")
	saveAnnotatedScene(t, r, id)

	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID: id, AnnotationID: "annot-other", ContentHash: "h",
		CreatedAt: fixedTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{{
			ItemID: "PVTI_other", Kind: port.KindEpic, Title: "認証の整備", LocalID: "e1",
			Action: port.ActionCreated, Confirmed: true, CreatedAt: fixedTime,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	job := interpretAndWait(t, r, id, "annot-1", nil)
	if job.Result == nil {
		t.Fatalf("job = %+v, want 結果あり", job)
	}
	want := []apitypes.OtherItem{{Kind: apitypes.ItemKindEpic, Title: "認証の整備"}}
	got := job.Result.Context.OtherItems
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("context.otherItems = %+v, want %+v", got, want)
	}
}
