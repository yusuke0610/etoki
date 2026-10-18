import { expect, test, type Locator, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import {
  authRequiredMock,
  baseMock,
  CONSENT_PATH,
  withConnections,
} from "./helpers/fixtures";

/**
 * ロゴ（ADR 0084）が、置いた 3 つの画面で、テーマの色に従って描けていること。
 *
 * **回帰止め。ここでしか気づけない。** 名前は `aria-label` が持つので、字が
 * 描けていなくても、ダークの地に沈んでいても、ほかの検査はどれも落ちない。
 * 字は置いた場所の文字色、付箋は `--brand-note` を読む。色を SVG に直書きすると
 * ダークで字が沈み、変数が消えると付箋は黒くなる。
 *
 * **値ではなく、同じ変数を外で読んだ色と比べる**（a11y.spec の「キャンバスの
 * 中の etoki の部品」と同じ）。値を書き写すと、配色を変えた日にここだけが古くなる。
 *
 * **字が描けたかは、字の外接矩形で見る。** `d` が壊れても `<svg>` の箱は
 * 残るので、見えているかだけでは通る。幅で字が欠けていないことを、画面の上で
 * `<svg>` の箱に収まっていることで、字がずれて表示範囲の外へ出ていないことを
 * 見る。外接矩形は、範囲の外にあって描かれない図形にも返る。
 */
const LETTERS_WIDTH = 212.4;
/** 画面の上の矩形を比べるときの許し（px）。`<svg>` の幅は整数に丸めてある。 */
const SLACK = 1;

async function expectWordmark(page: Page, container: Locator, colorScheme: string) {
  await expect(page.locator("html")).toHaveAttribute("data-theme", colorScheme);
  const wordmark = container.getByRole("img", { name: "etoki" });
  await expect(wordmark).toBeVisible();

  const letters = wordmark.locator("path");
  expect(
    await letters.evaluate((el: SVGGraphicsElement) => el.getBBox().width),
  ).toBeCloseTo(LETTERS_WIDTH, 0);
  const drawn = await letters.boundingBox();
  if (!drawn) throw new Error("字が表示されていない");
  const frame = await wordmark.boundingBox();
  if (!frame) throw new Error("ロゴが表示されていない");
  expect(drawn.x).toBeGreaterThanOrEqual(frame.x - SLACK);
  expect(drawn.y).toBeGreaterThanOrEqual(frame.y - SLACK);
  expect(drawn.x + drawn.width).toBeLessThanOrEqual(frame.x + frame.width + SLACK);
  expect(drawn.y + drawn.height).toBeLessThanOrEqual(frame.y + frame.height + SLACK);
  expect(await fillOf(letters)).toBe(await colorOfVariable(page, "--color-text"));
  expect(await fillOf(wordmark.locator("rect"))).toBe(
    await colorOfVariable(page, "--brand-note"),
  );
}

function fillOf(locator: Locator): Promise<string> {
  return locator.evaluate((el) => getComputedStyle(el).fill);
}

/** body 直下で、その変数を文字色に使ったときの色。 */
function colorOfVariable(page: Page, variable: string): Promise<string> {
  return page.evaluate((name) => {
    const probe = document.createElement("span");
    probe.style.color = `var(${name})`;
    document.body.appendChild(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, variable);
}

for (const colorScheme of ["light", "dark"] as const) {
  test.describe(`ロゴ（${colorScheme}）`, () => {
    test.use({ colorScheme });

    test("ログインの画面の見出しに描ける", async ({ page }) => {
      await installApi(page, authRequiredMock());
      await page.goto("/");
      await page.getByRole("button", { name: "GitHub でログイン" }).waitFor();

      const heading = page.getByRole("heading", { level: 1 });
      await expect(heading).toHaveAccessibleName("etoki");
      await expectWordmark(page, heading, colorScheme);
    });

    test("ボードの一覧の上の帯の見出しに描ける", async ({ page }) => {
      await installApi(page, baseMock());
      await page.goto("/");
      await page.locator(".board-list").waitFor();

      const heading = page.getByRole("heading", { level: 1 });
      await expect(heading).toHaveAccessibleName("etoki");
      await expectWordmark(page, heading, colorScheme);
    });

    test("MCP の同意の画面に描ける", async ({ page }) => {
      await installApi(page, withConnections(baseMock()));
      await page.goto(CONSENT_PATH);
      await page.getByRole("button", { name: "許可する" }).waitFor();

      await expectWordmark(page, page.locator(".consent-brand"), colorScheme);
    });
  });
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

/**
 * **回帰止め。** ファビコンは描けなくてもタブが既定の絵に戻るだけで、何も
 * 落ちない。見る幅は SVG の実寸（`width` 属性）。
 */
test("ファビコンを自前の配信元から描ける", async ({ page, baseURL }) => {
  await installApi(page, baseMock());
  await page.goto("/");
  await page.locator(".board-list").waitFor();

  const href = await page
    .locator('link[rel="icon"]')
    .evaluate((el: HTMLLinkElement) => el.href);
  expect(new URL(href).origin).toBe(new URL(baseURL ?? "").origin);
  expect(await faviconWidth(page)).toBe(100);
});
