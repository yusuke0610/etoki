import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, drawRectangle, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

/**
 * 画面全体に出す失敗の通知（ADR 0058、#89）。
 *
 * 各テストは、通知に移したものと移さなかったものの線引きを守っている。
 * **切れると何が起きるかを、それぞれのコメントに書いてある。** 後から消して
 * よいかは、そこで判断する。
 */
test.describe("通知", () => {
  // 一覧が読めないと、左が空のまま戻る手が画面に無かった。
  test("一覧の取得に失敗したら、通知から読み直せる", async ({ page }) => {
    const mock = baseMock();
    mock.boardsError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await installApi(page, mock);
    await page.goto("/");

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("ボード一覧を取得できませんでした");

    // 復帰してから押す。押した時点で読み直すこと。
    delete mock.boardsError;
    const listed = page.waitForRequest(
      (r) => r.method() === "GET" && new URL(r.url()).pathname === "/api/boards",
    );
    await alert.getByRole("button", { name: "再読み込み" }).click();
    await listed;

    await expect(
      page.locator(".board-list").getByRole("button", { name: BOARD_NAME }),
    ).toBeVisible();
    await expect(alert).toBeHidden();
  });

  // 保存の失敗から、その場で押し直せる。409 以外。
  test("保存に失敗したら、通知から再試行できる", async ({ page }) => {
    const mock = baseMock();
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("保存できませんでした");

    delete mock.saveSceneError;
    const saved = page.waitForRequest(
      (r) =>
        r.method() === "PUT" &&
        new URL(r.url()).pathname === `/api/boards/${BOARD_ID}/scene`,
    );
    await alert.getByRole("button", { name: "再試行" }).click();
    await saved;

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByRole("alert")).toHaveCount(0);
  });

  // 保存が通ったら、前の失敗は下げる。残すと「保存できませんでした」が
  // 保存済みの画面に並ぶ。
  test("保存できたら、前の保存の失敗は消える", async ({ page }) => {
    const mock = baseMock();
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByRole("alert")).toContainText("保存できませんでした");

    delete mock.saveSceneError;
    // 通知の「閉じる」も名前に「保存」を含む（本文が入る）ので、完全一致で引く。
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByRole("alert")).toHaveCount(0);
  });

  // 切れると: 衝突が通知に流れ、閉じたら相手の変更を消したのかどうか読めなく
  // なる（ADR 0020）。衝突は失敗ではなく状態なので、帯のまま残す。
  test("保存の衝突は通知に流さない", async ({ page }) => {
    const mock = baseMock();
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    mock.details[BOARD_ID] = { ...board(), updatedAt: "2026-08-09T00:00:00Z" };
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("他の人がこのボードを保存しました")).toBeVisible();
    await expect(page.locator(".notifications .notification")).toHaveCount(0);
  });

  // 切れると: どの注釈で失敗したのかが分からなくなる。
  test("解釈の失敗は通知に流さず、注釈のパネルに残す", async ({ page }) => {
    const mock = baseMock();
    mock.interpret = { status: 500, body: { code: "internal", error: "boom" } };
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByRole("alert")).toBeVisible();
    await expect(page.locator(".notifications .notification")).toHaveCount(0);
  });

  // 切れると: #89 の 1-1 が戻る。後から来た失敗が前を黙って消していた。
  test("2 つの失敗が重なっても、両方読める", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    mock.refreshTargetDisplayError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByRole("alert")).toContainText("保存できませんでした");
    await page.getByRole("button", { name: "作成先の名前を取り直す" }).click();

    const alerts = page.locator(".notifications").getByRole("alert");
    await expect(alerts).toHaveCount(2);
    await expect(alerts.nth(0)).toContainText("作成先の名前を取り直せませんでした");
    await expect(alerts.nth(1)).toContainText("保存できませんでした");
  });

  // 切れると: #89 の 1-3 が戻る。失敗が出た瞬間にキャンバスが縮み、描いている
  // 最中に描画位置が動く。
  test("通知が出てもキャンバスの大きさは変わらない", async ({ page }) => {
    const mock = baseMock();
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    const canvas = page.locator(".canvas");
    const before = await canvas.boundingBox();
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByRole("alert")).toContainText("保存できませんでした");

    expect(await canvas.boundingBox()).toEqual(before);
  });
});
