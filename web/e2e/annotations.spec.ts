import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

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

  // 逆方向同期は実装しないので、作成時に記録したものが唯一の手がかり
  // （ADR 0022）。
  test("前回作成した項目の本文が読める", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("前回作成した 2 件").click();

    const item = card.locator("li").filter({ hasText: "再設定メールを送る" }).last();
    await item.getByText("本文", { exact: true }).click();
    await expect(item.getByText("有効期限つきのリンクを送る")).toBeVisible();
  });

  // 記録を始める前に作った item は本文を持たない。GitHub からは取り直せない
  // ので、無いことをそのまま出す。
  test("本文を記録していない項目は、無いことが分かる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByText("前回作成した 1 件").click();

    await expect(card.getByText("本文なし")).toBeVisible();
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
