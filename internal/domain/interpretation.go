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
func ParseInterpretation(raw []byte) (Interpretation, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	var in Interpretation
	if err := dec.Decode(&in); err != nil {
		return Interpretation{}, fmt.Errorf("parse interpretation: %w", err)
	}
	if dec.More() {
		return Interpretation{}, errors.New("parse interpretation: JSON の後ろに余分な出力があります")
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

	for i, it := range items {
		field := func(name string) string { return fmt.Sprintf("items[%d].%s", i, name) }

		if !it.Kind.Valid() {
			errs = append(errs, ValidationError{
				Field:   field("kind"),
				Message: fmt.Sprintf("kind は %q か %q のどちらかです（%q が指定されています）", KindEpic, KindIssue, it.Kind),
			})
		}

		if strings.TrimSpace(it.Title) == "" {
			errs = append(errs, ValidationError{
				Field:   field("title"),
				Message: "title を空にはできません",
			})
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
