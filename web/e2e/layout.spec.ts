import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { BOARD_NAME, baseMock } from "./helpers/fixtures";

// 狭い画面（モバイルのツールバー）の右上の島。ツールバーと島を横に並べたままだと
// 行ごと右へはみ出し、`.excalidraw` の `overflow: hidden` に切られて、保存が
// 画面の外に出る。
test.describe("狭い画面", () => {
  test.use({ viewport: { width: 600, height: 800 } });

  test("右上の保存は、キャンバスの内側に収まる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const canvas = await page.locator(".excalidraw").first().boundingBox();
    const save = await page
      .getByRole("button", { name: "保存", exact: true })
      .boundingBox();
    if (!canvas || !save) throw new Error("キャンバスか保存ボタンが表示されていない");

    expect(save.x).toBeGreaterThanOrEqual(canvas.x);
    expect(save.x + save.width).toBeLessThanOrEqual(canvas.x + canvas.width);
  });
});
