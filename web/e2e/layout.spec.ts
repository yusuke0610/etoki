import { expect, test, type Locator, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { summarize } from "./helpers/boardData";
import {
  annotationCard,
  annotationDetail,
  backToList,
  drawRectangle,
  interpret,
  newBoardDialog,
  openBoardWithMock,
  openMermaidPaste,
  openPanelTab,
  picker,
  ekidokiButton,
  saveScene,
} from "./helpers/board";
import {
  BOARD_ID,
  BOARD_NAME,
  authRequiredMock,
  baseMock,
  board,
  unselectedBoard,
} from "./helpers/fixtures";

/**
 * 画面の幅に合わせた置き方（#199、`web/src/board/layout.ts`）。
 *
 * 置き方は 3 つ。並べる（右のパネルをキャンバスの横に）・重ねる（キャンバスに
 * 浮かせる）・スマホ（パネルは全面、右上と下の帯の中身はキャンバスの上の帯へ）。
 */

/** etoki の置き方。`.board` の `data-layout`。 */
function boardLayout(page: Page) {
  return page.locator(".board");
}

/** Excalidraw の枠。モバイル用 UI なら `excalidraw--mobile` が付く。 */
function excalidraw(page: Page) {
  return page.locator(".excalidraw").first();
}

/** スマホの上の帯の 2 段目（`BoardBar`）。 */
function boardBar(page: Page) {
  return page.locator(".board-bar-row");
}

/**
 * 横にスクロールできる入れ物を挙げる。**上の帯の 2 段目だけは例外**で、
 * はみ出したら横に流す（#199）。Excalidraw の中は etoki の持ち物ではないので
 * 見ない。
 */
async function horizontalScrollers(page: Page): Promise<string[]> {
  return page.evaluate(() => {
    const found: string[] = [];
    const root = document.scrollingElement;
    if (root && root.scrollWidth > root.clientWidth) found.push("document");
    for (const el of document.querySelectorAll<HTMLElement>("body *")) {
      if (el.closest(".excalidraw, .board-bar-row")) continue;
      const { overflowX } = getComputedStyle(el);
      if (
        (overflowX === "auto" || overflowX === "scroll") &&
        el.scrollWidth > el.clientWidth
      ) {
        found.push(`${el.tagName.toLowerCase()}.${el.className}`);
      }
    }
    return found;
  });
}

/** 2 つの要素の矩形が重なっていないこと。 */
async function expectApart(a: Locator, b: Locator): Promise<void> {
  const [boxA, boxB] = await Promise.all([a.boundingBox(), b.boundingBox()]);
  if (boxA === null || boxB === null) throw new Error("見えていない要素は比べられない");
  const overlap =
    boxA.x < boxB.x + boxB.width &&
    boxB.x < boxA.x + boxA.width &&
    boxA.y < boxB.y + boxB.height &&
    boxB.y < boxA.y + boxA.height;
  expect(overlap, `${JSON.stringify(boxA)} と ${JSON.stringify(boxB)} が重なる`).toBe(
    false,
  );
}

/** 保存に失敗させて、画面全体の通知（ADR 0058）を出す。 */
async function showSaveFailure(page: Page): Promise<Locator> {
  const mock = baseMock();
  mock.saveSceneError = {
    status: 500,
    body: { code: "internal", error: "internal error" },
  };
  await openBoardWithMock(page, mock);
  await drawRectangle(page);
  await saveScene(page);
  const notification = page.locator(".notifications").getByRole("alert");
  await expect(notification).toContainText("保存できませんでした");
  return notification;
}

// ライブラリを上げて Excalidraw の境目の定数が変わったら、ここが落ちる。
// `layout.ts` は式と定数を写しているので、写しが古くなったことに気づける場所は
// 実物を見るここしかない。**境目の両側を見る。** 1050 で並べるとキャンバスは
// ちょうど 730 になり、境目が 1px でもずれれば片側が食い違う。
test.describe("置き方と Excalidraw の判定が揃っている", () => {
  for (const { width, height, layout, mobile } of [
    { width: 375, height: 812, layout: "phone", mobile: true },
    { width: 812, height: 375, layout: "phone", mobile: true },
    { width: 729, height: 900, layout: "phone", mobile: true },
    { width: 730, height: 900, layout: "overlay", mobile: false },
    { width: 999, height: 499, layout: "phone", mobile: true },
    { width: 999, height: 500, layout: "overlay", mobile: false },
    { width: 1024, height: 768, layout: "overlay", mobile: false },
    { width: 1049, height: 900, layout: "overlay", mobile: false },
    { width: 1050, height: 900, layout: "side", mobile: false },
    { width: 1440, height: 900, layout: "side", mobile: false },
  ]) {
    test(`${width} × ${height} は ${layout}`, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await openBoardWithMock(page, baseMock());

      await expect(boardLayout(page)).toHaveAttribute("data-layout", layout);
      // スマホでないなら、**パネルを開いたまま**でもモバイル用 UI にならない。
      if (layout !== "phone") await expect(page.locator(".side-panel")).toBeVisible();
      if (mobile) await expect(excalidraw(page)).toHaveClass(/excalidraw--mobile/);
      else await expect(excalidraw(page)).not.toHaveClass(/excalidraw--mobile/);
    });
  }

  // 注意の帯はキャンバスを押し下げる。画面の大きさで決めると、帯が出ている
  // あいだだけ Excalidraw がモバイル用 UI になり、下の帯の中身が消える。
  // 置き方は帯を除いた枠の大きさで決める（`useBoardLayout`）。
  test("上の注意の帯でキャンバスが低くなると、スマホの置き方に切り替わる", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 900, height: 520 });
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), sceneOverLimit: true };
    await openBoardWithMock(page, mock);

    await expect(page.locator(".scene-limit-warning")).toBeVisible();
    await expect(boardLayout(page)).toHaveAttribute("data-layout", "phone");
    await expect(excalidraw(page)).toHaveClass(/excalidraw--mobile/);
    // 下の帯の中身は上の帯にある。
    await expect(boardBar(page)).toContainText("オーナー");
  });

  test("同じ大きさでも、注意の帯が無ければ重ねる", async ({ page }) => {
    await page.setViewportSize({ width: 900, height: 520 });
    await openBoardWithMock(page, baseMock());

    await expect(boardLayout(page)).toHaveAttribute("data-layout", "overlay");
    await expect(excalidraw(page)).not.toHaveClass(/excalidraw--mobile/);
  });
});

test.describe("重ねる", () => {
  test.use({ viewport: { width: 1024, height: 768 } });

  // 浮かせたパネルが、右上の島（「絵解き」と「未保存」）も下の帯（ロール・作成先・
  // 大きさ）も覆わない（ADR 0021 / 0064）。**位置で見る。** パネルは Excalidraw の
  // UI より下に置いているので、重なっていても島は上に出て押せてしまい、押せるか
  // では気づけない。
  test("パネルを開いたままでも、右上の島と下の帯に重ならない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await drawRectangle(page);
    await expect(page.locator(".excalidraw .board-status .dirty")).toBeVisible();

    const panel = await page.locator(".side-panel").boundingBox();
    const status = await page.locator(".excalidraw .board-status").boundingBox();
    const context = await page.locator(".excalidraw .board-context").boundingBox();
    if (!panel || !status || !context) throw new Error("パネルか右上の島か下の帯が無い");
    expect(panel.y).toBeGreaterThanOrEqual(status.y + status.height);
    expect(panel.y + panel.height).toBeLessThanOrEqual(context.y);
  });

  // 詳細を開いたまま隣のカードを押せるように、詳細はパネルを避けて開く。
  test("注釈の詳細は、浮かせたパネルに重ならない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await interpret(annotationCard(page, "ログイン"));

    const detail = await annotationDetail(page, "ログイン").boundingBox();
    const panel = await page.locator(".side-panel").boundingBox();
    if (!detail || !panel) throw new Error("詳細かパネルが表示されていない");
    expect(detail.x + detail.width).toBeLessThanOrEqual(panel.x);

    // 開いたまま別の注釈のカードを押せる。
    await annotationCard(page, "パスワード再設定").locator(".annotation-open").click();
    await expect(annotationDetail(page, "パスワード再設定")).toBeVisible();
  });

  // 通知は浮かせたパネルの左に出す（#216 の約束を、パネルの右の余白ぶんずらす）。
  test("通知は浮かせたパネルにも、畳んだ帯にも重ならない", async ({ page }) => {
    const notification = await showSaveFailure(page);
    await expectApart(notification, page.locator(".side-panel"));

    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    const rail = page.getByRole("navigation", { name: "パネル" });
    await expect(rail).toBeVisible();
    await expectApart(notification, rail);
  });

  // 畳んでも、キャンバスは押し縮めない（重ねたまま）。開閉は広い画面と同じく
  // 覚える（#202）。
  test("畳むと右端に帯が重なり、キャンバスの幅は変わらない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    const before = await page.locator(".canvas").boundingBox();

    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    await expect(page.locator(".side-panel-rail")).toBeVisible();

    expect(await page.locator(".canvas").boundingBox()).toEqual(before);
  });
});

test.describe("スマホ", () => {
  test.use({ viewport: { width: 375, height: 812 } });

  // スマホにはキーボードが無いことが多く、保存だけの口（`⌘/Ctrl+S`）は出さない。
  // 保存は「絵解き」が挟む（ADR 0077）。押すと保存してから、パネルを全面に開く。
  test("描くと上の帯に「未保存」が出て、「絵解き」で保存してパネルを開く", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());
    await drawRectangle(page);

    const status = page.locator(".board-bar-status");
    await expect(status.getByText("未保存", { exact: true })).toBeVisible();
    await expect(status.getByText("⌘/Ctrl+S")).toBeHidden();
    await status.getByRole("button", { name: "絵解き", exact: true }).click();
    await expect(status.getByText("未保存", { exact: true })).toBeHidden();
    expect(mock.saveRequests).toHaveLength(1);
    // 注釈が複数なので、全面のパネルで一覧を開く。
    await expect(page.locator(".side-panel")).toBeVisible();
    await expect(page.getByRole("tab", { name: "注釈", exact: true })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  // `renderTopRightUI` はスマホでは何も返さない。Excalidraw のツールバーの行に
  // 島を詰めると、ツールバーと合わせて画面からはみ出し、「絵解き」が切れる。
  test("右上の島は Excalidraw の中に出さず、「絵解き」は画面に収まる", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.locator(".excalidraw .board-status")).toHaveCount(0);
    const save = await ekidokiButton(page).boundingBox();
    if (!save) throw new Error("「絵解き」が表示されていない");
    expect(save.x + save.width).toBeLessThanOrEqual(375);
  });

  // モバイル用 UI では `Footer` が描かれない。中身を上の帯に移していないと、
  // 自分のロールと作る先が画面から消える（ADR 0064 / 0065）。
  test("下の帯の中身（ロール・作成先）が上の帯に出ている", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.locator(".excalidraw .board-context")).toHaveCount(0);
    await expect(boardBar(page)).toContainText("オーナー");
    await expect(boardBar(page)).toContainText("acme/web");
  });

  test("パネルは畳んで始まり、上の帯から開くと全面に出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await expect(page.locator(".side-panel")).toBeHidden();
    // 右端の帯は出さない。画面の右端をキャンバスから取らない。
    await expect(page.locator(".side-panel-rail")).toHaveCount(0);
    // 注釈には手を打つ必要がある件数を添える（畳んだ帯と同じ）。
    const opener = boardBar(page).getByRole("button", { name: /^注釈、/ });
    await expect(opener).toContainText("未作成");

    await opener.click();
    const panel = page.locator(".side-panel");
    await expect(panel).toBeVisible();
    await expect(page.getByRole("tab", { name: "注釈", exact: true })).toBeFocused();
    const box = await panel.boundingBox();
    if (!box) throw new Error("パネルが表示されていない");
    expect(box.width).toBe(375);

    // 1 段目（「絵解き」と「未保存」）は覆わない（ADR 0021）。
    await ekidokiButton(page).click({ trial: true });

    // 閉じると、開いたボタンへ焦点が戻る。
    await page.getByRole("button", { name: "パネルを閉じる" }).click();
    await expect(panel).toBeHidden();
    await expect(opener).toBeFocused();
  });

  // 開閉は覚えない（#202）。スマホではいつも畳んだ状態で始める。
  test("開いたまま読み込み直しても、畳んで始まる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await openPanelTab(page, "注釈");
    await page.reload();
    await expect(boardBar(page)).toBeVisible();
    await expect(page.locator(".side-panel")).toBeHidden();
  });

  test("上の帯から注釈を開き、詳細の帯の「GitHub に作成する」まで届く", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await openPanelTab(page, "注釈");
    await interpret(annotationCard(page, "ログイン"));
    const detail = annotationDetail(page, "ログイン");
    const box = await detail.boundingBox();
    if (!box) throw new Error("詳細が表示されていない");
    expect(box.width).toBe(375);

    // 帯の文（取り消せないことと作る先）は、ボタンの間に挟まず全幅で出す。
    // 挟むと 1 文字ずつ縦に折れて読めない。
    const create = detail.getByRole("button", { name: "GitHub に作成する" });
    await expect(create).toBeEnabled();
    const notice = await detail.locator(".annotation-detail-band-text").boundingBox();
    if (!notice) throw new Error("帯の文が表示されていない");
    expect(notice.width).toBeGreaterThan(box.width / 2);

    await create.click();
    await expect(detail.getByText("3 件を作成しました。")).toBeVisible();
    expect(mock.createRequests).toHaveLength(1);
  });

  // 詳細の「キャンバスで見る」は、全面のパネルも閉じる。開いたままだと、寄せた
  // frame はパネルの下に隠れる。
  test("詳細の「キャンバスで見る」は、全面のパネルも閉じる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await openPanelTab(page, "注釈");
    await annotationCard(page, "ログイン").locator(".annotation-open").click();
    const detail = annotationDetail(page, "ログイン");

    await detail.getByRole("button", { name: "キャンバスで見る" }).click();
    await expect(detail).toHaveCount(0);
    await expect(page.locator(".side-panel")).toBeHidden();
    await expect(
      boardBar(page).getByRole("button", { name: /^注釈(、|$)/ }),
    ).toBeFocused();
  });

  // 置いた図を見せる。置けなかったら閉じない（理由を読ませる）。
  test("mermaid を置けたらパネルを閉じ、置けなかったら開いたまま", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    const paste = await openMermaidPaste(page);

    await paste.getByLabel("貼る mermaid").fill("flowchart TD\n  A -->");
    await paste.getByRole("button", { name: "キャンバスに置く" }).click();
    await expect(paste.locator(".error")).toBeVisible();
    await expect(page.locator(".side-panel")).toBeVisible();

    await paste.getByLabel("貼る mermaid").fill("flowchart TD\n  A --> B");
    await paste.getByRole("button", { name: "キャンバスに置く" }).click();
    await expect(page.locator(".side-panel")).toBeHidden();
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 通知は下に出す。上の帯（ボード名・未保存・「絵解き」と、ロール・作成先）は状態を
  // 出しているので覆わない。
  test("通知は上の帯に重ならない", async ({ page }) => {
    const notification = await showSaveFailure(page);
    await expectApart(notification, page.locator(".board-bar-status"));
    await expectApart(notification, boardBar(page));
  });

  test("メニューの etoki の項目を押せる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await backToList(page);
  });

  test("ボード・一覧・ダイアログ・作成先の選択に横スクロールが出ない", async ({
    page,
  }) => {
    const mock = baseMock();
    const unselected = unselectedBoard();
    mock.boards = [...mock.boards, summarize(unselected)];
    mock.details[unselected.id] = unselected;
    mock.annotations[unselected.id] = [];
    await openBoardWithMock(page, mock);
    expect(await horizontalScrollers(page)).toEqual([]);

    await openPanelTab(page, "注釈");
    await interpret(annotationCard(page, "ログイン"));
    await expect(annotationDetail(page, "ログイン")).toBeVisible();
    expect(await horizontalScrollers(page)).toEqual([]);

    await page.goto("/");
    await page.locator(".board-list").waitFor();
    expect(await horizontalScrollers(page)).toEqual([]);

    await newBoardDialog(page);
    expect(await horizontalScrollers(page)).toEqual([]);
    await page.keyboard.press("Escape");

    await page
      .locator(".board-list")
      .getByRole("button", { name: new RegExp(unselected.name) })
      .click();
    await picker(page).waitFor();
    expect(await horizontalScrollers(page)).toEqual([]);
  });

  test("ログインに横スクロールが出ない", async ({ page }) => {
    await installApi(page, authRequiredMock());
    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();
    expect(await horizontalScrollers(page)).toEqual([]);
  });
});

test.describe("スマホの横長", () => {
  test.use({ viewport: { width: 812, height: 375 } });

  test("「絵解き」と下の帯の中身が見えている", async ({ page }) => {
    await openBoardWithMock(page, baseMock(), BOARD_NAME);
    await ekidokiButton(page).click({ trial: true });
    await expect(boardBar(page)).toContainText("オーナー");
  });
});
