import { expect, test } from "@playwright/test";

import { failAnnotations, holdAnnotations, holdSave } from "./helpers/api";
import {
  annotationCard,
  annotationDetail,
  drawRectangle,
  ekidokiButton,
  openAnnotationDetail,
  openBoardWithMock,
  openPanelTab,
} from "./helpers/board";
import { annotationsOfScene, emptyScene } from "./helpers/boardData";
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
  // 注釈が 1 件の構成。どれを開くかを人に選ばせる余地が無い。**保存済みの一覧も
  // シーンに揃える。** 開く先は保存済みの一覧で数えるので、既定の 3 件のままだと
  // 一覧を開く。
  function singleAnnotationMock() {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), scene: annotatedScene() };
    mock.annotations[BOARD_ID] = annotationsOfScene(annotatedScene());
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

  // **待つあいだに変わったら、保存し直してから開く**（#259 のレビュー）。待つ
  // あいだに付けた注釈を開くと、保存済みの一覧に無いので詳細がその場で閉じる。
  test("保存を待つあいだに注釈を付けたら、保存し直してから開く", async ({ page }) => {
    const mock = singleAnnotationMock();
    mock.annotationsFollowSave = true;
    const installed = await openBoardWithMock(page, mock);
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    await holdSave(page, held);

    // 外してから押し、保存が注釈の無いシーンを送ったあとで付け直す。
    await (
      await openAnnotationDetail(page, "ログイン")
    )
      .getByRole("button", { name: "キャンバスで見る" })
      .click();
    await page.getByRole("button", { name: /の注釈を外す/ }).click();
    const saving = page.waitForRequest(
      (r) =>
        r.method() === "PUT" &&
        new URL(r.url()).pathname === `/api/boards/${BOARD_ID}/scene`,
    );
    await ekidokiButton(page).click();
    await saving;
    await page.getByRole("button", { name: /を注釈にする/ }).click();

    release();
    await expect(annotationDetail(page, "ログイン")).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    expect(installed.saveRequests).toHaveLength(2);
  });

  // **一覧を取り直すあいだに変わっても、保存してから開く**（#259 のレビュー）。
  // 保存が要らないときも一覧は取り直すので、その待ちも保存を待つのと同じ隙になる。
  test("一覧の取り直しを待つあいだに描き足したら、保存してから開く", async ({ page }) => {
    const mock = await openBoardWithMock(page, singleAnnotationMock());
    await expect(annotationCard(page, "ログイン")).toBeVisible();
    let release!: () => void;
    const held = new Promise<void>((resolve) => (release = resolve));
    await holdAnnotations(page, held);

    const refreshing = page.waitForRequest(
      (r) =>
        r.method() === "GET" &&
        new URL(r.url()).pathname === `/api/boards/${BOARD_ID}/annotations`,
    );
    await ekidokiButton(page).click();
    await refreshing;
    await drawRectangle(page);

    release();
    await expect(annotationDetail(page, "ログイン")).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    expect(mock.saveRequests).toHaveLength(1);
  });

  // **数えるのは保存済みの一覧で、キャンバスではない**（#259 のレビュー）。状態は
  // 保存済みシーンが基準。キャンバスで数えると、フレームが画面に無い注釈を数え
  // そこね、1 件なのに一覧を開く。
  test("保存済みの注釈が 1 件なら、フレームが画面に無くてもその面を開く", async ({
    page,
  }) => {
    const mock = singleAnnotationMock();
    mock.details[BOARD_ID] = { ...board(), scene: emptyScene() };
    await openBoardWithMock(page, mock);

    await ekidokiButton(page).click();

    await expect(annotationDetail(page, "ログイン")).toBeVisible();
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

  // **一覧を取り直せなかったら開かない**（#259 のレビュー）。開く先は取り直した
  // 一覧で数えるので、前の一覧のままでは決められない。理由は取得の失敗として
  // 通知が言う。開くのは通知を出したあとなので、間を置いてから見る。
  test("保存のあと注釈の一覧を取り直せなかったら、開かない", async ({ page }) => {
    await openBoardWithMock(page, singleAnnotationMock());
    await expect(annotationCard(page, "ログイン")).toBeVisible();
    await failAnnotations(page);
    await drawRectangle(page);

    await ekidokiButton(page).click();

    await expect(page.getByRole("alert")).toContainText(
      "注釈の状態を取得できませんでした",
    );
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));
    await expect(annotationDetail(page, "ログイン")).toHaveCount(0);
  });

  // **保存が要らないときも取り直す。** 手元の一覧は、前の取り直しが失敗していれば
  // 古い（#259 のレビュー）。
  test("保存が要らないときも一覧を取り直し、取れなかったら開かない", async ({ page }) => {
    const mock = await openBoardWithMock(page, singleAnnotationMock());
    await expect(annotationCard(page, "ログイン")).toBeVisible();
    await failAnnotations(page);

    await ekidokiButton(page).click();

    await expect(page.getByRole("alert")).toContainText(
      "注釈の状態を取得できませんでした",
    );
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve, 200)));
    await expect(annotationDetail(page, "ログイン")).toHaveCount(0);
    expect(mock.saveRequests).toHaveLength(0);
  });

  // 右上のボタンはどの注釈かを指していない。複数あるなら、選ぶのは人
  // （中核思想 3）。別のタブを開いていても、畳んでいても、注釈の一覧に戻す。
  test("注釈が複数なら、パネルの注釈の一覧を開く", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await openPanelTab(page, "etoki AI");
    await page.getByRole("button", { name: "パネルを閉じる" }).click();

    await ekidokiButton(page).click();

    await expect(
      page.getByRole("tab", { name: "絵解いた", exact: true }),
    ).toHaveAttribute("aria-selected", "true");
    await expect(
      page.getByRole("tabpanel", { name: "絵解いた", exact: true }),
    ).toBeVisible();
    await expect(page.locator("section.annotation-detail:not([hidden])")).toHaveCount(0);
  });
});
