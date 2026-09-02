import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { annotatedScene, baseMock, board, BOARD_ID } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

/**
 * フォントを配る URL。正本は `web/vite.config.ts` の `EXCALIDRAW_ASSET_PATH`。
 *
 * **写しだが、ずれれば落ちる。** 下のテストは「ここへの応答が 1 つ以上ある」
 * ことを先に確かめるので、配る側が別の場所へ移れば緑のままにはならない。
 */
const ASSET_PATH = "/excalidraw-assets/";

/**
 * キャンバスのフォントを外から取ってこないこと（#114）。
 *
 * **ここでしか気づけない。** Excalidraw は指定した先が 404 になると必ず
 * esm.sh へ落ちるので、配り漏れても画面は普通に描ける。ネットワークのある
 * 環境なら見た目すら変わらない。
 */
test.describe("キャンバスのフォント", () => {
  test("自前の配信元から取り、外部のホストへ出ていかない", async ({ page }) => {
    const external: string[] = [];
    const fonts: number[] = [];

    // **素通しにしない。** 通すと、ネットワークのある環境では CDN から取れて
    // しまい、配り漏れが緑のまま通る。傍受してから落とす。
    await page.route(/^https?:\/\/(?!127\.0\.0\.1:5173\/)/, async (route) => {
      external.push(route.request().url());
      await route.abort();
    });

    page.on("response", (response) => {
      if (response.url().includes(`${ASSET_PATH}fonts/`)) {
        fonts.push(response.status());
      }
    });

    // **手書きフォントを取りに行くのは、描く文字があるときだけ。** 既定の
    // シーンは frame しか持たないので、そのままだと 1 つも要求が出ず、
    // 「外部に出ていない」が何もしていないだけで成り立ってしまう。
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), scene: annotatedScene() };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 取りに行くまで待つ。**外部への要求も待つ対象に入れる。** 自前の側だけを
    // 待つと、CDN へ出ていったときに「まだ何もしていない」と区別が付かず、
    // どこへ出ていったのかが失敗の出力に残らない。
    await expect.poll(() => fonts.length + external.length).toBeGreaterThan(0);

    expect(external).toEqual([]);
    expect(fonts.filter((status) => status !== 200)).toEqual([]);
  });
});
