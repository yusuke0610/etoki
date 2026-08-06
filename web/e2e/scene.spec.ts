import { expect, test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

/**
 * キャンバスに矩形を 1 つ描いて、シーンを変更した状態にする。
 *
 * 座標はキャンバスからの相対で取る。左上に固定オフセットで打つと、ツールバーや
 * 案内文が重なっていてポインタが canvas に届かない。実際それで 1 つも描けて
 * いなかったが、当時は onChange が発火しただけで未保存になっていたためテストは
 * 通っていた。描けたことを undo の活性で確かめてから返す。
 */
async function drawRectangle(page: Page): Promise<void> {
  const canvas = page.locator(".excalidraw canvas").first();
  const box = await canvas.boundingBox();
  if (!box) throw new Error("キャンバスが表示されていない");

  // 図形ツールはショートカットで選ぶ。ツールバーのラベルは Excalidraw の翻訳に
  // 依存する。ただしショートカットはキャンバスにフォーカスが無いと届かないので、
  // 先に何も無いところをクリックしておく。
  await page.mouse.click(box.x + box.width * 0.5, box.y + box.height * 0.85);
  await page.keyboard.press("r");

  await page.mouse.move(box.x + box.width * 0.35, box.y + box.height * 0.4);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.6, box.y + box.height * 0.65, {
    steps: 10,
  });
  await page.mouse.up();

  await expect(page.getByRole("button", { name: "元に戻す" })).toBeEnabled();
}

test.describe("シーンの保存", () => {
  // Excalidraw はマウント時にも onChange を発火する。それを編集として扱うと、
  // 開いただけで未保存になり、表示が信用できなくなる。
  test("開いた直後は未保存にならない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("未保存の変更あり")).toBeHidden();
  });

  // 選択やスクロールでも onChange は発火する。見ただけで未保存になってはならない。
  test("キャンバスを触っただけでは未保存にならない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const canvas = page.locator(".excalidraw canvas").first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error("キャンバスが表示されていない");

    // 何も無いところをクリックし、選択矩形をドラッグする。要素は増えない。
    await page.mouse.click(box.x + 200, box.y + 200);
    await page.mouse.move(box.x + 150, box.y + 150);
    await page.mouse.down();
    await page.mouse.move(box.x + 400, box.y + 320, { steps: 8 });
    await page.mouse.up();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  test("編集すると未保存が出て、保存すると消える", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

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
