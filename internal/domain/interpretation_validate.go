package domain

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

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
	if len(in.Items) > MaxItems {
		errs = append(errs, newValidationError(RuleItemsCount, "items",
			fmt.Sprintf("項目は %d 件までです（%d 件ありました）", MaxItems, len(in.Items))))
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
				"title が Unicode コードポイントで %d 個あります。GitHub 側の上限は %d 個です。"+
					"入りきらない内容は body に移してください", n, MaxTitleRunes)))
		}
		if n := utf8.RuneCountInString(it.Body); n > MaxBodyRunes {
			errs = append(errs, newValidationError(RuleBodyLength, field("body"), fmt.Sprintf(
				"body が Unicode コードポイントで %d 個あります。GitHub 側の上限は %d 個です", n, MaxBodyRunes)))
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
