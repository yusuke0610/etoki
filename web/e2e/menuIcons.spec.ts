import { expect, test, type Locator } from "@playwright/test";

import { openBoardMenu, openBoardWithMock } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

/**
 * キャンバスのメニューの etoki の項目のアイコン（#204）。
 *
 * Excalidraw の既定の項目にはアイコンがあり、etoki の項目に無いと字下げが揃わず
 * 2 種類の部品が混ざって見える。**見るのは絵ではなく、付いていることと、名前に
 * 混ざっていないこと。** 絵の良し悪しはスクリーンショットで見る。
 */

/** 項目のボタン。**名前は完全一致で引く。** アイコンが名前に混ざると引けなくなる。 */
function item(menu: Locator, name: string): Locator {
  return menu.getByRole("button", { name, exact: true });
}

/** 項目の名前の頭の位置。アイコンの右から始まる。 */
async function textLeft(target: Locator): Promise<number> {
  const box = await target.boundingBox();
  if (box === null) throw new Error("見えていない要素の位置は測れない");
  return box.x;
}

test.describe("メニューのアイコン", () => {
  test("作成先が未確定のボードで、etoki の項目にはどれもアイコンが付く", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());
    const menu = await openBoardMenu(page);

    for (const name of [
      "ボード一覧へ戻る",
      "名前を変更",
      "作成先を変更",
      "書き出し",
      "取り込み",
      "表",
      "ボードを削除",
    ]) {
      const icon = item(menu, name).locator(".dropdown-menu-item__icon svg");
      await expect(icon, name).toHaveCount(1);
      // 飾り。読み上げは文字の名前だけを読む。
      await expect(icon, name).toHaveAttribute("aria-hidden", "true");
    }
  });

  // 作成先が確定すると「作成先を変更」の代わりに「取り直す」が出る。
  test("作成先が確定したボードで、取り直しにもアイコンが付く", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    await openBoardWithMock(page, mock);
    const menu = await openBoardMenu(page);

    await expect(
      item(menu, "作成先の名前を取り直す").locator(".dropdown-menu-item__icon svg"),
    ).toHaveCount(1);
  });

  // 取り消せない操作は、キャンバスの中身を消す操作と同じ絵にしない。
  // 同じ絵だと、ボードごと消すのか中身だけ消すのかを絵で見分けられない。
  test("ボードを削除のアイコンは危険の色で、キャンバスのリセットと違う絵", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());
    const menu = await openBoardMenu(page);

    const remove = item(menu, "ボードを削除");
    const removeIcon = remove.locator(".dropdown-menu-item__icon svg");
    const resetIcon = item(menu, "キャンバスのリセット").locator(
      ".dropdown-menu-item__icon svg",
    );
    expect(await removeIcon.innerHTML()).not.toBe(await resetIcon.innerHTML());

    // 線は文字の色を継ぐ。文字が危険の色なら線も同じ色になり、ふつうの項目とは
    // 違う色になる。
    const removeStroke = await removeIcon.evaluate((el) => getComputedStyle(el).stroke);
    const removeText = await remove.evaluate((el) => getComputedStyle(el).color);
    const plainStroke = await item(menu, "書き出し")
      .locator(".dropdown-menu-item__icon svg")
      .evaluate((el) => getComputedStyle(el).stroke);
    expect(removeStroke).toBe(removeText);
    expect(removeStroke).not.toBe(plainStroke);
  });

  // 押せない理由の文（ADR 0066）は、項目の縁ではなく名前の頭に揃える。
  // 縁に揃えると、理由だけがアイコンの列にはみ出す。字下げは Excalidraw の
  // 項目の寸法を写しているので、ライブラリを上げて寸法が変わるとここが落ちる。
  test("押せない理由の文の頭が、項目の名前の頭と揃う", async ({ page }) => {
    const mock = baseMock();
    // オーナーでなければ「作成先を変更」の代わりに理由の文が出る。
    mock.details[BOARD_ID] = { ...board(), role: "editor" };
    await openBoardWithMock(page, mock);
    const menu = await openBoardMenu(page);

    const note = menu.getByText("作成先を変えられるのはオーナーだけです");
    const name = item(menu, "書き出し").locator(".dropdown-menu-item__text");
    expect(Math.abs((await textLeft(note)) - (await textLeft(name)))).toBeLessThan(1);
  });
});
