import { expect, test } from "@playwright/test";

import { breakAnnotations, breakBoards, installApi } from "./helpers/api";
import { drawRectangle, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

/**
 * 描画中に落ちたときの見せ方（ADR 0027）。
 *
 * ここで確かめるのは**キャンバスを巻き込まないこと**。React は境界が無いと
 * ツリー全体を unmount するので、境界の位置を外側の 1 枚だけに戻すと、この
 * spec が落ちる。シーンは保存するまで Excalidraw のメモリにしかないため、
 * unmount された時点で描いたものは取り返せない。
 *
 * 落とし方は API の応答を壊す（`breakAnnotations` / `breakBoards`）。フロントは
 * 応答を検証せずに型として扱うので、契約から外れた本文は render まで届く。
 */

const BOARD_NAME = "認証まわりのブレスト";

test.describe("描画に失敗したとき", () => {
  test("注釈パネルが落ちても、落ちる前に描いたものは保存できる", async ({ page }) => {
    // 落ちる時刻をこちらで決める。マウント直後に落とすと描く隙が無く、
    // 「キャンバスが見えている」ことしか確かめられない。境界がキャンバスごと
    // 作り直していても、保存済みシーンから描き直されるので同じ見た目になる。
    let crash!: () => void;
    const held = new Promise<void>((resolve) => (crash = resolve));

    const mock = await installApi(page, baseMock());
    await breakAnnotations(page, held);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    crash();
    await expect(page.getByRole("alert")).toContainText("この部分を表示できませんでした");

    // 未保存の印が残っていること自体が、BoardPage が作り直されていない証拠。
    // 作り直されると dirty は初期値に戻る。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();

    // 描いたものがサーバーまで届くところまで見る。ここが要点で、届かないなら
    // 未保存のブレストは失われている。既定のシーンに rectangle は無い。
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("未保存", { exact: true })).toHaveCount(0);

    const saved = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements?: { type: string }[];
    };
    expect(saved.elements?.some((el) => el.type === "rectangle")).toBe(true);
  });

  test("パネルの中で落ちても、外側は読み込み直しを迫らない", async ({ page }) => {
    await installApi(page, baseMock());
    await breakAnnotations(page);

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: BOARD_NAME }).click();

    await expect(page.getByRole("alert")).toBeVisible();
    // 読み込み直すと未保存のものが消える。消さずに済む落ち方で消させない。
    await expect(page.getByRole("button", { name: "読み込み直す" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "再表示" })).toBeVisible();
  });

  test("キャンバスの外で落ちたときは、失われることを言ってから読み込み直させる", async ({
    page,
  }) => {
    await installApi(page, baseMock());
    await breakBoards(page);

    await page.goto("/");

    // ここまで来るとツリーごと外れている。**残っているように書かない。**
    const alert = page.getByRole("alert");
    await expect(alert).toContainText("保存していないブレストは失われています");
    await expect(page.getByRole("button", { name: "読み込み直す" })).toBeVisible();
  });

  // 生の例外文字列は画面に出さない（#46 と同じ論点）。読めないものが増える
  // だけで、打てる手は変わらない。
  test("例外の中身は画面に出さない", async ({ page }) => {
    await installApi(page, baseMock());
    await breakAnnotations(page);

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: BOARD_NAME }).click();

    const alert = page.getByRole("alert");
    await expect(alert).toBeVisible();
    await expect(alert).not.toContainText("TypeError");
    await expect(alert).toContainText("コンソールに出しています");
  });
});
