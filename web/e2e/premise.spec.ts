import { expect, test, type Locator } from "@playwright/test";

import {
  drawRectangle,
  openBoardWithMock,
  openPremiseDialog,
  saveScene,
} from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

/**
 * ボードの背景のダイアログ（`PremiseDialog`、ADR 0093）。キャンバスのメニューの
 * 「ボードの背景…」から開く。
 *
 * **守りたいのは、書きかけを黙って失わないことと、先に保存した人の背景を黙って
 * 上書きしないこと。** viewer の出し分けは `sharing.spec.ts`、絵解きの面に
 * 添えたものを見せるのは `interpretation.spec.ts` が見ている。
 */

function input(dialog: Locator): Locator {
  return dialog.getByRole("textbox", { name: "ボードの背景", exact: true });
}

function saveIn(dialog: Locator): Locator {
  return dialog.getByRole("button", { name: "保存", exact: true });
}

test.describe("ボードの背景", () => {
  // 切れると: 照合の基準を送らない・古い基準を送り続ける実装でも、画面だけは
  // 保存できたように見える。
  test("書いた背景を、編集を始めた時点の背景と一緒に送り、開き直すと出る", async ({
    page,
  }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), premise: "前の背景" };
    await openBoardWithMock(page, mock);

    const dialog = await openPremiseDialog(page);
    await expect(input(dialog)).toHaveValue("前の背景");
    await input(dialog).fill("acme/web のフロントエンド。\n\n「絵」は注釈のこと。");
    await saveIn(dialog).click();

    // 保存できたら閉じる。成功は通知に重ねない（ADR 0058）。
    await expect(dialog).toBeHidden();
    expect(mock.premiseRequests).toEqual([
      {
        premise: "acme/web のフロントエンド。\n\n「絵」は注釈のこと。",
        basePremise: "前の背景",
      },
    ]);

    const reopened = await openPremiseDialog(page);
    await expect(input(reopened)).toHaveValue(
      "acme/web のフロントエンド。\n\n「絵」は注釈のこと。",
    );
  });

  // **背景の応答で、保存の基準を進めない**（ADR 0092 / 0020）。開いたあとに別の
  // メンバーが保存していると、背景の応答はその版を載せて返る。それで基準を差し
  // 替えると、読み込んでいない相手の保存を、こちらの次の保存が黙って上書きする。
  // 先に書き換えられていたときに取り直したボードでも同じことが起きるので、両方を通す。
  for (const [name, conflicted] of [
    ["保存できたとき", false],
    ["先に書き換えられていたので取り直したとき", true],
  ] as const) {
    test(`他の人が保存したあとに背景を書き換えても、こちらの保存は上書きせずに知らせる（${name}）`, async ({
      page,
    }) => {
      const mock = await openBoardWithMock(page, baseMock());

      const dialog = await openPremiseDialog(page);
      await input(dialog).fill("新しい背景");
      // こちらが開いた後に、他の人が保存した状況。版が進むので基準は古くなる。
      mock.details[BOARD_ID] = {
        ...board(),
        updatedAt: "2026-08-05T09:45:00Z",
        premise: conflicted ? "ほかの人の背景" : "",
      };
      await saveIn(dialog).click();
      if (conflicted) {
        await expect(dialog.getByRole("region", { name: "いまの背景" })).toBeVisible();
        await page.keyboard.press("Escape");
      }
      await expect(dialog).toBeHidden();

      await drawRectangle(page);
      await saveScene(page);

      await expect(page.getByText("他の人がこのボードを保存しました")).toBeVisible();
      // 相手のシーンはそのまま。上書きしていないことをモック側でも確かめる。
      expect(mock.details[BOARD_ID]?.scene).toBe(board().scene);
    });
  }

  // 書きかけはダイアログの外で持つ。ダイアログに置くと、閉じただけで消える。
  test("閉じても書きかけは残り、捨てるといまの背景に戻る", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), premise: "前の背景" };
    await openBoardWithMock(page, mock);

    const dialog = await openPremiseDialog(page);
    await input(dialog).fill("書きかけ");
    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    expect(mock.premiseRequests).toEqual([]);

    const reopened = await openPremiseDialog(page);
    await expect(input(reopened)).toHaveValue("書きかけ");
    await expect(reopened.getByText(/保存していない書きかけです/)).toContainText(
      "閉じても、このボードを開いているあいだは残ります。",
    );

    await reopened.getByRole("button", { name: "書きかけを捨てる" }).click();
    await expect(input(reopened)).toHaveValue("前の背景");
    await expect(reopened.getByText(/保存していない書きかけです/)).toHaveCount(0);
  });

  // 切れると: 先に保存した人の背景を、古い本文から編集していた人が黙って上書きする。
  // 見比べる相手が画面に無いと、409 の文言（見比べてから保存し直す）に従えない。
  test("先に書き換えられていたら、いまの背景を並べ、見比べたうえで押し直せる", async ({
    page,
  }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), premise: "前の背景" };
    await openBoardWithMock(page, mock);

    const dialog = await openPremiseDialog(page);
    await input(dialog).fill("自分の背景");
    // 開いたあとに、ほかのメンバーが背景を書き換えた。
    mock.details[BOARD_ID] = { ...board(), premise: "ほかの人の背景" };
    await saveIn(dialog).click();

    // 打ち手を言う句を全部見る（「見比べてから」「保存し直す」）。
    await expect(dialog.getByRole("alert")).toContainText(
      "ほかの人が先にボードの背景を書き換えました。いまの背景と見比べてから、保存し直してください。",
    );
    const latest = dialog.getByRole("region", { name: "いまの背景" });
    await expect(latest).toContainText("ほかの人の背景");
    // 押し直すと何が起きるかと、もう 1 つの手（捨てて戻す）の両方を言う。
    await expect(latest).toContainText(
      "「保存」を押すと、この背景を上の書きかけで置き換えます。",
    );
    await expect(latest).toContainText("書きかけを捨てると、この背景に戻ります。");
    // 書きかけは捨てない。閉じもしない。
    await expect(input(dialog)).toHaveValue("自分の背景");
    expect(mock.details[BOARD_ID]?.premise).toBe("ほかの人の背景");

    await saveIn(dialog).click();
    await expect(dialog).toBeHidden();
    expect(mock.premiseRequests.map((r) => r.basePremise)).toEqual([
      "前の背景",
      "ほかの人の背景",
    ]);
    expect(mock.details[BOARD_ID]?.premise).toBe("自分の背景");
  });

  // 切り詰めない（ADR 0093）。書いたつもりの背景が届いていないことに気づけなく
  // なる。上限の数値は画面に出さない（サーバーだけが持つ、ADR 0038）。
  test("長すぎる背景は弾かれ、理由を出して書きかけを残す", async ({ page }) => {
    const mock = baseMock();
    await openBoardWithMock(page, mock);

    const dialog = await openPremiseDialog(page);
    const long = "あ".repeat(4001);
    await input(dialog).fill(long);
    await saveIn(dialog).click();

    await expect(dialog.getByRole("alert")).toContainText(
      "ボードの背景が長すぎて保存できません。短くしてください。",
    );
    await expect(dialog.locator(".error-message")).not.toContainText("4000");
    await expect(input(dialog)).toHaveValue(long);
    expect(mock.details[BOARD_ID]?.premise).toBe("");
  });

  // 背景は注釈のハッシュに入らない（ADR 0093）。書き換える場所で言わないと、
  // 作成済みのままなのを「読み直す必要が無い」印と読む。
  test("書き換えても作成済みの絵は作成済みのままだと、書く場所で言う", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());

    const dialog = await openPremiseDialog(page);
    await expect(dialog).toContainText(
      "書き換えても、作成済みの絵は「作成済み」のままです。",
    );
    await expect(dialog).toContainText("読み直すかどうかは、絵ごとに選べます。");
  });

  test("閉じると、焦点がメニューの口へ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const dialog = await openPremiseDialog(page);
    await dialog.getByRole("button", { name: "閉じる", exact: true }).click();

    await expect(dialog).toBeHidden();
    await expect(page.locator('[data-testid="main-menu-trigger"]')).toBeFocused();
  });
});
