package domain

// DiagramKind は図の種類。
//
// **語彙はここ 1 つ。** ブレストの出発点になるテンプレート（#52）と、
// プロンプトから生成するドラフト（#58）は同じ 5 種を指す。2 箇所に持つと、
// 片方に種類を足したときにもう片方が知らないまま残る。
//
// **種類は開発者が明示的に選んだメタデータであって、絵から推測したものでは
// ない**ので、LLM に渡しても中核思想 2 に反しない。反するのは、種類を使って
// etoki のコードが図を読み始めたとき（「シーケンス図だからこの矢印は呼び出し
// だ」）。**この型から図の読み方を導く実装を書かないこと。**
type DiagramKind string

// DiagramKind の取りうる値。
//
// ゼロ値が DiagramKindUnspecified になるよう空文字にしている。注釈に種類を
// 付けるかは任意で、「指定なし」が既定の状態であるため（Granularity と同じ）。
// **指定を要求するかどうかは使う側が決める。** プロンプトからの生成は
// 「何の図を描くか」が入力そのものなので、指定なしを受け付けない。
const (
	// DiagramKindUnspecified は種類を指定していないことを表す。
	DiagramKindUnspecified DiagramKind = ""
	// DiagramKindTodo はやることの洗い出し。
	DiagramKindTodo DiagramKind = "todo"
	// DiagramKindMindmap は発想の広げ方。
	DiagramKindMindmap DiagramKind = "mindmap"
	// DiagramKindSequence は登場人物とやりとりの順序。
	DiagramKindSequence DiagramKind = "sequence"
	// DiagramKindER は実体と関連。
	DiagramKindER DiagramKind = "er"
	// DiagramKindArchitecture は構成要素と境界。
	DiagramKindArchitecture DiagramKind = "architecture"
)

// DiagramKinds は指定なしを除いた図の種類を、表示したい順に返す。
//
// 選択肢を並べる側と、種類ごとの指示を持つ側の両方がここを引く。**それぞれが
// 自前で並べないこと。** 種類を足したときに、片方だけ知らないまま残る。
func DiagramKinds() []DiagramKind {
	return []DiagramKind{
		DiagramKindTodo,
		DiagramKindMindmap,
		DiagramKindSequence,
		DiagramKindER,
		DiagramKindArchitecture,
	}
}

// Valid は k が定義済みの種類かどうかを返す。指定なしも含む。
func (k DiagramKind) Valid() bool {
	if k == DiagramKindUnspecified {
		return true
	}
	for _, known := range DiagramKinds() {
		if k == known {
			return true
		}
	}
	return false
}
