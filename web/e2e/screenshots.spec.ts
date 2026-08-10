import { test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, drawRectangle, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, signedIn, unselectedBoard } from "./helpers/fixtures";

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

  // ブレストに入る前の画面。作成先を選ばないとキャンバスが出ない（ADR 0014）。
  test("作成先の選択を撮る", async ({ page }) => {
    const mock = baseMock();
    const board = unselectedBoard();
    mock.boards = [
      {
        id: board.id,
        name: board.name,
        role: board.role,
        createdAt: board.createdAt,
        updatedAt: board.updatedAt,
      },
    ];
    mock.details = { [board.id]: board };
    mock.annotations = { [board.id]: [] };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: board.name }).click();
    await page.getByRole("heading", { name: "リポジトリ" }).waitFor();
    await shot(page, "05-target-repositories");

    await page.getByRole("button", { name: /acme\/web/ }).click();
    await page.getByRole("button", { name: "#1 ロードマップ" }).waitFor();
    await shot(page, "06-target-projects");

    await page.getByRole("button", { name: "#1 ロードマップ" }).click();
    await page.locator(".badge-target").waitFor();
    // バッジはキャンバスより先に出る。ここで待たないと Excalidraw の
    // 「Loading scene...」を撮ってしまい、報告に使えない画像になる。
    await page.locator(".excalidraw canvas").first().waitFor();
    await page.getByRole("heading", { name: "注釈" }).waitFor();
    await shot(page, "07-target-selected");
  });

  // 押せない理由は title ではなく本文で出す。ホバーできない利用者と読み上げにも
  // 届く必要がある。見た目の話でもあるので撮る。
  test("作成先を変更できない状態を撮る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);
    await page.getByText("未保存", { exact: true }).waitFor();
    await shot(page, "08-target-change-blocked");
  });

  // 認証を設定した構成の入口。ここを通らないとボードに触れない（ADR 0015）。
  test("ログイン画面を撮る", async ({ page }) => {
    const mock = baseMock();
    mock.session = { status: 200, body: { authRequired: true, authenticated: false } };

    await installApi(page, mock);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();
    await shot(page, "09-login");

    // ログイン後はサイドバーに利用者が出る。
    mock.session = { status: 200, body: signedIn() };
    await page.reload();
    await page.getByText("Octo Cat").waitFor();
    await shot(page, "10-signed-in");
  });
});
