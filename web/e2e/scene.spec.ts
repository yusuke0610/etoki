import { expect, test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

/** キャンバスに矩形を 1 つ描いて、シーンを変更した状態にする。 */
async function drawRectangle(page: Page): Promise<void> {
  const canvas = page.locator(".excalidraw canvas").first();
  const box = await canvas.boundingBox();
  if (!box) throw new Error("キャンバスが表示されていない");

  // 図形ツールはショートカットで選ぶ。ツールバーのボタンはアイコンのみで、
  // ラベルが Excalidraw の翻訳に依存する。
  await page.keyboard.press("r");
  await page.mouse.move(box.x + 120, box.y + 120);
  await page.mouse.down();
  await page.mouse.move(box.x + 320, box.y + 260, { steps: 8 });
  await page.mouse.up();
}

test.describe("シーンの保存", () => {
  test("編集すると未保存が出て、保存すると消える", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // Excalidraw はマウント時にも onChange を発火するため、開いた直後は
    // 未保存が立っている。ここで見たいのは編集と保存の対応なので、いったん
    // 保存して基準を揃えてから確かめる。
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    await drawRectangle(page);

    // 3 状態の判定は保存済みシーンが基準。編集中の内容は反映されないので、
    // 反映されていないことが画面から分かる必要がある。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(page.getByText("未保存の変更あり")).toBeVisible();

    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("未保存の変更あり")).toBeHidden();
  });

  test("未保存のまま解釈しようとすると、保存を促す", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);

    await expect(
      page
        .getByText("未保存の変更は解釈に含まれません。保存してから実行してください。")
        .first(),
    ).toBeVisible();
  });

  test("保存すると、それまでの解釈結果は捨てられる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = page.locator("li.annotation").filter({ hasText: "ログイン" });
    await card.getByRole("button", { name: "解釈する" }).click();
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();

    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    // 解釈は保存済みシーンに対する結果。保存したら対象が変わっているので、
    // 古い結果のまま作成させてはならない。
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);
  });
});
