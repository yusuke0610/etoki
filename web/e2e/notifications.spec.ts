import { expect, test } from "@playwright/test";

import { holdSave, installApi, summarize } from "./helpers/api";
import {
  annotationCard,
  drawRectangle,
  openBoard,
  openBoardWithMock,
} from "./helpers/board";
import { BOARD_ID, BOARD_NAME, baseMock, board } from "./helpers/fixtures";

const OTHER_NAME = "課金まわりのブレスト";
const OTHER_ID = "board-other";

/** 切り替え先のあるボード一覧。ボードを跨ぐ通知は 2 枚ないと確かめられない。 */
function twoBoards() {
  const mock = baseMock();
  const other = { ...board(), id: OTHER_ID, name: OTHER_NAME };

  mock.boards = [...mock.boards, summarize(other)];
  mock.details[other.id] = other;
  mock.annotations[other.id] = [];

  return mock;
}

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

  // 切れると: 別のボードの画面に「保存できませんでした」が残る。しかも
  // 「再試行」が保存するのはいま開いているボードなので、読んでいる文と
  // 起きることが食い違う。
  test("ボードを変えたら保存失敗の通知は消える", async ({ page }) => {
    const mock = twoBoards();
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await openBoardWithMock(page, mock);
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByRole("alert")).toContainText("保存できませんでした");

    // 未保存のまま離れるので確認が出る。
    page.on("dialog", (dialog) => void dialog.accept());
    await openBoard(page, OTHER_NAME);

    await expect(page.getByRole("alert")).toHaveCount(0);
  });

  // 切れると: 離れる前に投げた保存が離れたあとで失敗し、別のボードの画面に
  // 「保存できませんでした」が出る。その「再試行」は離れたボードの save を
  // 呼ぶので、捨てると決めたシーンを前のボードへ保存しにいく。
  // 上のテストと違い、離れる時点ではまだ通知が出ていない（応答待ち）。
  test("離れたあとに失敗した保存は通知しない", async ({ page }) => {
    const mock = twoBoards();
    await openBoardWithMock(page, mock);
    // installApi より後に登録する（後に登録したルートが先に当たる）。
    let release = () => {};
    await holdSave(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    page.on("dialog", (dialog) => void dialog.accept());
    await openBoard(page, OTHER_NAME);

    // 離れたあとで保存が失敗して返る。
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    const failed = page.waitForResponse(
      (r) =>
        r.request().method() === "PUT" &&
        new URL(r.url()).pathname === `/api/boards/${BOARD_ID}/scene`,
    );
    release();
    expect((await failed).status()).toBe(500);

    // 応答を受けた処理が通知を出すまでの間を置いてから、出ていないことを見る。
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));
    await expect(page.locator(".notifications .notification")).toHaveCount(0);
  });

  // 切れると: 離れたボードの取得が成功した時点で `dismissKey` が走り、通知は
  // ボードより上（`NotificationProvider`）に key で消されるので、**いま開いて
  // いるボードに出ている同じ通知が消える。** 出す側だけを塞いでも足りない。
  test("離れたあとに届いた取得成功で、別のボードの通知を消さない", async ({ page }) => {
    const mock = twoBoards();
    await installApi(page, mock);

    // **ボードごとに分ける。** 片方は止めて成功させ、もう片方は失敗させたいので、
    // 共有の `breakAnnotations`（どのボードにも当たる）では作れない並び。
    let release = (): void => {};
    const held = new Promise<void>((r) => (release = () => r()));
    await page.route(
      (url) => url.pathname === `/api/boards/${BOARD_ID}/annotations`,
      async (route) => {
        if (route.request().method() !== "GET") {
          await route.fallback();
          return;
        }
        await held;
        await route.fallback();
      },
    );
    await page.route(
      (url) => url.pathname === `/api/boards/${OTHER_ID}/annotations`,
      async (route) => {
        if (route.request().method() !== "GET") {
          await route.fallback();
          return;
        }
        await route.fulfill({
          status: 500,
          contentType: "application/json",
          body: JSON.stringify({ code: "internal", error: "internal error" }),
        });
      },
    );

    await page.goto("/");
    // 1 枚目は注釈の取得を止めたまま開く。
    await openBoard(page, BOARD_NAME);
    // 2 枚目へ移る。こちらの取得は失敗するので通知が出る。
    await openBoard(page, OTHER_NAME);

    const alert = page.getByRole("alert");
    await expect(alert).toContainText("注釈の状態を取得できませんでした");

    // ここで 1 枚目の取得が成功して返る。
    release();
    await page.waitForTimeout(500);

    // 2 枚目の通知は残る。
    await expect(page.getByRole("alert")).toContainText(
      "注釈の状態を取得できませんでした",
    );
  });
});
