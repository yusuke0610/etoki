import { expect, test, type Page } from "@playwright/test";

import {
  annotationCard,
  annotationDetail,
  drawRectangle,
  openBoardWithMock,
} from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

/**
 * 面を開いたまま、面の下の空いたところに矩形を描く。描いた矩形は選ばれたまま
 * になり、設定欄が出る。`drawRectangle` は面が覆う位置に描くので使えない。
 */
async function drawRectangleBelowDetail(page: Page): Promise<void> {
  const box = await page.locator(".excalidraw canvas").first().boundingBox();
  if (!box) throw new Error("キャンバスが表示されていない");

  // ショートカットはキャンバスに焦点が無いと届かない。
  await page.mouse.click(box.x + box.width * 0.2, box.y + box.height * 0.9);
  await page.keyboard.press("r");
  await page.mouse.move(box.x + box.width * 0.3, box.y + box.height * 0.75);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.5, box.y + box.height * 0.88, {
    steps: 10,
  });
  await page.mouse.up();

  await expect(page.getByRole("button", { name: "元に戻す" })).toBeEnabled();
}

/**
 * 図形を選んだまま絵解きの面を開いても、帯の「絵解く」が押せる（#262）。
 *
 * 図形を選ぶと Excalidraw は左に設定欄（線・背景・太さ…）を出す。面は
 * `.excalidraw` の外に置いてあり（ADR 0065）、重なると設定欄が押下を受けて
 * 「絵解く」が押せなかった。#259 で「絵解く」が保存を挟むようになり、描いて
 * すぐ押せる（＝図形が選ばれたまま面を開く）ので表に出た。
 */
test.describe("図形を選んだまま絵解きの面を開く", () => {
  // 並べる（パネルがキャンバスの外）と、重ねる（パネルがキャンバスに浮く）の両方。
  // 面の位置はパネルの覆う幅も引いて決めるので、置き方ごとに確かめる。
  for (const [layout, size] of [
    ["並べる", { width: 1280, height: 720 }],
    ["重ねる", { width: 1000, height: 720 }],
  ] as const) {
    test.describe(layout, () => {
      test.use({ viewport: size });

      test("設定欄が出ていても、帯の「絵解く」は押せる", async ({ page }, testInfo) => {
        await openBoardWithMock(page, baseMock());
        await drawRectangle(page);
        await annotationCard(page, "ログイン").locator(".annotation-open").click();

        const detail = annotationDetail(page, "ログイン");
        await expect(detail).toBeVisible();
        await page.screenshot({
          path: testInfo.outputPath(`detail-with-selection-${layout}.png`),
        });

        // 押せるかどうかは、押す位置で最前面にいる要素が自分かで決まる。`trial` は
        // 押さずに、他の要素が覆っていれば待って落ちる。
        await detail
          .getByRole("button", { name: "絵解く" })
          .click({ trial: true, timeout: 3000 });
      });

      // 開いたあとで図形を選び直しても同じ。面は非モーダルで、開いたまま描ける
      // ので、設定欄が出るのは開く前とは限らない。
      test("開いたあとに図形を選んでも、帯の「絵解く」は押せる", async ({ page }) => {
        await openBoardWithMock(page, baseMock());
        await annotationCard(page, "ログイン").locator(".annotation-open").click();
        const detail = annotationDetail(page, "ログイン");
        await expect(detail).toBeVisible();

        await drawRectangleBelowDetail(page);

        await detail
          .getByRole("button", { name: "絵解く" })
          .click({ trial: true, timeout: 3000 });
      });
    });
  }
});
