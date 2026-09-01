package usecase

import (
	"fmt"
	"strings"

	"github.com/yusuke0610/etoki/internal/domain"
)

// diagramNotation は図の種類 1 つを、どの mermaid 記法で書かせるかに結ぶ。
//
// **検査と指示を 1 つの表に結ぶ**（ADR 0029 と同じ形）。指示だけ変えて headers を
// 直し忘れると、頼んだとおりに書いた出力を弾くことになる。
type diagramNotation struct {
	// name は指摘に出す記法の呼び名。
	name string
	// headers は出力の 1 行目に来ることを確かめる語。
	//
	// 複数あるのは mermaid が同じ図に別名を持つため（flowchart と graph）。
	headers []string
	// instruction はその記法で書かせる指示。**読み方の規則は書かない。**
	// 何の図かを言うところまでで、図をどう解釈するかは etoki の仕事ではない。
	instruction string
}

// diagramNotations は種類ごとの記法。
//
// # なぜ mermaid の mindmap と architecture-beta を使わないか
//
// **記法を選ぶ基準は「mermaid で書けるか」ではなく「Excalidraw の要素として
// 置けるか」。** 生成したドラフトは人が手で直すためのもので、直せない絵を
// 置くくらいなら置かない（ADR 0040）。変換器は mindmap と architecture-beta を
// 図形に分解せず、SVG を畳んだ画像 1 枚にして返すので、そのまま頼むと
// **必ず置けないものが返る。**
//
// どちらも flowchart で書ける。マインドマップは根から枝を伸ばす木、システム
// 構成図は subgraph で境界を作った図になる。**開発者が選ぶ語彙（5 種）は
// 変えずに、その中でどう書かせるかだけを変えている。**
var diagramNotations = map[domain.DiagramKind]diagramNotation{
	domain.DiagramKindTodo: {
		name:    "flowchart",
		headers: []string{"flowchart", "graph"},
		instruction: `やることの洗い出しです。flowchart で書いてください。

- 1 行目は "flowchart TD" にしてください。
- 作業を 1 つずつノードにしてください。
- 先にやる必要があるものから矢印を引いてください。順序が決まっていない
  ものは矢印で結ばないでください。`,
	},

	domain.DiagramKindMindmap: {
		name:    "flowchart",
		headers: []string{"flowchart", "graph"},
		instruction: `発想を広げるマインドマップです。**mindmap 記法は使わず、
flowchart で木として書いてください。**

- 1 行目は "flowchart LR" にしてください。
- 中心になる主題をノード 1 つにして、そこから枝を伸ばしてください。
- 枝は 2 段か 3 段までにしてください。それより深いものは、別の主題として
  分けたほうが読めます。`,
	},

	domain.DiagramKindSequence: {
		name:    "sequenceDiagram",
		headers: []string{"sequenceDiagram"},
		instruction: `登場人物とやりとりの順序です。sequenceDiagram で書いてください。

- 1 行目は "sequenceDiagram" にしてください。
- 登場人物は participant として先に並べてください。
- やりとりは起きる順に並べ、矢印には何をするのかを書いてください。`,
	},

	domain.DiagramKindER: {
		name:    "erDiagram",
		headers: []string{"erDiagram"},
		instruction: `実体と関連です。erDiagram で書いてください。

- 1 行目は "erDiagram" にしてください。
- 実体どうしの関連には、多重度と関連の名前を書いてください。
- 属性は、その実体が何であるかを決めるものだけにしてください。列を
  すべて挙げる場ではありません。`,
	},

	domain.DiagramKindArchitecture: {
		name:    "flowchart",
		headers: []string{"flowchart", "graph"},
		instruction: `構成要素と境界です。**architecture-beta 記法は使わず、
flowchart で書いてください。**

- 1 行目は "flowchart TD" にしてください。
- 構成要素を 1 つずつノードにしてください。
- 境界（サービス、ネットワーク、チームの持ち場）は subgraph で囲んで
  ください。
- 矢印は呼び出しやデータの向きに引き、何が流れるのかを書いてください。`,
	},
}

// diagramSystemPrompt は生成の役割と出力形式を伝えるシステム指示。
//
// **bot は絵の話しかしない**（#58）。epic / issue を出させないのは、出せると
// 分かった時点で「じゃあ issue にします」が会話の流れでできてしまい、
// 構造化が開発者の明示的なトリガーから外れるため（中核思想 3）。
const diagramSystemPrompt = `あなたはホワイトボードに図の下書きを描く担当です。

開発者が書いた一文から、図の骨格を mermaid で描いてください。**完成品では
なく叩き台です。** このあと開発者が手で直すので、抜けや粗さは構いません。
迷ったら細かく描き込むより、少ない要素で読める形にしてください。

出力は mermaid のコードだけにしてください。前置き・説明文・コードフェンスを
付けないでください。

守ること:

- 図に出す言葉は、開発者が使った言葉に寄せてください。言い換えると、
  直すときにどこが自分の言葉だったのか分からなくなります。
- **epic や issue といった開発タスクの分類を出さないでください。** ここで
  描くのは図であって、作業の分解ではありません。
- 開発者に質問を返さないでください。足りないところは、ありそうなもので
  埋めて構いません。直すのは開発者の仕事です。
`

// buildDiagramMessage は会話をまるごと 1 通の自己完結したメッセージにする。
//
// port.LLMClient は 1 往復ぶんしか運べない（ADR 0005）。会話履歴の代わりに、
// 何をどう直してきたのかを毎回書き下ろす。**port を拡張しないのは、自前実装を
// 差し込む外部リポジトリの負担を増やさないため**（ADR 0001 / 0008）。
func buildDiagramMessage(req DiagramRequest) string {
	var b strings.Builder

	b.WriteString(diagramNotations[req.Kind].instruction)
	b.WriteString("\n\n")

	if len(req.History) > 0 {
		b.WriteString("ここまでのやりとり:\n\n")
		for i, t := range req.History {
			fmt.Fprintf(&b, "%d. 指示: %s\n", i+1, oneLine(t.Prompt))
			b.WriteString("   返した図:\n")
			for _, line := range strings.Split(strings.TrimSpace(t.Mermaid), "\n") {
				b.WriteString("   ")
				b.WriteString(line)
				b.WriteString("\n")
			}
			b.WriteString("\n")
		}
		// **直前の図を捨てさせない。** 会話を積む意味は「前の図を直す」こと
		// なので、毎回描き直されると指示が積み上がらない。
		b.WriteString("直前の図を土台にして、次の指示を反映してください。" +
			"指示に関係しないところは変えないでください。\n\n")
		b.WriteString("次の指示:\n")
	} else {
		b.WriteString("指示:\n")
	}

	b.WriteString(strings.TrimSpace(req.Prompt))
	b.WriteString("\n")

	return b.String()
}
