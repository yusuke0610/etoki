import { expect, test } from "@playwright/test";

import { breakAnnotations, breakBoards, installApi } from "./helpers/api";
import { baseMock } from "./helpers/fixtures";

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
  test("注釈パネルが落ちてもキャンバスは残る", async ({ page }) => {
    await installApi(page, baseMock());
    await breakAnnotations(page);

    await page.goto("/");
    await page.locator(".board-list").getByRole("button", { name: BOARD_NAME }).click();

    // 落ちたのはパネルだけ。キャンバスと保存の導線が生きていることが要点で、
    // ここが消えるなら未保存のブレストごと消えている。
    await expect(page.getByRole("alert")).toContainText("この部分を表示できませんでした");
    await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
    await expect(page.getByRole("button", { name: "保存" })).toBeEnabled();
    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
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
