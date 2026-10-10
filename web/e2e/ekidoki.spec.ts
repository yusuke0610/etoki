import { expect, test } from "@playwright/test";

import {
  annotationDetail,
  drawRectangle,
  ekidokiButton,
  openBoardWithMock,
  openPanelTab,
} from "./helpers/board";
import { BOARD_ID, annotatedScene, baseMock, board } from "./helpers/fixtures";

/**
 * 右上の「絵解き」（#247）。未保存なら保存してから、絵解きの面かパネルを開く。
 *
 * **守りたいのは 2 つ。** 保存を知らなくても絵解きまで進めること（保存が要るのは
 * etoki の都合、ADR 0018）。そして、**保存が要らないときに保存しないこと。**
 * 保存は全部の注釈の引いた解釈と作成の状態を捨てるので、押すたびに保存すると、
 * 開き直すたびに前の結果が消える。
 */
test.describe("右上の「絵解き」", () => {
  // 注釈が 1 件の構成。どれを開くかを人に選ばせる余地が無い。
  function singleAnnotationMock() {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), scene: annotatedScene() };
    return mock;
  }

  test("注釈が 1 件なら、その絵解きの面を開く。保存済みなら保存しない", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, singleAnnotationMock());

    await ekidokiButton(page).click();

    await expect(annotationDetail(page, "ログイン")).toBeVisible();
    expect(mock.saveRequests).toHaveLength(0);
  });

  test("未保存なら、保存してから開く", async ({ page }) => {
    const mock = await openBoardWithMock(page, singleAnnotationMock());
    await drawRectangle(page);

    await ekidokiButton(page).click();

    await expect(annotationDetail(page, "ログイン")).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    expect(mock.saveRequests).toHaveLength(1);
  });

  // 保存できなかったら開かない。理由は保存の失敗として通知が言う。開くと、
  // 保存できていないシーンの絵解きに進めてしまう。
  test("保存に失敗したら、開かない", async ({ page }) => {
    const mock = singleAnnotationMock();
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    await openBoardWithMock(page, mock);
    await drawRectangle(page);

    await ekidokiButton(page).click();

    await expect(page.getByRole("alert")).toContainText("保存できませんでした");
    await expect(annotationDetail(page, "ログイン")).toHaveCount(0);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 右上のボタンはどの注釈かを指していない。複数あるなら、選ぶのは人
  // （中核思想 3）。別のタブを開いていても、畳んでいても、注釈の一覧に戻す。
  test("注釈が複数なら、パネルの注釈の一覧を開く", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await openPanelTab(page, "図のドラフト");
    await page.getByRole("button", { name: "パネルを閉じる" }).click();

    await ekidokiButton(page).click();

    await expect(page.getByRole("tab", { name: "注釈", exact: true })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await expect(page.getByRole("tabpanel", { name: "注釈", exact: true })).toBeVisible();
    await expect(page.locator("section.annotation-detail:not([hidden])")).toHaveCount(0);
  });
});
