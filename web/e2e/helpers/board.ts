import { expect, type Locator, type Page } from "@playwright/test";

/**
 * サイドバーからボードを開き、キャンバスと注釈パネルが出るまで待つ。
 *
 * Excalidraw のマウントはキャンバスの描画を伴い、注釈パネルより遅れる。
 * ここで揃うまで待たないと、後続の操作がマウント途中の DOM に当たる。
 */
export async function openBoard(page: Page, name: string): Promise<void> {
  await page.locator(".board-list").getByRole("button", { name }).click();
  await expect(page.getByRole("heading", { name, level: 1 })).toBeVisible();
  await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "注釈" })).toBeVisible();
}

/** 注釈 1 つぶんのカード。名前で絞り込む。 */
export function annotationCard(page: Page, name: string): Locator {
  return page.locator("li.annotation").filter({ hasText: name });
}
