package domain

import (
	"fmt"
	"strings"
)

// RuleID は解釈結果に課す制約 1 つを指す。
//
// **ValidationError は必ずどれかを名乗る。** 名乗らせているのは、検査と LLM への
// 指示が食い違わないようにするため。指示していない制約で弾くと、LLM は直しようの
// ない再送を繰り返す（ADR 0028）。
type RuleID string

// 制約の一覧。**検査を足すならここにも足す。** どちらか片方だけでは
// TestRules_MatchValidation が落ちる。
const (
	RuleSummary          RuleID = "summary"
	RuleItemsPresent     RuleID = "itemsPresent"
	RuleItemsCount       RuleID = "itemsCount"
	RuleKind             RuleID = "kind"
	RuleTitle            RuleID = "title"
	RuleTitleSingleLine  RuleID = "titleSingleLine"
	RuleTitleLength      RuleID = "titleLength"
	RuleBodyLength       RuleID = "bodyLength"
	RuleEpicTitleUnique  RuleID = "epicTitleUnique"
	RuleLocalID          RuleID = "localId"
	RuleLocalIDUnique    RuleID = "localIdUnique"
	RuleEpicHasNoParent  RuleID = "epicHasNoParent"
	RuleParentExists     RuleID = "parentExists"
	RuleParentIsEpic     RuleID = "parentIsEpic"
	RulePreviousRef      RuleID = "previousRef"
	RulePreviousItemID   RuleID = "previousItemId"
	RuleGranularityIssue RuleID = "granularityIssue"
	RuleGranularityEpic  RuleID = "granularityEpic"
)

// GitHub が draft issue に課している長さの上限。単位は Unicode のコードポイント。
//
// **これは GitHub が課す制約であって、etoki の好みではない。** 超えたものを
// 送ると作成の途中で弾かれ、そこで止まる。**作れたぶんは消せない**（ADR 0009）
// ので、構造が半分だけ GitHub に残る。宣言してあれば LLM に伝わり（ADR 0029）、
// 再送で直せる。
//
// **etoki が切り詰めない。弾く。** 切り詰める対象はブレストの中身そのもので、
// シーンの上限で縮小も切り捨てもしないと決めたのと同じ（ADR 0038）。
//
// 値の出どころは issue の上限（title 256 文字 / body 65536 コードポイント）で、
// 実測にもとづく公開の一覧から採った。**draft issue そのもので確かめた値では
// ない。** 公式ドキュメントには記載が無い。draft issue は issue に変換できる
// ので title が issue の上限を超えられるとは考えにくい、という推論を根拠に
// している。**確かめられたら、ここを直す。**
const (
	MaxTitleRunes = 256
	MaxBodyRunes  = 65536
)

// MaxItems は 1 回の解釈・作成で扱える項目数の上限（issue #147）。
//
// **GitHub が課す制約ではなく etoki の都合。** 作成はボード単位の排他
// （usecase.BoardLocks）を取ったまま進み、1 項目につき GitHub への書き込みが
// 最大 3 回走る。上限が無いと、1 回のリクエストでそのボードの作成と作成先の
// 変更を任意の長さ止められる。
//
// **切り詰めない。弾く**（MaxTitleRunes と同じ理由）。切り詰めると、作られな
// かった項目が「作った」と記録されないまま消える。
//
// 200 は、囲み 1 つから出てくる現実的な件数（数件〜数十件）に対して十分な
// 余裕がある一方、排他を握る時間には上限が付く、という線で選んでいる。
// **根拠のある値ではない。** 実際に詰まるなら動かす。
const MaxItems = 200

// Rule は制約 1 つと、それを LLM に伝える文。
type Rule struct {
	ID RuleID

	// Instruction は LLM に出す指示。**Validate が弾くものはここに書く。**
	//
	// 空文字は「LLM の出力では起こりえない」という意味。画面や API を直接叩く
	// 経路でだけ起きる検査がこれにあたる。伝えようのないものを伝えると、
	// 出しようのないフィールドの説明がプロンプトに混ざる。
	Instruction string
}

// Rules は制約の全件。プロンプトの制約一覧はここから組み立てる。
//
// **並び順がそのままプロンプトの並び順になる。** 読み手が構造から順に辿れるよう、
// 全体にかかるものから項目ごとのものへ降りる。
var Rules = []Rule{
	{RuleKind, `kind は "epic" か "issue" のどちらかです。ほかの値は使えません。`},
	{RuleParentIsEpic, "階層は epic と issue の 2 段だけです。epic の下に epic は作れず、" +
		"issue を親にはできません。"},
	{RuleEpicHasNoParent, "epic の parentLocalId は必ず null です。"},
	{RuleParentExists, "issue の parentLocalId には親となる epic の localId を書きます。" +
		"どの epic にも属さない issue は null にします。存在しない localId や自分自身は指せません。"},
	{RuleLocalIDUnique, "localId は出力の中で一意にしてください。GitHub 上の ID は作成後にしか" +
		"決まらないため、親子関係はこの一時 ID で表します。"},
	{RuleLocalID, "localId は空にできません。"},
	{RuleTitle, "title は空にできません。"},
	{RuleTitleSingleLine, "title に改行を入れないでください。draft issue のタイトルは" +
		"一覧や通知で 1 行として扱われ、どこで折り返るか（あるいは切られるか）は" +
		"GitHub 側の都合です。複数行にしたい内容は body に書いてください。"},
	{RuleTitleLength, fmt.Sprintf("title は Unicode コードポイントで %d 個までです。超えると"+
		"GitHub が受け付けず、そこまでに作ったものだけが残ります。入りきらない内容は body に"+
		"移してください。", MaxTitleRunes)},
	{RuleBodyLength, fmt.Sprintf("body は Unicode コードポイントで %d 個までです。超えると"+
		"GitHub が受け付けず、そこまでに作ったものだけが残ります。", MaxBodyRunes)},
	{RuleEpicTitleUnique, "epic の title は出力の中で一意にしてください。子は親の epic を" +
		"タイトルで指すため、同じタイトルの epic が 2 つあると、どちらの配下なのか区別が" +
		"付かなくなります。**空白の違いだけでは別のタイトルになりません。** " +
		"前後の空白と Unicode の正規化形の違いは同じタイトルとして扱います。" +
		"issue の title は重複していても構いません。"},
	{RuleItemsPresent, "items は少なくとも 1 件必要です。囲んだ範囲から作るものが 1 つも" +
		"無い、という出力はしないでください。"},
	{RuleItemsCount, fmt.Sprintf("items は %d 件までです。これを超えるなら、囲む範囲を"+
		"分けてもらう前提で、いまの範囲から読み取れるぶんだけを出してください。", MaxItems)},
	{RuleSummary, "summary は GitHub には作りません。解釈が意図どおりかを開発者が確かめる" +
		"ためのものなので、何をどうまとめたのかが分かる文にしてください。空にはできません。"},
	{RulePreviousRef, "previousRef は、前回までに作ったものを書き換える場合にだけ、その ID" +
		"（p1 など）を入れます。**前回の一覧を渡していないときは必ず null です。** " +
		"新しく作るものも null です。1 つの ID を 2 つの項目に使わないでください。"},
	{RuleGranularityIssue, "粒度に issue が指定されているときは、epic を作らず issue だけを" +
		"出力してください。"},
	{RuleGranularityEpic, "粒度に epic が指定されているときは、epic を少なくとも 1 件" +
		"含めてください。"},
	// previousItemId は LLM が出す値ではない。LLM には previousRef を出させ、
	// ParseInterpretation が解決する（ADR 0026）。画面や API を直接叩く経路で
	// だけ検査に掛かるので、指示は無い。
	{RulePreviousItemID, ""},
}

// InterpretationConstraints は Rules を LLM に出す制約一覧の形にする。
//
// **プロンプト側で書き写さない。** 写すと、検査を足したときに片方だけ古くなる。
func InterpretationConstraints() string {
	var b strings.Builder
	for _, r := range Rules {
		if r.Instruction == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(r.Instruction)
		b.WriteString("\n")
	}
	return b.String()
}

// newValidationError は指摘 1 件を組み立てる。
//
// **構造体リテラルではなくこれを通す。** リテラルだと Rule を書き忘れても
// コンパイルが通り、どの制約に対応するのか分からない指摘が混ざる。
// contestedMessage は「1 つにつき 1 件」を破ったときの指摘文。
//
// **検査そのものは 2 箇所から消せない。** 解釈の出口（resolvePreviousRefs）と
// 作成のリクエスト（validateItems）は別経路で、後者は画面から来るので前者を
// 通らない。寄せられるのは同じ日本語を 2 度書いているほうで、片方だけ言い回しを
// 変えると、同じ規則の違反が経路によって別の文言で返る。
func contestedMessage(fieldName, value, owner string) string {
	return fmt.Sprintf("%s %q を %q と取り合っています。1 つにつき 1 件までです",
		fieldName, value, owner)
}

func newValidationError(rule RuleID, field, message string) ValidationError {
	return ValidationError{Rule: rule, Field: field, Message: message}
}

// ValidationError は解釈結果の検証で見つかった問題 1 件。
type ValidationError struct {
	// Rule はこの指摘が対応する制約。LLM への指示と検査を結ぶ。
	Rule RuleID

	// Field は問題のある場所。"items[2].title" のような形を取る。
	Field string
	// Message は何が問題かの説明。
	//
	// この文はそのまま LLM への修正指示になる。人間向けの説明ではなく、
	// 「何をどう直せばよいか」が読み取れる文にする。
	Message string
}

// Error は "場所: 説明" の形の 1 行を返す。
func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// ValidationErrors は検証で見つかった問題の全件。
//
// スライスをそのままエラーにしているのは、再送時に呼び出し側が 1 件ずつ
// 取り出して修正指示を組み立てられるようにするため。最初の 1 件で打ち切ると、
// LLM は 1 往復で 1 箇所しか直せない（ADR 0005）。
type ValidationErrors []ValidationError

// Error は全件を改行区切りで連結して返す。
func (errs ValidationErrors) Error() string {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}
