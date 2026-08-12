import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { drawRectangle, openBoard } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

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
      page.getByText("保存してから解釈できます", { exact: false }).first(),
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
