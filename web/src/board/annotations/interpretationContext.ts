import type { InterpretationContext } from "../../api/types";

/**
 * 解釈に、囲みの外から添えたもの（ADR 0093）の見出し。畳んだ枠の `summary` に出す。
 *
 * **何を添えたかは、開かなくても読めるようにする。** 黙って材料を足して結果だけを
 * 見せると、出力がなぜそうなったかを読めない（中核思想 3）。中身は長くなりうる
 * （背景も別の絵の一覧も、上限はサーバーだけが持つ）ので畳むが、見出しだけで
 * 「何を何件」が分かる形にする。
 *
 * **何も添えていなければ null。** 見出しを出さない。最初の絵解きで背景も書いて
 * いないボードでは、どの結果にも同じ「ありません」が並ぶだけになる。
 */
export function contextSummary(context: InterpretationContext): string | null {
  const parts: string[] = [];
  if (context.premise !== "") parts.push("ボードの背景");

  const shown = context.otherItems.length;
  const omitted = context.omittedOtherItems;
  if (shown > 0 || omitted > 0) {
    // **打ち切った件数を黙らない**（ADR 0054 と同じ形）。上限の値は出さない
    // （サーバーだけが持つ）。
    parts.push(
      omitted > 0
        ? `別の絵から作ったもの ${shown} 件（上限を超えた ${omitted} 件は添えていません）`
        : `別の絵から作ったもの ${shown} 件`,
    );
  }

  if (parts.length === 0) return null;
  return `絵の外から添えたもの：${parts.join("・")}`;
}
