import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, multiFrameMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

test.describe("注釈の状態", () => {
  test("3 状態がバッジとして出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(annotationCard(page, "ログイン").getByText("未作成")).toBeVisible();
    await expect(
      annotationCard(page, "パスワード再設定").getByText("作成済み"),
    ).toBeVisible();
    await expect(
      annotationCard(page, "セッション管理").getByText("変更あり"),
    ).toBeVisible();
  });

  test("粒度はサーバーが返した値が選ばれている", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 空文字は「指定なし」。粒度の判断を LLM に任せることを表す。
    await expect(annotationCard(page, "ログイン").getByLabel("粒度")).toHaveValue("");
    await expect(annotationCard(page, "パスワード再設定").getByLabel("粒度")).toHaveValue(
      "epic",
    );
    await expect(annotationCard(page, "セッション管理").getByLabel("粒度")).toHaveValue(
      "issue",
    );
  });

  test("前回作成した項目は畳まれていて、開くと中身が出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    const item = card.getByText("再設定メールを送る");

    await expect(item).toBeHidden();
    await card.getByText("前回作成した 2 件").click();
    await expect(item).toBeVisible();
  });

  // 名前を付けていない frame は Excalidraw 側も `Frame` としか描かないので、
  // 名前を頼りにすると全部同じ見出しで並ぶ（ADR 0022）。
  test("名前の無い注釈は一覧上の位置で採番して出す", async ({ page }) => {
    await installApi(page, multiFrameMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(annotationCard(page, "注釈 2")).toBeVisible();
  });

  // 押す → updateScene → onChange → 選択の反映、の一周が実ブラウザで
  // 通ることを見る。ここが切れると、どのカードがどのフレームか分からない
  // という元の状態に戻る。
  test("カードを押すとキャンバスでそのフレームが選ばれ、カードが強調される", async ({
    page,
  }) => {
    await installApi(page, multiFrameMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "注釈 2");
    await expect(card).not.toHaveAttribute("aria-current", "true");

    await card.getByRole("button", { name: "注釈 2" }).click();

    await expect(card).toHaveAttribute("aria-current", "true");
    await expect(annotationCard(page, "ログイン")).not.toHaveAttribute(
      "aria-current",
      "true",
    );
    // 選択とスクロールは appState の話で、保存すべき変更ではない。
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  test("注釈が無いボードでは案内を出す", async ({ page }) => {
    const mock = baseMock();
    mock.annotations[BOARD_ID] = [];
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByText("保存済みの注釈はありません。")).toBeVisible();
  });
});
