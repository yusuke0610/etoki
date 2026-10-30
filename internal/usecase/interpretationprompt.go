package usecase

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yusuke0610/etoki/internal/domain"
)

// buildUserMessage は注釈範囲のテキストと粒度指定を 1 通のメッセージにする。
//
// hasImage は画像を添えたかどうか。画像の話は添えたときにだけ書く。常に書くと、
// 画像なしの解釈でモデルが「見えるはずの図」を前提に埋め合わせを始める。
//
// 並びは、囲みのテキスト → 画像 → 囲みの外の材料（背景・別の絵、ADR 0093）→
// この囲みから前回までに作ったもの → 種別と粒度。**囲みのテキストを先頭に
// 置き、空行で区切る。** 偽の LLM（internal/fakeupstream）はここで切って
// 読んでいる。
func buildUserMessage(
	a domain.Annotation, texts []domain.TextElement, hasImage bool, previous []domain.PreviousItem,
	ic InterpretationContext,
) string {
	var b strings.Builder

	b.WriteString("囲みに含まれるテキスト:\n")
	if len(texts) == 0 {
		// テキストが無くても解釈は試みる。図形と矢印だけの囲みがありうるため
		// で、その中身は画像でしか渡せない。
		//
		// 「無い」ことは明示する。黙って空にすると、モデルが入力の欠落を
		// 疑って作り話を始める。
		b.WriteString("(なし)\n")
	}
	for _, t := range texts {
		// 囲みの名前は AnnotationTexts が先頭に含めているので、ここでは
		// 別立てにしない。
		b.WriteString("- ")
		b.WriteString(strings.ReplaceAll(t.Text, "\n", " "))
		b.WriteString("\n")
	}

	if hasImage {
		b.WriteString("\n")
		b.WriteString(imageInstruction)
	}

	b.WriteString(premiseSection(ic.Premise))
	b.WriteString(otherItemsSection(ic.OtherItems, ic.OmittedOtherItems))
	b.WriteString(previousInstruction(previous))

	b.WriteString("\n")
	// **種別を先に言う。** 何の図として読むかが決まってから粒度の話になる。
	// 逆にすると、粒度の指定が「何の図か」を上書きする指示に見える。
	if instruction := kindInstruction(a.Kind); instruction != "" {
		b.WriteString(instruction)
		b.WriteString("\n")
	}
	b.WriteString(granularityInstruction(a.Granularity))
	b.WriteString("\n")

	return b.String()
}

// previousInstruction は前回までに作ったものを ref つきで並べる。
//
// **node ID は見せない。** `PVTI_lADOA...` を復唱させると取り違えるので、`p1` の
// ような短い ref にして、解決は etoki 側で行う（ADR 0026）。
//
// 1 件も無ければ節ごと省く。空の一覧を見せると、モデルが「あるはずのもの」を
// 埋め合わせ始める。
func previousInstruction(previous []domain.PreviousItem) string {
	if len(previous) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n前回までにこの囲みから作ったもの:\n")

	for _, p := range previous {
		fmt.Fprintf(&b, "- %s (%s) %s\n", p.Ref, p.Kind, oneLine(p.Title))
		if body := strings.TrimSpace(p.Body); body != "" {
			fmt.Fprintf(&b, "    本文: %s\n", oneLine(body))
		}
	}

	b.WriteString(previousMatchInstruction)

	return b.String()
}

// premiseQuote は背景の各行の頭に付ける印。
//
// **背景は人が書いた複数行の文で、何でも書ける。** 行頭に印を付けて、etoki が
// 組み立てた見出しや一覧（「- p1 (epic) …」）と取り違えられない形にする。
// 背景の中の空行も印だけの行になるので、メッセージの節の区切り（空行）が
// 背景の中に現れない。
const premiseQuote = "> "

// premiseSection はボードの背景を、囲みの外の材料として並べる（ADR 0093）。
//
// 空なら節ごと省く。空の節を見せると、モデルが「あるはずのもの」を埋め合わせ
// 始める（前回ぶんと同じ）。
//
// **改行は潰さない。** 付箋や前回ぶんと違い、背景は書いた人が行で区切って
// いる。潰すと箇条書きがつながって読めなくなる。
func premiseSection(premise string) string {
	if premise == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("\nこのボードの背景（開発者が書いたもの。囲みの外の材料です）:\n")
	for line := range strings.SplitSeq(premise, "\n") {
		b.WriteString(strings.TrimRight(premiseQuote+line, " "))
		b.WriteString("\n")
	}
	b.WriteString(premiseInstruction)

	return b.String()
}

// premiseInstruction は背景の使い方を伝える。
//
// **読み方の規則は書かない**（中核思想 2）。背景に何を書くかは人に任せて
// いて、そこに読み方が書いてあれば決めているのは人。etoki が足すのは、背景を
// 項目の出どころにしないことと、着手に要る部分を body に書くこと（出口の側、
// ADR 0093）だけ。
//
// **背景を body に丸写しさせない。** 全部の項目に同じ文が載り、前回ぶんとして
// 次の解釈にも戻ってくる（ADR 0026）。
const premiseInstruction = `
上の背景は、この囲みを読むための前提です。

- 用語や呼び方は背景に合わせてください。
- 背景に書かれていることから項目を作らないでください。項目にするのは囲みの中身だけです。
- 各項目の body には、背景のうちその項目に着手するのに要ること（使う技術や守る制約など）を
  書いてください。背景をまるごと写さず、その項目に関係する部分だけにしてください。
`

// otherItemsSection は同じボードの別の絵から作ったものを並べる（ADR 0093）。
//
// **ref を振らない。** 振ると対応づけの候補に見える。更新先はその囲みの
// 畳み込みだけ（ADR 0026）。
//
// 1 件も無ければ節ごと省く。打ち切ったときは、渡していないものがあることを
// 書く。書かないと、モデルは一覧が全部だと読む。
func otherItemsSection(items []OtherItem, omitted int) string {
	if len(items) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n同じボードの別の絵からすでに作ったもの（参考。対応づけの対象ではありません）:\n")
	for _, it := range items {
		fmt.Fprintf(&b, "- (%s) %s\n", it.Kind, oneLine(it.Title))
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "（ほかに %d 件ありますが、ここには載せていません）\n", omitted)
	}
	b.WriteString(otherItemsInstruction)

	return b.String()
}

// otherItemsInstruction は別の絵のぶんの使い方を伝える。
//
// **重なっても項目を落とさせない。** 落とすかどうかを LLM に決めさせると、
// 開発者が選ぶ前に候補が消える（中核思想 3）。外すのは作成前の選択（ADR 0024）。
//
// **body に関連として挙げさせない。** draft issue は番号を持たずリンクできない
// ので、タイトルの羅列になる（ADR 0093）。
const otherItemsInstruction = `
上の一覧は、用語をそろえ、重なりに気づくための参考です。

- 今回の項目と重なると思っても、項目を外さずに出力し、どれと重なりそうかを summary に
  書いてください。外すかどうかは開発者が決めます。
- 一覧の項目を、関連する項目として body に挙げないでください。
`

// oneLine は一覧に載せるために改行を潰す。
//
// 箇条書きの途中で改行されると、次の行が別の項目に見える。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// previousMatchInstruction は対応づけの出し方を伝える。
//
// **previousRef の制約は書かない。** null にする場合と 1 つを 2 つに使えないことは
// domain.Rules の RulePreviousRef が制約一覧として言う（ADR 0029）。前回ぶんの
// 直後という位置は目に付くが、そこに写すと検査を足したときに片方だけが古くなる。
// ここが持つのは、検査では決まらない「どれとどれが同じものか」の判断だけ。
const previousMatchInstruction = `
上の一覧と今回の項目が同じものを指しているなら、その項目の previousRef に
一覧の ID（p1 など）を入れてください。

- タイトルが変わっていても、同じことを指しているなら対応づけてください。
  文言の見直しはよくある変更です。
- 迷ったら null にしてください。**新しく作るほうが取り返しがつきます。**
  対応づけを間違えると、別のものを書き換えてしまいます。
- 今回の出力に対応するものが無い項目が一覧に残っていても構いません。
`

// imageInstruction は添えた画像とテキスト一覧の役割分担を伝える。
//
// 分担を書かないとモデルは画像の文字を読み直す。画像は frame の範囲を写した
// ものなので囲みの外は入らないが、読み取りの誤りは入る。文言の正はテキスト
// 一覧に置き、画像はテキストに現れない関係のためだけに使わせる。
const imageInstruction = `この囲みを写した画像を添えています。矢印の向き、囲みの入れ子、
配置の近さといった、上のテキスト一覧には現れない関係を読み取るために使ってください。
文言は上のテキスト一覧が正です。画像から文字を読み取り直さないでください。
`

// granularityInstruction は開発者の粒度指定を指示文にする。
func granularityInstruction(g domain.Granularity) string {
	switch g {
	// **どう振る舞うかは制約一覧（domain.Rules）が言う。** ここで繰り返すと、
	// 粒度の規則だけが 2 箇所になる。
	case domain.GranularityEpic:
		return "開発者はこの範囲を epic 相当と指定しています。"
	case domain.GranularityIssue:
		return "開発者はこの範囲を issue 相当と指定しています。"
	default:
		return "粒度の指定はありません。内容に応じて epic と issue を使い分けてください。"
	}
}

// diagramReadings は図の種別 1 つを、解釈のときに何の図として読ませるかに結ぶ。
//
// **書くのは「何の図として読むか」までで、読み方の規則は書かない**（中核思想 2）。
// 「矢印の向きは呼び出し方向」「線は 1 対多」といった規則をここに書くと、
// 座標や図形から構造を決める仕事を etoki が引き受けたことになる。それを
// プロンプトに書いてもコードに書いても、決めているのは etoki である点は
// 変わらない。読むのは LLM。
//
// **粒度の指定と衝突する書き方をしない。** 「todo だから全部 issue にする」
// と書くと、開発者が選んだ粒度（epic / issue / 指定なし）とどちらが効くのか
// 分からなくなる。ここが言うのは図の性質までで、何を作るかは
// granularityInstruction と domain.Rules が言う。
//
// **生成の指示（diagramNotations）とは別に持つ。** あちらは「どう描かせるか」、
// こちらは「どう読ませるか」で、答えている問いが違う。まとめると、mermaid の
// 記法の話が解釈のプロンプトに混ざる。語彙（domain.DiagramKind）だけを共有する。
var diagramReadings = map[domain.DiagramKind]string{
	domain.DiagramKindTodo: "この囲みはやることの洗い出しとして描かれています。",

	domain.DiagramKindMindmap: "この囲みはマインドマップとして描かれています。",

	domain.DiagramKindSequence: "この囲みはシーケンス図として描かれています。",

	domain.DiagramKindER: "この囲みは ER 図として描かれています。",

	domain.DiagramKindArchitecture: "この囲みはシステム構成図として描かれています。",
}

// kindInstruction は開発者が選んだ種別を指示文にする。
//
// 指定が無ければ何も足さない。**「種別は指定されていません」と書かない。**
// 書くと、モデルが図の種類を当てにいく余地を作る。テキストと画像だけを見て
// 読ませるのが指定なしの状態で、それはこの機能が入る前と同じ入力である。
func kindInstruction(k domain.DiagramKind) string {
	return diagramReadings[k]
}

// buildRetryMessage は直前の出力と指摘を添えた再送メッセージを組み立てる。
func buildRetryMessage(base, previous string, err error) string {
	var b strings.Builder

	b.WriteString(base)
	b.WriteString("\n---\n\n")
	b.WriteString("直前の出力は次の点で誤っています。指摘をすべて直したうえで、")
	b.WriteString("JSON 全体をもう一度出力してください。\n\n")
	b.WriteString("直前の出力:\n")
	b.WriteString(previous)
	b.WriteString("\n\n指摘:\n")

	for _, line := range correctionLines(err) {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// correctionLines は検証エラーを 1 件 1 行の修正指示にする。
//
// domain.ValidationErrors がスライスなのは、ここで 1 件ずつ取り出すため。
// 全件を並べれば LLM は 1 往復ですべて直せる。
func correctionLines(err error) []string {
	var verrs domain.ValidationErrors
	if errors.As(err, &verrs) {
		lines := make([]string, len(verrs))
		for i, e := range verrs {
			lines[i] = e.Error()
		}
		return lines
	}

	// パースに失敗した場合は 1 件だけ。JSON として読めなかった旨を渡す。
	return []string{err.Error()}
}

// interpretationSystemPrompt は解釈の役割と出力形式を伝えるシステム指示。
//
// **制約一覧は domain.Rules から組み立てる。** 手で書くと、検査を足したときに
// 片方だけ古くなり、指示していない制約で弾くことになる（ADR 0028）。ここが持つ
// のは役割・出力形式・再送の枠組みだけ。
//
// **出力例は粒度ごとに変える**（issue #251）。issue 指定なのに例が epic を
// 含むと、小型モデルは例をなぞって epic を返し、再送でも直らない。例が言うのは
// 形までで、何を作るかは granularityInstruction と domain.Rules が言う。
func interpretationSystemPrompt(g domain.Granularity) string {
	return fmt.Sprintf(interpretationSystemPromptTemplate,
		interpretationExampleItems(g), domain.InterpretationConstraints())
}

// interpretationExampleItems は出力例の items の中身を、粒度で通る形にする。
//
// issue だけの例は previousRef を null にする。前回の一覧を渡していないときは
// 必ず null という制約があるのに、唯一の例が p1 だと、存在しない ref を書き写して
// 弾かれる。
func interpretationExampleItems(g domain.Granularity) string {
	if g == domain.GranularityIssue {
		return `    {"localId": "i1", "kind": "issue", "title": "...", "body": "...", "parentLocalId": null, "previousRef": null}`
	}
	return `    {"localId": "e1", "kind": "epic",  "title": "...", "body": "...", "parentLocalId": null,  "previousRef": null},
    {"localId": "i1", "kind": "issue", "title": "...", "body": "...", "parentLocalId": "e1", "previousRef": "p1"}`
}

// interpretationSystemPromptTemplate の 1 つ目の %s に出力例の items、2 つ目に
// 制約一覧が入る。
const interpretationSystemPromptTemplate = `あなたはホワイトボード上のブレスト内容を読み、開発タスクへ整理する担当です。

与えられるのは、ホワイトボード上で開発者が囲んだ 1 つの範囲に含まれるテキストです。
ブレスト中に書かれたものなので、粒度も表現もばらばらです。これを GitHub の
draft issue に落とせる形へ整理してください。

出力は次の形の JSON だけにしてください。前置き・説明文・コードフェンスを付けないでください。

{
  "summary": "この範囲をどう解釈したかの説明",
  "items": [
%s
  ]
}

制約:

%s- 上に挙げたフィールド以外を出力しないでください。
`
