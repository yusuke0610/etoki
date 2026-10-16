import { expect, test, type Locator, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import {
  authRequiredMock,
  baseMock,
  CONSENT_PATH,
  withConnections,
} from "./helpers/fixtures";

/**
 * ロゴ（ADR 0082）が、置いた 3 つの画面とブラウザのタブで描けていること。
 *
 * **回帰止め。ここでしか気づけない。** SVG が XML として壊れていても、`<img>` は
 * 失敗を出さずに空の箱になるだけで、名前は文字が持っているので他の検査は
 * どれも落ちない。ファビコンも同じで、描けなくてもタブが既定の絵に戻るだけ。
 *
 * **見る幅は SVG の実寸（`width` 属性）。** 0 より大きいかだけを見ると、置く絵を
 * 取り違えても通る（ログインの画面に印を置く、など）。
 */
const LOGO_WIDTH = 880;
const MARK_WIDTH = 100;

async function expectDrawn(img: Locator, width: number) {
  await expect
    .poll(() =>
      img.evaluate((el: HTMLImageElement) => (el.complete ? el.naturalWidth : 0)),
    )
    .toBe(width);
}

/** `<link rel="icon">` の指す先を画像として読んだときの幅。読めなければ 0。 */
function faviconWidth(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (link === null) return 0;
    const img = new Image();
    img.src = link.href;
    try {
      await img.decode();
    } catch {
      return 0;
    }
    return img.naturalWidth;
  });
}

test.describe("ロゴ", () => {
  test("ログインの画面に描ける", async ({ page }) => {
    await installApi(page, authRequiredMock());
    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();

    await expectDrawn(page.locator(".login-logo"), LOGO_WIDTH);
    // 絵は飾りで、名前は見出しの文字が持つ。
    await expect(page.getByRole("heading", { level: 1 })).toHaveAccessibleName("etoki");
  });

  test("ボードの一覧の上の帯に描ける", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await page.locator(".board-list").waitFor();

    await expectDrawn(page.locator(".brand-logo"), MARK_WIDTH);
    await expect(page.getByRole("heading", { level: 1 })).toHaveAccessibleName("etoki");
  });

  test("MCP の同意の画面に描ける", async ({ page }) => {
    await installApi(page, withConnections(baseMock()));
    await page.goto(CONSENT_PATH);
    await page.getByRole("button", { name: "許可する" }).waitFor();

    await expectDrawn(page.locator(".consent-logo"), MARK_WIDTH);
  });

  test("ファビコンを自前の配信元から描ける", async ({ page, baseURL }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await page.locator(".board-list").waitFor();

    const href = await page
      .locator('link[rel="icon"]')
      .evaluate((el: HTMLLinkElement) => el.href);
    expect(new URL(href).origin).toBe(new URL(baseURL ?? "").origin);
    expect(await faviconWidth(page)).toBe(MARK_WIDTH);
  });
});
