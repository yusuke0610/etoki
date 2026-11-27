import { expect, test } from "@playwright/test";

import {
  annotationCard,
  annotationDetail,
  interpret,
  openBoardWithMock,
} from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

/**
 * 作成先の Project に書けるか（`GET /api/boards/{id}/access` の `projectAccess`、
 * ADR 0017）。**判定ではなく状態として見せる。** 実際に作れるかは作成時に GitHub が
 * 返したものが正しく、これを見て作成を止めるのではなく、できない理由を先に見せる。
 */
test.describe("作成先に書けるか", () => {
  // GitHub の資格情報に Project への書き込み権限が無くても、ブレストと解釈は
  // できる。作成だけができない。
  test("書き込み権限が無いと、作成の代わりに理由が出る", async ({ page }) => {
    const mock = baseMock();
    mock.access = {
      [BOARD_ID]: { status: 200, body: { projectAccess: "denied" } },
    };

    await openBoardWithMock(page, mock);

    const card = annotationCard(page, "ログイン");
    const detail = annotationDetail(page, "ログイン");
    await interpret(card);

    // 解釈まではできる。GitHub は要らない。
    await expect(
      detail.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();

    // 作成だけができない。押させずに理由を出す。
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(
      0,
    );
    await expect(
      detail.getByText("この Project に書き込む権限がありません。"),
    ).toBeVisible();
  });

  // 作成先の Project に書けるかは、解釈まで進まなくても読める（#217）。
  // **書けないと分かったときだけ出す。** unknown（確かめていない・確かめられ
  // なかった）を「書けません」に見せない（ADR 0017）。
  for (const [projectAccess, shown] of [
    ["denied", true],
    ["allowed", false],
    ["unknown", false],
  ] as const) {
    test(`下の帯の「書けません」：${projectAccess}`, async ({ page }) => {
      const mock = baseMock();
      mock.access = {
        [BOARD_ID]: { status: 200, body: { projectAccess } },
      };
      const accessed = page.waitForResponse((r) =>
        /^\/api\/boards\/[^/]+\/access$/.test(new URL(r.url()).pathname),
      );
      await openBoardWithMock(page, mock);
      await accessed;

      const footer = page.locator(".board-context");
      // 作成先は Project まで書く。リポジトリ名だけでは作る先が決まらない。
      await expect(footer.locator(".badge-target")).toHaveText(
        "acme/web › #1 ロードマップ",
      );
      const denied = footer.getByText("書けません", { exact: true });
      if (shown) {
        await expect(denied).toBeVisible();
      } else {
        // 応答を受けてから 1 回描かれるのを待ち、出ていないことを見る。
        await page.evaluate(
          () => new Promise((resolve) => requestAnimationFrame(() => resolve(null))),
        );
        await expect(denied).toHaveCount(0);
      }
    });
  }
});
