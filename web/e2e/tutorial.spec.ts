import { expect, test, type Locator, type Page } from "@playwright/test";

import { installApi, type ApiMock } from "./helpers/api";
import { chooseFromMenu, openBoardWithMock } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

/**
 * 使い方のダイアログ（#66、ADR 0081）。
 *
 * **勝手に始めない**（中核思想 3）。ボードが 0 件でも、押されるまで開かない。
 * 見返す口は一覧の上の帯とキャンバスのメニューの 2 つ。中身は録った動画と、
 * 同じことを書いた本文。
 */

/** ボードを 1 枚も持たない構成。初めて使う人はここから始まる。 */
function noBoardsMock(): ApiMock {
  const mock = baseMock();
  mock.boards = [];
  mock.details = {};
  mock.annotations = {};
  return mock;
}

/** 開いている使い方のダイアログ。 */
function tutorial(page: Page): Locator {
  return page.getByRole("dialog", { name: "etoki の使い方" });
}

/** 一覧の空状態に出す口。 */
function emptyStateOpener(page: Page): Locator {
  return page.getByRole("button", { name: "使い方を見る", exact: true });
}

/** 一覧の上の帯の口。ボードの有無に依らず出す。 */
function headerOpener(page: Page): Locator {
  return page
    .locator(".list-header")
    .getByRole("button", { name: "使い方", exact: true });
}

/** いま見えているページの動画。 */
function video(page: Page): Locator {
  return tutorial(page).locator("video");
}

async function openFromEmptyState(page: Page): Promise<Locator> {
  await installApi(page, noBoardsMock());
  await page.goto("/");
  await emptyStateOpener(page).click();
  await expect(tutorial(page)).toBeVisible();
  return tutorial(page);
}

test.describe("使い方", () => {
  test("ボードが 0 件なら空状態に口が出る。読み込んだだけでは開かない", async ({
    page,
  }) => {
    await installApi(page, noBoardsMock());
    await page.goto("/");

    await expect(emptyStateOpener(page)).toBeVisible();
    // 開くのは押したときだけ。勝手に始めると思想 3 に反する。
    await expect(tutorial(page)).toBeHidden();
  });

  test("ボードがあれば空状態の口は出ない。上の帯からは開ける", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await page.locator(".board-list").waitFor();

    await expect(emptyStateOpener(page)).toHaveCount(0);
    await headerOpener(page).click();
    await expect(tutorial(page)).toBeVisible();
  });

  test("ボードではキャンバスのメニューから開ける", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await chooseFromMenu(page, "etoki の使い方");

    await expect(tutorial(page)).toBeVisible();
  });

  test("次へ・戻るで 3 ページを行き来し、最後は閉じる", async ({ page }) => {
    const dialog = await openFromEmptyState(page);
    const heading = dialog.getByRole("heading", { level: 3 });

    await expect(dialog.getByText("1 / 3")).toBeVisible();
    // 1 ページ目に戻る先は無い。押せない「戻る」を置くと理由が要るので出さない。
    await expect(dialog.getByRole("button", { name: "戻る" })).toHaveCount(0);
    const first = await heading.innerText();

    await dialog.getByRole("button", { name: "次へ" }).click();
    await expect(dialog.getByText("2 / 3")).toBeVisible();
    await dialog.getByRole("button", { name: "戻る" }).click();
    await expect(heading).toHaveText(first);

    await dialog.getByRole("button", { name: "次へ" }).click();
    await dialog.getByRole("button", { name: "次へ" }).click();
    await expect(dialog.getByText("3 / 3")).toBeVisible();
    // 最後のページに「次へ」は無い。
    await expect(dialog.getByRole("button", { name: "次へ" })).toHaveCount(0);

    await dialog.getByRole("button", { name: "閉じる", exact: true }).click();
    await expect(tutorial(page)).toBeHidden();
  });

  // 閉じたら開いたボタンへ焦点を戻す。戻さないと body に落ち、キーボードの
  // 利用者は居場所を失う。
  test("Esc で閉じ、焦点は開いたボタンへ戻る", async ({ page }) => {
    await openFromEmptyState(page);

    await page.keyboard.press("Escape");

    await expect(tutorial(page)).toBeHidden();
    await expect(emptyStateOpener(page)).toBeFocused();
  });

  test("メニューから開いたら、閉じたあとメニューの口へ焦点を戻す", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await chooseFromMenu(page, "etoki の使い方");
    await expect(tutorial(page)).toBeVisible();

    await page.keyboard.press("Escape");

    // 押した項目はメニューごと消えているので、メニューを開くボタンへ戻す。
    await expect(page.locator('[data-testid="main-menu-trigger"]')).toBeFocused();
  });

  test("閉じて開き直すと 1 ページ目から始まる", async ({ page }) => {
    const dialog = await openFromEmptyState(page);
    await dialog.getByRole("button", { name: "次へ" }).click();
    await page.keyboard.press("Escape");

    await emptyStateOpener(page).click();

    await expect(dialog.getByText("1 / 3")).toBeVisible();
  });

  test.describe("動きを減らす設定", () => {
    test.use({ contextOptions: { reducedMotion: "reduce" } });

    // 自動では再生しない。再生は人が操作で選ぶ（ADR 0081）。
    test("動画は止まったままで、再生の操作が出る", async ({ page }) => {
      await openFromEmptyState(page);

      await expect(video(page)).toHaveAttribute("controls", "");
      // 読み込みを待ってから見る。読む前は再生していなくても止まって見える。
      await expect
        .poll(() => video(page).evaluate((v: HTMLVideoElement) => v.readyState))
        .toBeGreaterThanOrEqual(2);
      expect(await video(page).evaluate((v: HTMLVideoElement) => v.paused)).toBe(true);
    });
  });

  // 設定が無ければ、ミュートで繰り返し再生する。音は無いが、ミュートで
  // なければブラウザは自動再生を止める。
  test("設定が無ければ、ミュートで繰り返し再生する", async ({ page }) => {
    await openFromEmptyState(page);

    await expect
      .poll(() => video(page).evaluate((v: HTMLVideoElement) => !v.paused))
      .toBe(true);
    expect(await video(page).evaluate((v: HTMLVideoElement) => v.muted)).toBe(true);
    expect(await video(page).evaluate((v: HTMLVideoElement) => v.loop)).toBe(true);
  });

  // テーマは OS に追従する（ADR 0055）。片方だけで録ると、もう片方で浮く。
  for (const colorScheme of ["light", "dark"] as const) {
    test.describe(`テーマに合わせた動画（${colorScheme}）`, () => {
      test.use({ colorScheme });

      test("いまのテーマで録った動画を読む", async ({ page }) => {
        await openFromEmptyState(page);

        await expect(video(page)).toHaveAttribute(
          "src",
          new RegExp(`/${colorScheme}/page1\\.[^/]*mp4`),
        );
      });
    });
  }

  // **素材のコミット漏れはここでしか気づけない。** 動画は import しているので
  // 型検査は通るが、ファイルが無ければ Vite は 404 を返す。
  test("どのページの動画も配られる", async ({ page }) => {
    const dialog = await openFromEmptyState(page);

    for (let i = 0; i < 3; i++) {
      const src = await video(page).getAttribute("src");
      expect(src, `${i + 1} ページ目`).not.toBeNull();
      const res = await page.request.get(src as string);
      expect(res.status(), `${i + 1} ページ目`).toBe(200);
      expect(res.headers()["content-type"], `${i + 1} ページ目`).toContain("video/mp4");
      if (i < 2) await dialog.getByRole("button", { name: "次へ" }).click();
    }
  });

  // **LLM も GitHub も設定していない構成でも見られる**（ADR 0081）。ブレストだけ
  // 先にやる使い方を潰さない（README）。ボードを作らない形にしたのはこのため。
  test("LLM と GitHub が未設定でも、全ページを見られる", async ({ page }) => {
    const mock = noBoardsMock();
    mock.capabilities = {
      status: 200,
      body: {
        interpretation: false,
        diagramDraft: false,
        creation: false,
        sharing: false,
        mcpConnections: false,
      },
    };
    await installApi(page, mock);
    await page.goto("/");
    await emptyStateOpener(page).click();
    const dialog = tutorial(page);

    await dialog.getByRole("button", { name: "次へ" }).click();
    await dialog.getByRole("button", { name: "次へ" }).click();

    await expect(dialog.getByText("3 / 3")).toBeVisible();
    await expect(video(page)).toBeVisible();
  });

  // 動画の中身は読み上げに届かない。同じことを本文に書き、動画から結ぶ。
  test("動画は本文の説明を指している", async ({ page }) => {
    const dialog = await openFromEmptyState(page);

    for (let i = 0; i < 3; i++) {
      const described = await video(page).getAttribute("aria-describedby");
      expect(described, `${i + 1} ページ目`).not.toBeNull();
      const target = page.locator(`[id="${described}"]`);
      await expect(target, `${i + 1} ページ目`).toHaveCount(1);
      await expect(target, `${i + 1} ページ目`).toBeVisible();
      await expect(target, `${i + 1} ページ目`).not.toHaveText("");
      if (i < 2) await dialog.getByRole("button", { name: "次へ" }).click();
    }
  });
});
