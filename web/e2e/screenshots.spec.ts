import { test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

/**
 * 主要な画面のスクリーンショットを撮る。
 *
 * 失敗時の証跡ではなく、変更を報告するときに添える成功時の画像を作るのが目的。
 * 期待値を持たないので、これ自体はリグレッションを検知しない。画面の約束を
 * 固定するのは他の spec の仕事。
 *
 * 出力先: web/e2e-output/screenshots/（gitignore 済み）
 */

const SHOT_DIR = "e2e-output/screenshots";
const BOARD_NAME = "認証まわりのブレスト";

async function shot(page: Page, name: string): Promise<void> {
  await page.screenshot({ path: `${SHOT_DIR}/${name}.png`, fullPage: false });
}

test.describe("スクリーンショット", () => {
  test("主要な画面を撮る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await shot(page, "01-boards-empty");

    await openBoard(page, BOARD_NAME);
    await shot(page, "02-board-states");

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();
    await shot(page, "03-interpretation");

    mock.annotations[BOARD_ID] = [
      {
        id: "frame-uncreated",
        name: "ログイン",
        granularity: "",
        state: "created",
        lastSyncedAt: "2026-08-05T10:00:00Z",
      },
    ];
    await card.getByRole("button", { name: "GitHub に作成する" }).click();
    await card.getByText("3 件を作成しました。").waitFor();
    await shot(page, "04-created");
  });
});
