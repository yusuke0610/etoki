package fakeupstream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// issueOnlyLine は解釈のメッセージで粒度に issue が指定されたときの 1 行。
//
// 偽物がプロンプトの文言を読むのはここと previousLine だけ。文言が変わると出力が
// 検査に落ちるので、usecase の契約テスト
// （internal/usecase/fakeupstream_contract_test.go）が落ちて気づける。
//
// **行ごとの一致を見る。部分一致にしない。** 指示の節には、利用者の書いた文字も
// 入る（ボードの背景、別の絵から作ったもののタイトル、前回ぶんのタイトルと本文、
// ADR 0093）。どれも行頭に印（"> " / "- " / 字下げ）が付くので、この 1 行と
// 丸ごと一致することはない。部分一致にすると、背景に「issue 相当」と書いた
// だけで epic が消える。
var issueOnlyLine = regexp.MustCompile(`(?m)^開発者はこの範囲を issue 相当と指定しています。$`)

// 空行は「囲みに含まれるテキスト」の節と、その後ろに buildUserMessage が
// 組み立てた指示の境目。テキストの改行は空白に潰されるので、付箋の中には
// 現れない。
const sectionSeparator = "\n\n"

// previousLine は「前回までにこの囲みから作ったもの」の 1 行を読む。
//
// **行頭の "- p1 (epic) " の形は、前回ぶんの一覧にしか出ない。** 背景の行は
// "> " で始まり、別の絵の一覧は ref を持たない（"- (issue) …"）。どちらも
// buildUserMessage がそう組み立てる。だから見出しで節を切らずに、指示の節
// 全体からこの形の行を拾う。
var previousLine = regexp.MustCompile(`(?m)^- (p\d+) \((epic|issue)\) `)

// textLine は「囲みに含まれるテキスト」の 1 行を読む。
var textLine = regexp.MustCompile(`(?m)^- (.+)$`)

type wireItem struct {
	LocalID       string  `json:"localId"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	Body          string  `json:"body"`
	ParentLocalID *string `json:"parentLocalId"`
	PreviousRef   *string `json:"previousRef"`
}

type wireInterpretation struct {
	Summary string     `json:"summary"`
	Items   []wireItem `json:"items"`
}

// Interpretation は解釈のユーザーメッセージから、決め打ちの解釈結果 JSON を作る。
//
// 形は epic 1 件と、その配下の issue 2 件。粒度に issue が指定されていれば
// epic を出さない。囲みの先頭のテキストをタイトルに混ぜるので、テキストを
// 直して解釈し直すと出力も変わる（更新の経路で差分が見える）。
//
// 前回ぶんの ref が載っていれば、同じ種別の項目に並び順で振る。**これは
// 通しで更新を確かめるための決め打ちで、対応づけの推測ではない。** 本物の
// 解釈では LLM が決め、開発者が選ぶ（ADR 0026）。
func Interpretation(userText string) string {
	// 先頭の行は囲みの名前なので、それだけだと中身を直してもタイトルが
	// 変わらない。本文の行までつなぐ。
	var lines []string
	for _, m := range textLine.FindAllStringSubmatch(textSection(userText), -1) {
		lines = append(lines, strings.TrimSpace(m[1]))
	}
	head := "ブレスト"
	if len(lines) > 0 {
		head = truncateRunes(strings.Join(lines, " / "), 40)
	}

	// **マーカーは指示の節でだけ探す。** 付箋には何でも書けるので、
	// 「issue 相当」や前回一覧の行をそのまま書いた付箋がありうる。
	// 全文を見ると、それだけで粒度の判断と前回ぶんの解決が変わる。
	control := controlSection(userText)

	refs := map[string][]string{}
	for _, m := range previousLine.FindAllStringSubmatch(control, -1) {
		refs[m[2]] = append(refs[m[2]], m[1])
	}
	take := func(kind string) *string {
		if len(refs[kind]) == 0 {
			return nil
		}
		ref := refs[kind][0]
		refs[kind] = refs[kind][1:]
		return &ref
	}

	var items []wireItem
	var parent *string
	if !issueOnlyLine.MatchString(control) {
		epicID := "e1"
		parent = &epicID
		items = append(items, wireItem{
			LocalID: epicID, Kind: "epic",
			Title:       head + " をまとめる",
			Body:        "偽の LLM が決め打ちで返した epic。",
			PreviousRef: take("epic"),
		})
	}
	for i := 1; i <= 2; i++ {
		items = append(items, wireItem{
			LocalID:       fmt.Sprintf("i%d", i),
			Kind:          "issue",
			Title:         fmt.Sprintf("%s の作業 %d", head, i),
			Body:          "偽の LLM が決め打ちで返した issue。",
			ParentLocalID: parent,
			PreviousRef:   take("issue"),
		})
	}

	out, _ := json.Marshal(wireInterpretation{
		Summary: fmt.Sprintf("「%s」を偽の LLM が決め打ちで読んだ。", head),
		Items:   items,
	})
	return string(out)
}

// textSection は「囲みに含まれるテキスト」の節だけを切り出す。前回ぶんの一覧も
// "- " で始まるので、切り出さないとそちらの行までタイトルに混ざる。
func textSection(userText string) string {
	section, _, _ := strings.Cut(userText, sectionSeparator)
	return section
}

// controlSection は buildUserMessage が組み立てた指示の節を切り出す。
// textSection の裏返しで、**付箋の文字が入らない側**。
//
// 偽物が読むマーカーはすべてここで探す。全文を見ると、付箋に書いた
// 「issue 相当」で epic が消え、付箋に書いた前回一覧の行で存在しない ref が
// 出る。どちらも本物の検査に落ちて、通しが理由の見えない失敗になる。
//
// **この節にも利用者の書いた文字は入る**（ボードの背景、別の絵や前回ぶんの
// タイトル）。それらは行頭に印が付くので、マーカーは行の形で見分ける
// （issueOnlyLine / previousLine）。
func controlSection(userText string) string {
	_, section, _ := strings.Cut(userText, sectionSeparator)
	return section
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

type messagesRequest struct {
	Messages []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"messages"`
}

// handleMessages は Anthropic Messages API 形状で解釈結果を返す。
//
// 図のドラフト生成も同じ口に来るが区別しない。解釈と同じ JSON を返すので、
// 画面では mermaid として読めずに失敗する（通しの確認の対象外）。
func handleMessages(w http.ResponseWriter, r *http.Request) {
	var req messagesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"type":  "error",
			"error": map[string]string{"type": "invalid_request_error", "message": err.Error()},
		})
		return
	}

	var text strings.Builder
	for _, m := range req.Messages {
		for _, c := range m.Content {
			if c.Type == "text" {
				text.WriteString(c.Text)
			}
		}
	}

	out := Interpretation(text.String())
	writeJSON(w, http.StatusOK, map[string]any{
		"type":        "message",
		"role":        "assistant",
		"content":     []map[string]string{{"type": "text", "text": out}},
		"stop_reason": "end_turn",
		"usage":       map[string]int{"input_tokens": len(text.String()), "output_tokens": len(out)},
	})
}
