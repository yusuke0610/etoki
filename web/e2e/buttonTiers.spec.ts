import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import {
  annotationCard,
  annotationDetail,
  chooseFromMenu,
  newBoardDialog,
  openBoardWithMock,
  openPanelTab,
} from "./helpers/board";
import { authRequiredMock, BOARD_ID, baseMock } from "./helpers/fixtures";

/**
 * ボタンの格（#203）。どの操作がどの格かを固定する。
 *
 * **見るのはクラスで、色ではない。** 格の見た目は `web/src/index.css` が 1 か所で
 * 決めている。色を断言すると、トークンの値を変えるたびにここまで直すことになり、
 * 守りたい「どの操作がどの格か」とは関係の無い理由で落ちる。
 */
const PRIMARY = /(^|\s)primary(\s|$)/;
const QUIET = /(^|\s)quiet(\s|$)/;
const DANGER = /(^|\s)danger(\s|$)/;
const ANY_TIER = /(^|\s)(primary|quiet|danger)(\s|$)/;

test.describe("ボタンの格", () => {
  test("ログインは主となる操作", async ({ page }) => {
    await installApi(page, authRequiredMock());
    await page.goto("/");

    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toHaveClass(
      PRIMARY,
    );
  });

  // 「GitHub に作成する」は取り消せないが、赤ではなく主となる操作にする。赤は
  // 「消す」操作だけに使う（#203）。
  // ボード一覧の画面と、新しいボードのダイアログ（#200）。どちらも面ごとに主となる
  // 操作は 1 つ。
  test("新しいボードと次へは主となる操作、ダイアログのキャンセルは控えめ", async ({
    page,
  }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    await expect(
      page.getByRole("button", { name: "新しいボード", exact: true }),
    ).toHaveClass(PRIMARY);
    const dialog = await newBoardDialog(page);
    await expect(dialog.getByRole("button", { name: "次へ" })).toHaveClass(PRIMARY);
    await expect(dialog.getByRole("button", { name: "キャンセル" })).toHaveClass(QUIET);
  });

  test("保存と作成は主となる操作、解釈はふつう、詳細を閉じるのは控えめ", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.getByRole("button", { name: "保存", exact: true })).toHaveClass(
      PRIMARY,
    );

    const interpret = annotationCard(page, "ログイン").getByRole("button", {
      name: "解釈する",
    });
    await expect(interpret).not.toHaveClass(ANY_TIER);
    await interpret.click();

    const detail = annotationDetail(page, "ログイン");
    await expect(detail.getByRole("button", { name: "GitHub に作成する" })).toHaveClass(
      PRIMARY,
    );
    await expect(detail.getByRole("button", { name: "閉じる" })).toHaveClass(QUIET);
  });

  test("ボードの削除は取り消せない操作、やめるのは控えめ", async ({ page }) => {
    const mock = baseMock();
    mock.deletion = { [BOARD_ID]: { status: 200, body: { recordedItemCount: 0 } } };
    await openBoardWithMock(page, mock);

    await chooseFromMenu(page, "ボードを削除");
    const confirm = page.getByRole("alertdialog");
    await expect(confirm.getByRole("button", { name: "削除する" })).toHaveClass(DANGER);
    await expect(confirm.getByRole("button", { name: "やめる" })).toHaveClass(QUIET);
  });

  test("メンバーを外すのは取り消せない操作", async ({ page }) => {
    const mock = baseMock();
    mock.members = {
      [BOARD_ID]: [
        {
          userId: "user-alice",
          login: "alice",
          displayName: "Alice",
          role: "owner",
          createdAt: "2026-08-01T09:00:00Z",
        },
        {
          userId: "user-bob",
          login: "bob",
          displayName: "Bob",
          role: "editor",
          createdAt: "2026-08-03T09:00:00Z",
        },
      ],
    };
    await openBoardWithMock(page, mock);

    const panel = await openPanelTab(page, "メンバー");
    await panel.getByText("Bob").waitFor();
    const remove = panel.getByRole("button", { name: /を外す$/ });
    await expect(remove.first()).toBeVisible();
    for (const button of await remove.all()) {
      await expect(button).toHaveClass(DANGER);
    }
  });
});
