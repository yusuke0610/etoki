import { expect, test } from "@playwright/test";

import { openBoardWithMock, openPanelTab, openUsageGuide } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

/**
 * 使い方のダイアログ（`UsageGuideDialog`、ADR 0094）。
 *
 * **守りたいのは、勝手に開かないことと、閉じたあとに居場所を失わないこと。**
 * 本文が表と食い違わないことは単体テスト（`UsageGuideDialog.test.tsx`）が見る。
 */
test.describe("使い方のダイアログ", () => {
  // 切れると: ボードを開くたびに説明が割り込み、描き始めを止める（中核思想 3）。
  test("ボードを開いただけでは開かない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await expect(page.getByRole("dialog", { name: "etoki の使い方" })).toHaveCount(0);
  });

  // 切れると: 開いた項目はメニューごと消えるので、焦点が body に落ちる。
  test("メニューから開いて閉じると、焦点がメニューの口へ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    const dialog = await openUsageGuide(page);
    // 切れると: 焦点が最初の押せるもの（いちばん下の「閉じる」）へ行き、そこまで
    // スクロールして心得が画面の外に出る（小さい画面で起きる）。
    await expect(
      dialog.getByRole("heading", { name: "etoki の使い方", level: 2 }),
    ).toBeFocused();
    await expect(dialog.getByRole("heading", { name: "はじめに" })).toBeInViewport();
    await dialog.getByRole("button", { name: "閉じる", exact: true }).click();

    await expect(dialog).toBeHidden();
    await expect(page.locator('[data-testid="main-menu-trigger"]')).toBeFocused();
  });

  // 切れると: パネルの案内から開いた人が、閉じたあとメニューの口に飛ばされ、
  // 読んでいた場所を見失う。開いた口へ戻す。
  test("パネルの案内から開いて Esc で閉じると、焦点が案内のボタンへ戻る", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());

    const panel = await openPanelTab(page, "絵解いた");
    const opener = panel.getByRole("button", { name: "使い方を見る", exact: true });
    await opener.click();

    const dialog = page.getByRole("dialog", { name: "etoki の使い方", exact: true });
    await expect(dialog).toBeVisible();
    await page.keyboard.press("Escape");

    await expect(dialog).toBeHidden();
    await expect(opener).toBeFocused();
  });
});
