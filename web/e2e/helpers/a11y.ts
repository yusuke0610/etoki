import AxeBuilder from "@axe-core/playwright";
import { expect, type Locator, type Page } from "@playwright/test";

/**
 * アクセシビリティを守るための道具（ADR 0039）。
 *
 * **2 つ入っている。混ぜない。**
 *
 * - `expectBlockedReason` は etoki 固有の判断（押せない理由を本文として出す）を
 *   見る。既製のルールでは検知できないので、こちらを手で書く。
 * - `expectNoAxeViolations` は形の規約のうち、静的解析では見えないもの
 *   （実際に描いた色のコントラスト、指す先の実在）を見る。
 */

/**
 * 押せないボタンの理由が、本文として読めることを確かめる。
 *
 * **`disabled` なボタンはフォーカスも当たらない。** 理由を `title` に置くと、
 * ホバーできない利用者と読み上げには届かない。etoki は理由を本文として出し、
 * ボタンから `aria-describedby` で指す形に揃えてある
 * （`BoardPage` / `AnnotationPanel` に理由つきのコメントで残っている）。
 *
 * **回帰止め。切れると何が起きるか。** 理由を `title` に移す、`aria-describedby`
 * を落とす、指す先の `id` を注釈ごとに分け忘れる、理由の要素を出さないまま
 * ボタンだけ止める。どれも画面は「押せないボタンがある」ままなので、
 * **見た目では壊れたことが分からない。**
 */
export async function expectBlockedReason(
  button: Locator,
  reason: string | RegExp,
): Promise<void> {
  await expect(button).toBeDisabled();

  const described = await button.getAttribute("aria-describedby");
  expect(described, "押せないボタンが理由を指していない").not.toBeNull();

  const page = button.page();
  const ids = (described ?? "").split(/\s+/).filter((id) => id !== "");
  expect(ids.length, "aria-describedby が空").toBeGreaterThan(0);

  let text = "";
  for (const id of ids) {
    // 属性セレクタで引く。id には注釈の element ID が入るので、CSS の
    // 識別子として安全とは限らない。
    const target = page.locator(`[id="${id}"]`);
    // **1 つに定まること。** 一覧に複数の注釈が並ぶので、id を注釈ごとに
    // 分け忘れると 2 つ以上になり、読み上げはどれを読むか決められない。
    await expect(
      target,
      `aria-describedby の指す先 #${id} が 1 つに定まらない`,
    ).toHaveCount(1);
    // 出ていなければ理由は届かない。押せないボタンには焦点も当たらないので、
    // 「フォーカスすれば読める」では出口にならない。
    await expect(target, `#${id} が画面に出ていない`).toBeVisible();
    text += `${await target.innerText()}\n`;
  }

  if (typeof reason === "string") expect(text).toContain(reason);
  else expect(text).toMatch(reason);
}

/**
 * etoki が書いた DOM に axe を掛け、違反が無いことを確かめる。
 *
 * **`.excalidraw` は外す。** 掛けた時点で Excalidraw 自身のメニューボタンが
 * `button-name` に引っかかっており、etoki には直せない。外さないと、直せない
 * 指摘が常時 1 件出続ける検査になり、やがて誰も見なくなる（ADR 0039）。
 *
 * jsx-a11y と重ならない。あちらは JSX の属性しか見ないので、**実際に描いた
 * 色のコントラストと、`aria-describedby` の指す先が実在するかは見えない。**
 */
export async function expectNoAxeViolations(page: Page): Promise<void> {
  const results = await new AxeBuilder({ page }).exclude(".excalidraw").analyze();

  // id と対象の要素まで出す。件数だけでは、落ちたときにどこを直すのか分からない。
  const found = results.violations.map(
    (v) => `${v.id} (${v.impact}): ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`,
  );

  expect(found).toEqual([]);
}
