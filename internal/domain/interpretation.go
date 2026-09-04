package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// ItemKind は draft issue の種別。
//
// port.ItemKind と同じ値を持つが、あちらは境界の DTO であり、こちらは
// ドメインモデルである。port は internal/ に依存しないため型を共有できず、
// 詰め替えはユースケース層が行う。ContentHash が port では string に
// なっているのと同じ扱い（ADR 0001）。
type ItemKind string

// ItemKind の取りうる値。
//
// etoki が GitHub に作るのは epic と issue の 2 階層のみ（ADR 0006）。
const (
	KindEpic  ItemKind = "epic"
	KindIssue ItemKind = "issue"
)

// Valid は k が定義済みの種別かどうかを返す。
func (k ItemKind) Valid() bool {
	return k == KindEpic || k == KindIssue
}

// Interpretation は LLM が注釈範囲を解釈した結果。
type Interpretation struct {
	// Summary は LLM がこの囲みをどう解釈したかの説明。
	//
	// GitHub には作らない。issue 化を実行する前に開発者へ見せる確認材料
	// としてのみ使う（ADR 0006）。
	Summary string `json:"summary"`

	// Items は作成対象の draft issue。
	Items []InterpretedItem `json:"items"`
}

// InterpretedItem は解釈結果に含まれる draft issue 1 件。
type InterpretedItem struct {
	// LocalID は LLM が採番する一時 ID。1 つの解釈結果の中で一意。
	//
	// GitHub の ID は作成後にしか決まらないため、親子関係はこの一時 ID で表す。
	LocalID string `json:"localId"`

	// Kind は epic か issue。
	Kind ItemKind `json:"kind"`

	// Title は draft issue のタイトル。
	Title string `json:"title"`

	// Body は draft issue の本文。空でもよい。
	Body string `json:"body"`

	// ParentLocalID は親の LocalID。親を持たないなら nil。
	//
	// epic は必ず nil。issue は epic を親に取るか、単独で立つ。
	ParentLocalID *string `json:"parentLocalId"`

	// PreviousItemID は書き換える対象の ProjectV2Item ID。新規なら nil。
	//
	// **LLM が直接出す値ではない。** LLM には `previousRef`（`p1` のような短い
	// ID）を出させ、ParseInterpretation がここへ解決する（ADR 0026）。node ID は
	// 長く不透明で、復唱させると取り違える。
	//
	// **これが指す先が本当にその注釈のものかは、ここでは分からない。**
	// 作成時に CreationService が畳み込み集合と突き合わせる。省くと、リクエストに
	// 任意の node ID を書いて無関係な draft issue を書き換えられる。
	PreviousItemID *string `json:"previousItemId"`
}

// PreviousItem は前回までにその注釈が GitHub に作らせたもの 1 件。
//
// 解釈のときに LLM へ見せ、「これは前回のどれの更新か」を答えさせる材料にする。
// 構造の対応づけをルールベースで推測せず LLM に解釈させるのは、座標から構造を
// 推測しないのと同じ判断（中核思想 2、ADR 0026）。
type PreviousItem struct {
	// Ref は LLM に見せる短い ID。1 回の解釈の中でだけ通じる。
	Ref string
	// ItemID は GitHub の ProjectV2Item ID。Ref はこれに解決される。
	ItemID string
	// Kind は前回作ったときの種別。
	Kind ItemKind
	// Title と Body は前回作った時点のスナップショット（ADR 0023）。
	Title string
	Body  string
}

// interpretedItemWire は LLM の出力そのままの形。
//
// **境界の DTO と分ける。** LLM には短い ref を出させ、外へ出るのは解決済みの
// item ID にする（ADR 0026）。1 つの型に両方を持たせると、どちらが入っている
// のかが読む場所によって変わる。
type interpretedItemWire struct {
	LocalID       string   `json:"localId"`
	Kind          ItemKind `json:"kind"`
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	ParentLocalID *string  `json:"parentLocalId"`
	PreviousRef   *string  `json:"previousRef"`
}

// interpretationWire は LLM の出力そのままの形。
type interpretationWire struct {
	Summary string                `json:"summary"`
	Items   []interpretedItemWire `json:"items"`
}

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
	{RuleTitleLength, fmt.Sprintf("title は %d 文字までです。超えると GitHub が受け付けず、"+
		"そこまでに作ったものだけが残ります。入りきらない内容は body に移してください。",
		MaxTitleRunes)},
	{RuleBodyLength, fmt.Sprintf("body は %d 文字までです。超えると GitHub が受け付けず、"+
		"そこまでに作ったものだけが残ります。", MaxBodyRunes)},
	{RuleEpicTitleUnique, "epic の title は出力の中で一意にしてください。子は親の epic を" +
		"タイトルで指すため、同じタイトルの epic が 2 つあると、どちらの配下なのか区別が" +
		"付かなくなります。**空白の違いだけでは別のタイトルになりません。** " +
		"前後の空白と Unicode の正規化形の違いは同じタイトルとして扱います。" +
		"issue の title は重複していても構いません。"},
	{RuleItemsPresent, "items は少なくとも 1 件必要です。囲んだ範囲から作るものが 1 つも" +
		"無い、という出力はしないでください。"},
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

// ParseInterpretation は LLM の出力 JSON を読む。
//
// 未知のフィールドと JSON の後ろに続く余分な出力をどちらも誤りとして扱う。
// 素通しにすると parentLocalId を parentId と綴り間違えた出力が「親なし」として
// 通ってしまい、構造が黙って壊れる。ここで弾いておけば、綴り間違いがそのまま
// 再送時の修正指示になる。
// previous は前回までにその注釈が作らせたもの。空なら `previousRef` を許さない。
func ParseInterpretation(raw []byte, previous []PreviousItem) (Interpretation, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var wire interpretationWire
	if err := dec.Decode(&wire); err != nil {
		return Interpretation{}, fmt.Errorf("parse interpretation: %w", err)
	}
	if dec.More() {
		return Interpretation{}, errors.New("parse interpretation: JSON の後ろに余分な出力があります")
	}

	return resolvePreviousRefs(wire, previous)
}

// resolvePreviousRefs は LLM が出した previousRef を item ID に解決する。
//
// **問題は ValidationErrors で返す。** 綴り間違いや存在しない ref は、そのまま
// 修正指示になって再送で直せる（ADR 0005）。パースの失敗として扱うと、1 往復
// あたり 1 箇所しか直せない。
func resolvePreviousRefs(wire interpretationWire, previous []PreviousItem) (Interpretation, error) {
	itemIDByRef := make(map[string]string, len(previous))
	for _, p := range previous {
		itemIDByRef[p.Ref] = p.ItemID
	}

	in := Interpretation{
		Summary: wire.Summary,
		Items:   make([]InterpretedItem, 0, len(wire.Items)),
	}

	var errs ValidationErrors
	// 1 つの draft issue を 2 件で奪い合えない。通すと、あとに処理したほうの
	// 内容だけが残り、もう片方は「作ったつもり」で消える。
	claimedBy := make(map[string]string, len(previous))

	for i, w := range wire.Items {
		item := InterpretedItem{
			LocalID:       w.LocalID,
			Kind:          w.Kind,
			Title:         w.Title,
			Body:          w.Body,
			ParentLocalID: w.ParentLocalID,
		}

		if w.PreviousRef != nil {
			field := fmt.Sprintf("items[%d].previousRef", i)
			ref := *w.PreviousRef

			itemID, known := itemIDByRef[ref]
			owner, claimed := claimedBy[ref]

			switch {
			case !known && len(previous) == 0:
				errs = append(errs, newValidationError(RulePreviousRef, field,
					"この囲みから作ったものはまだありません。previousRef は null にしてください"))
			case !known:
				errs = append(errs, newValidationError(RulePreviousRef, field, fmt.Sprintf(
					"previousRef %q に対応するものがありません。前回作成したものの ID か null にしてください", ref)))
			case claimed:
				errs = append(errs, newValidationError(RulePreviousRef, field,
					contestedMessage("previousRef", ref, owner)))
			default:
				claimedBy[ref] = w.LocalID
				item.PreviousItemID = &itemID
			}
		}

		in.Items = append(in.Items, item)
	}

	if len(errs) > 0 {
		return Interpretation{}, errs
	}

	return in, nil
}

// Validate は解釈結果が 2 階層の制約と粒度指定を満たすか検べる。
//
// 問題があれば ValidationErrors を返す。最初の 1 件で打ち切らず全件を集める。
// 問題がなければ nil を返す。
func (in Interpretation) Validate(g Granularity) error {
	var errs ValidationErrors

	if strings.TrimSpace(in.Summary) == "" {
		errs = append(errs, newValidationError(RuleSummary, "summary",
			"この囲みをどう解釈したかの説明を入れてください"))
	}
	if len(in.Items) == 0 {
		errs = append(errs, newValidationError(RuleItemsPresent, "items",
			"少なくとも 1 件の項目が必要です"))
	}

	errs = append(errs, validateItems(in.Items)...)
	errs = append(errs, validateGranularity(in.Items, g)...)

	// 型付き nil スライスをそのまま返すと、error インターフェースが
	// 非 nil になってしまう。
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// validateItems は項目ごとの制約と親子関係を検べる。
func validateItems(items []InterpretedItem) ValidationErrors {
	// 親の解決には全体を先に見る必要があるので、種別表を作ってから回す。
	// 重複する localId は先勝ちにする。重複そのものは下のループで報告する。
	kindByLocalID := make(map[string]ItemKind, len(items))
	for _, it := range items {
		if it.LocalID == "" {
			continue
		}
		if _, dup := kindByLocalID[it.LocalID]; !dup {
			kindByLocalID[it.LocalID] = it.Kind
		}
	}

	var errs ValidationErrors
	seen := make(map[string]struct{}, len(items))
	// epic のタイトルから localId を引く表。**先に出たほうを残す。**
	//
	// 親は epic のタイトル文字列で指す（ADR 0006）ので、同名の epic が並ぶと
	// その配下は GitHub 上で 1 つにまとまり、どちらの子なのか区別が付かなく
	// なる。draft issue は削除できないため、作ってからでは取り返せない。
	epicTitles := make(map[string]string, len(items))
	// 更新先の取り合いはここでも見る。解釈の出口では resolvePreviousRefs が
	// 弾いているが、作成のリクエストは画面から送られてくるので、そこを通らない
	// 経路でも 1 つの draft issue に 2 件が向かうのを防ぐ必要がある。
	claimed := make(map[string]string, len(items))

	for i, it := range items {
		field := func(name string) string { return fmt.Sprintf("items[%d].%s", i, name) }

		if it.PreviousItemID != nil {
			switch owner, dup := claimed[*it.PreviousItemID]; {
			case *it.PreviousItemID == "":
				errs = append(errs, newValidationError(RulePreviousItemID, field("previousItemId"),
					"previousItemId を空文字にはできません。新規なら null にしてください"))
			case dup:
				errs = append(errs, newValidationError(RulePreviousItemID, field("previousItemId"),
					contestedMessage("previousItemId", *it.PreviousItemID, owner)))
			default:
				claimed[*it.PreviousItemID] = it.LocalID
			}
		}

		if !it.Kind.Valid() {
			errs = append(errs, newValidationError(RuleKind, field("kind"),
				fmt.Sprintf("kind は %q か %q のどちらかです（%q が指定されています）", KindEpic, KindIssue, it.Kind)))
		}

		// 改行コードだけを畳んだ形と、重複判定に使う正規形を分けて持つ。
		// 前者で改行の有無を見る。normalizeTitle は前後の空白を落とすので、
		// そちらで見ると "\n認証\n" のような前後だけの改行が消えて通る。
		lines := normalizeText(it.Title)
		title := normalizeTitle(it.Title)
		switch owner, dup := epicTitles[title]; {
		case title == "":
			errs = append(errs, newValidationError(RuleTitle, field("title"),
				"title を空にはできません"))

		case strings.Contains(lines, "\n"):
			// **畳まずに弾く。** 改行を空白に置き換えて通すと、開発者が確認した
			// ものと違う文字列で作ることになる（中核思想 3）。作った draft issue は
			// 消せない（ADR 0009）ので、直させるのは作る前しかない。
			//
			// 弾くのは改行だけに絞る。タブやゼロ幅スペースまで広げると
			// 「タイトルに使ってよい文字」を etoki が決めることになり、
			// 画面上に現れる違いは触らないという線（ADR 0028）を越える。
			errs = append(errs, newValidationError(RuleTitleSingleLine, field("title"),
				"title に改行を入れることはできません。1 行にまとめるか、"+
					"入りきらない内容は body に移してください"))

		case it.Kind != KindEpic:
			// issue のタイトルは重複してよい。親として指されないので、
			// 同名でも構造は壊れない。

		case dup:
			errs = append(errs, newValidationError(RuleEpicTitleUnique, field("title"), fmt.Sprintf(
				"epic の title %q が localId %q のものと重複しています。子は親の"+
					"epic をタイトルで指すため、どちらの配下なのか区別が付かなく"+
					"なります。どちらかを別のタイトルにしてください", it.Title, owner)))

		default:
			epicTitles[title] = it.LocalID
		}

		// 長さは上の switch とは別に見る。**あちらは 1 件だけを報告する形**
		// なので、混ぜると「改行があって、かつ長すぎる」title の片方しか
		// 返らず、再送で 1 往復あたり 1 箇所しか直せない（ADR 0005）。
		//
		// 数えるのは送る文字列そのもの。前後の空白を落として数えると、
		// GitHub に届くのより短い値で判定することになる。
		if n := utf8.RuneCountInString(it.Title); n > MaxTitleRunes {
			errs = append(errs, newValidationError(RuleTitleLength, field("title"), fmt.Sprintf(
				"title が %d 文字あります。GitHub 側の上限は %d 文字です。"+
					"入りきらない内容は body に移してください", n, MaxTitleRunes)))
		}
		if n := utf8.RuneCountInString(it.Body); n > MaxBodyRunes {
			errs = append(errs, newValidationError(RuleBodyLength, field("body"), fmt.Sprintf(
				"body が %d 文字あります。GitHub 側の上限は %d 文字です", n, MaxBodyRunes)))
		}

		if it.LocalID == "" {
			errs = append(errs, newValidationError(RuleLocalID, field("localId"),
				"localId を空にはできません"))
		} else {
			if _, dup := seen[it.LocalID]; dup {
				errs = append(errs, newValidationError(RuleLocalIDUnique, field("localId"),
					fmt.Sprintf("localId %q が重複しています。項目ごとに別の値にしてください", it.LocalID)))
			}
			seen[it.LocalID] = struct{}{}
		}

		errs = append(errs, validateParent(it, kindByLocalID, field)...)
	}

	return errs
}

// normalizeTitle は epic のタイトルの重複判定に使う正規形を返す。
//
// **ここが規則の正本。** 判断の理由は ADR 0028 にあるが、あちらが持つのは基準
// （人に見分けが付くかどうか）だけで、何が畳まれるかは書いていない。列挙を
// 2 箇所に置くと、normalizeText が変わったときに片方が古いまま残る。
//
// 畳む:
//
//   - Unicode の合成済みと結合文字（NFC と NFD の "ガード"）
//   - 前後の空白（"認証" と " 認証 "）
//
// 畳まない:
//
//   - 大文字小文字（"API" と "api"）
//   - 全角半角（"API" と "ＡＰＩ"）
//   - 途中の空白（"認証 方式" と "認証方式"）
//
// **改行まわりは、ここへ来る前に RuleTitleSingleLine が弾いている。**
// normalizeText は改行コードと行末の空白も畳むが、複数行の title は重複判定に
// 届かないので、その分はここでは効かない。
//
// 境目は「画面上に現れる違いかどうか」。畳むほうは現れないので、別の文字列
// として通しても「なぜか 2 つある」という同じ壊れ方をする。畳まないほうは
// 見分けが付くうえ GitHub 側でも別の文字列なので、畳むと壊れないものを弾く。
//
// 前の 3 つは normalizeText がやっている。**再利用しているのは、「見た目が同じ
// ものは同じ」という判断が ComputeContentHash に既にあるため。** 別の正規化を
// 2 つ持つと、どちらが正しい「同じ」なのかが決まらない。
//
// **同じことを interpretationSystemPrompt にも書いてある**（畳む側だけ）。
// 片方だけ変えると、指示していない制約で弾くことになる。
func normalizeTitle(s string) string {
	return strings.TrimSpace(normalizeText(s))
}

// validateParent は 1 項目の親参照を検べる。
//
// 親なしは誤りではない。epic は必ず親を持たず、issue も epic に属さず
// 単独で立つことがある。
func validateParent(it InterpretedItem, kindByLocalID map[string]ItemKind, field func(string) string) ValidationErrors {
	if it.ParentLocalID == nil {
		return nil
	}
	parent := *it.ParentLocalID

	switch {
	case it.Kind == KindEpic:
		return ValidationErrors{newValidationError(RuleEpicHasNoParent, field("parentLocalId"),
			"epic は親を持てません。parentLocalId を null にしてください")}

	case parent == it.LocalID:
		return ValidationErrors{newValidationError(RuleParentExists, field("parentLocalId"),
			"自分自身を親にはできません")}
	}

	parentKind, ok := kindByLocalID[parent]
	switch {
	case !ok:
		return ValidationErrors{newValidationError(RuleParentExists, field("parentLocalId"),
			fmt.Sprintf("parentLocalId %q に対応する localId がありません", parent))}

	case !parentKind.Valid():
		// 親の kind が不正なことは親自身の項目で報告済み。ここで重ねると、
		// 元を 1 箇所直せば消える指摘が修正指示に混ざる。
		return nil

	case parentKind != KindEpic:
		// 階層は epic ← issue の 1 本だけ（ADR 0006）。
		return ValidationErrors{newValidationError(RuleParentIsEpic, field("parentLocalId"),
			fmt.Sprintf("issue の親になれるのは epic だけです（%q は %s です）", parent, parentKind))}
	}

	return nil
}

// validateGranularity は開発者が指定した粒度との整合を検べる。
//
// 粒度はプロンプトでも指示するが、LLM が従う保証はない。構造で守る。
// GranularityAuto は指定なしなので制約を課さない。
func validateGranularity(items []InterpretedItem, g Granularity) ValidationErrors {
	var errs ValidationErrors

	switch g {
	case GranularityIssue:
		for i, it := range items {
			if it.Kind == KindEpic {
				errs = append(errs, newValidationError(RuleGranularityIssue,
					fmt.Sprintf("items[%d].kind", i),
					"粒度に issue が指定されているので epic は作れません"))
			}
		}

	case GranularityEpic:
		hasEpic := slices.ContainsFunc(items, func(it InterpretedItem) bool {
			return it.Kind == KindEpic
		})
		if !hasEpic {
			errs = append(errs, newValidationError(RuleGranularityEpic, "items",
				"粒度に epic が指定されているので、epic が少なくとも 1 件必要です"))
		}
	}

	return errs
}
