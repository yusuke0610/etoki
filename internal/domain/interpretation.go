package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
