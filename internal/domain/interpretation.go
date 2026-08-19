package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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

// ValidationError は解釈結果の検証で見つかった問題 1 件。
type ValidationError struct {
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
				errs = append(errs, ValidationError{
					Field:   field,
					Message: "この囲みから作ったものはまだありません。previousRef は null にしてください",
				})
			case !known:
				errs = append(errs, ValidationError{
					Field: field,
					Message: fmt.Sprintf(
						"previousRef %q に対応するものがありません。前回作成したものの ID か null にしてください", ref),
				})
			case claimed:
				errs = append(errs, ValidationError{
					Field: field,
					Message: fmt.Sprintf(
						"previousRef %q を %q と取り合っています。1 つにつき 1 件までです", ref, owner),
				})
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
		errs = append(errs, ValidationError{
			Field:   "summary",
			Message: "この囲みをどう解釈したかの説明を入れてください",
		})
	}
	if len(in.Items) == 0 {
		errs = append(errs, ValidationError{
			Field:   "items",
			Message: "少なくとも 1 件の項目が必要です",
		})
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
				errs = append(errs, ValidationError{
					Field:   field("previousItemId"),
					Message: "previousItemId を空文字にはできません。新規なら null にしてください",
				})
			case dup:
				errs = append(errs, ValidationError{
					Field: field("previousItemId"),
					Message: fmt.Sprintf(
						"previousItemId %q を %q と取り合っています。1 つにつき 1 件までです",
						*it.PreviousItemID, owner),
				})
			default:
				claimed[*it.PreviousItemID] = it.LocalID
			}
		}

		if !it.Kind.Valid() {
			errs = append(errs, ValidationError{
				Field:   field("kind"),
				Message: fmt.Sprintf("kind は %q か %q のどちらかです（%q が指定されています）", KindEpic, KindIssue, it.Kind),
			})
		}

		title := normalizeTitle(it.Title)
		switch owner, dup := epicTitles[title]; {
		case title == "":
			errs = append(errs, ValidationError{
				Field:   field("title"),
				Message: "title を空にはできません",
			})

		case it.Kind != KindEpic:
			// issue のタイトルは重複してよい。親として指されないので、
			// 同名でも構造は壊れない。

		case dup:
			errs = append(errs, ValidationError{
				Field: field("title"),
				Message: fmt.Sprintf(
					"epic の title %q が localId %q のものと重複しています。子は親の"+
						"epic をタイトルで指すため、どちらの配下なのか区別が付かなく"+
						"なります。どちらかを別のタイトルにしてください", it.Title, owner),
			})

		default:
			epicTitles[title] = it.LocalID
		}

		if it.LocalID == "" {
			errs = append(errs, ValidationError{
				Field:   field("localId"),
				Message: "localId を空にはできません",
			})
		} else {
			if _, dup := seen[it.LocalID]; dup {
				errs = append(errs, ValidationError{
					Field:   field("localId"),
					Message: fmt.Sprintf("localId %q が重複しています。項目ごとに別の値にしてください", it.LocalID),
				})
			}
			seen[it.LocalID] = struct{}{}
		}

		errs = append(errs, validateParent(it, kindByLocalID, field)...)
	}

	return errs
}

// normalizeTitle は epic のタイトルの重複判定に使う正規形を返す。
//
// **基準は「人に見分けが付くかどうか」であって、GitHub 上で同じ文字列かどうか
// ではない。** 見分けが付かない違いは、GitHub 上では別の文字列になっても
// 「なぜか 2 つある」という同じ壊れ方をする。だから衝突として扱う。
//
// 畳むのは normalizeText がやるぶん（改行コードの違い、行末の空白とタブ、
// Unicode の合成済みと結合文字）に、前後の空白を足したもの。**どれも画面上に
// 現れない違いである。** normalizeText を再利用しているのは、その判断が
// ComputeContentHash に既にあるため。別の正規化を 2 つ持つと、どちらが正しい
// 「同じ」なのかが決まらない。
//
// **大文字小文字と全角半角は畳まない。** どちらも見分けが付くうえ GitHub 側でも
// 別の文字列なので、畳むと壊れないものを弾くことになる。NFKC まで掛けないのは
// このため。
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
		return ValidationErrors{{
			Field:   field("parentLocalId"),
			Message: "epic は親を持てません。parentLocalId を null にしてください",
		}}

	case parent == it.LocalID:
		return ValidationErrors{{
			Field:   field("parentLocalId"),
			Message: "自分自身を親にはできません",
		}}
	}

	parentKind, ok := kindByLocalID[parent]
	switch {
	case !ok:
		return ValidationErrors{{
			Field:   field("parentLocalId"),
			Message: fmt.Sprintf("parentLocalId %q に対応する localId がありません", parent),
		}}

	case !parentKind.Valid():
		// 親の kind が不正なことは親自身の項目で報告済み。ここで重ねると、
		// 元を 1 箇所直せば消える指摘が修正指示に混ざる。
		return nil

	case parentKind != KindEpic:
		// 階層は epic ← issue の 1 本だけ（ADR 0006）。
		return ValidationErrors{{
			Field:   field("parentLocalId"),
			Message: fmt.Sprintf("issue の親になれるのは epic だけです（%q は %s です）", parent, parentKind),
		}}
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
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("items[%d].kind", i),
					Message: "粒度に issue が指定されているので epic は作れません",
				})
			}
		}

	case GranularityEpic:
		hasEpic := slices.ContainsFunc(items, func(it InterpretedItem) bool {
			return it.Kind == KindEpic
		})
		if !hasEpic {
			errs = append(errs, ValidationError{
				Field:   "items",
				Message: "粒度に epic が指定されているので、epic が少なくとも 1 件必要です",
			})
		}
	}

	return errs
}
