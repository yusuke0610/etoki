import { expect, test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

/**
 * 配色の持ち方（ADR 0049）。
 *
 * **ここで守るのは 3 つ。** OS の設定に従うこと、キャンバスのメニューで
 * 切り替えるとパネルも一緒に変わること、切り替えが保存すべき変更にならないこと。
 * 配色そのものの読みやすさは `a11y.spec.ts` の axe が見る。
 */

const BOARD_NAME = "認証まわりのブレスト";
const STORAGE_KEY = "etoki.theme";

async function toggleThemeFromCanvasMenu(page: Page): Promise<void> {
  await page.locator('[data-testid="main-menu-trigger"]').click();
  await page.locator('[data-testid="toggle-dark-mode"]').click();
}

test.describe("配色", () => {
  test("OS がダークなら、パネルもキャンバスもダークで開く", async ({ page }) => {
    await page.emulateMedia({ colorScheme: "dark" });
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    // キャンバスだけ明るい、パネルだけ暗い、を分けて検知する。
    await expect(page.locator(".excalidraw").first()).toHaveClass(/theme--dark/);
  });

  // **一番失いたくないのは「未保存」の信用。** テーマが署名に混ざると、
  // 切り替えただけで未保存になり、離れるたびに確認が出る（ADR 0021）。
  test("キャンバスのメニューで切り替えるとパネルも変わり、未保存にはならない", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

    await toggleThemeFromCanvasMenu(page);

    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator(".excalidraw").first()).toHaveClass(/theme--dark/);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  test("選んだテーマは読み込み直しても残り、OS と同じに戻すと覚えた選択は消える", async ({
    page,
  }) => {
    await page.emulateMedia({ colorScheme: "light" });
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await toggleThemeFromCanvasMenu(page);
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

    // OS と同じテーマに戻したら、以後は OS に従う。覚えたままだと、
    // 一度切り替えた人は OS の設定を変えても付いてこなくなる。
    await openBoard(page, BOARD_NAME);
    await toggleThemeFromCanvasMenu(page);
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(
      await page.evaluate((key) => localStorage.getItem(key), STORAGE_KEY),
    ).toBeNull();

    await page.emulateMedia({ colorScheme: "dark" });
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  });
});
