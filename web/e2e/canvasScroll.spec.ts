import { expect, test, type Page } from "@playwright/test";

import { drawRectangle, openBoardWithMock, saveScene } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";
import type { ApiMock } from "./helpers/api";

/**
 * キャンバスの移動を楽にする（#250）。何も選んでいないときの矢印キーと、
 * ドラッグ中に端へ寄せたときのスクロール。
 *
 * **スクロールしたかは、注釈の枠（`AnnotationOverlay`）の位置で見る。** 枠は
 * キャンバスのスクロールに合わせて置き直す DOM で、シーンの座標は動かない。
 */

/** 最初の注釈の枠の左上（画面の座標）。 */
async function overlayOrigin(page: Page): Promise<{ x: number; y: number }> {
  const box = await page.locator(".annotation-overlay-frame").first().boundingBox();
  if (!box) throw new Error("注釈の枠が表示されていない");
  return { x: box.x, y: box.y };
}

/** キャンバスの枠（`.canvas-frame`）。 */
async function canvasBox(page: Page) {
  const box = await page.locator(".canvas-frame").boundingBox();
  if (!box) throw new Error("キャンバスが表示されていない");
  return box;
}

/** 何も無いところを押して、選択を外し、焦点をキャンバスに置く。 */
async function focusEmptyCanvas(page: Page): Promise<void> {
  const box = await canvasBox(page);
  await page.mouse.click(box.x + box.width * 0.5, box.y + box.height * 0.85);
}

/** 描いた直後の矩形（選ばれている）を掴み、`edgeX` まで運んで止め、離す。 */
async function dragRectangleTo(page: Page, edgeX: number) {
  // 描いた直後の矩形は選ばれているので、その中を掴めばドラッグになる。
  const box = await canvasBox(page);
  const grab = { x: box.x + box.width * 0.475, y: box.y + box.height * 0.525 };
  const overlayBefore = await overlayOrigin(page);

  await page.mouse.move(grab.x, grab.y);
  await page.mouse.down();
  await page.mouse.move(edgeX, grab.y, { steps: 10 });
  // ポインターを止めたまま待つ。止めているあいだにも流れ続けること。
  await page.waitForTimeout(600);
  await page.mouse.up();

  const overlayAfter = await overlayOrigin(page);
  return { moved: edgeX - grab.x, overlayBefore, overlayAfter };
}

test.describe("矢印キーでのスクロール", () => {
  test("何も選んでいなければ、矢印キーでキャンバスが動き、Shift で大きく動く", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());
    await focusEmptyCanvas(page);
    const before = await overlayOrigin(page);

    // 右へ動かすと、中身は左へ流れる。
    await page.keyboard.press("ArrowRight");
    await expect
      .poll(async () => (await overlayOrigin(page)).x)
      .toBeCloseTo(before.x - 100, 0);

    const height = (await canvasBox(page)).height;
    await page.keyboard.press("Shift+ArrowDown");
    await expect
      .poll(async () => (await overlayOrigin(page)).y)
      .toBeCloseTo(before.y - height * 0.8, 0);
  });

  test("選んだ要素があれば、矢印キーは要素を動かし、キャンバスは動かさない", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());
    // 描いた直後の矩形は選ばれている。保存しても選んだまま残る。
    await drawRectangle(page);
    await saveScene(page);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    const before = await overlayOrigin(page);

    await page.keyboard.press("ArrowRight");
    await page.keyboard.press("ArrowRight");

    // 要素を動かした結果は未保存に出る（保存してから押したので、描いたぶんでは
    // ない）。キャンバスは動いていない。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    expect(await overlayOrigin(page)).toEqual(before);
  });

  test("右のパネルのタブに焦点があれば、矢印キーはタブを移り、キャンバスは動かさない", async ({
    page,
  }) => {
    await openBoardWithMock(page, baseMock());
    const before = await overlayOrigin(page);

    await page.getByRole("tab", { name: /^絵解いた/ }).focus();
    await page.keyboard.press("ArrowRight");

    await expect(page.getByRole("tab", { name: "etoki AI" })).toBeFocused();
    expect(await overlayOrigin(page)).toEqual(before);
  });

  // キーボードでボードを見て回る手段がほかに無いので、読むだけのロールでも
  // 使えるようにしてある。
  test("読むだけのロールでも、矢印キーでキャンバスが動く", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), role: "viewer" };
    mock.boards = mock.boards.map((b) => ({ ...b, role: "viewer" }));
    await openBoardWithMock(page, mock);
    await focusEmptyCanvas(page);
    const before = await overlayOrigin(page);

    await page.keyboard.press("ArrowLeft");

    await expect
      .poll(async () => (await overlayOrigin(page)).x)
      .toBeCloseTo(before.x + 100, 0);
  });
});

test.describe("ドラッグ中の端でのスクロール", () => {
  /** 保存したシーンの、描いた矩形の左上（シーンの座標）。 */
  function savedRectangle(mock: ApiMock): { x: number; y: number } {
    const scene = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements?: { type: string; x: number; y: number; isDeleted?: boolean }[];
    };
    const rect = scene.elements?.find((el) => el.type === "rectangle" && !el.isDeleted);
    if (!rect) throw new Error("矩形が保存されていない");
    return { x: rect.x, y: rect.y };
  }

  test("要素を端へ運んで止めると、キャンバスが流れ、要素がポインターに付いてくる", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());
    await drawRectangle(page);
    await saveScene(page);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    const startScene = savedRectangle(mock);

    const box = await canvasBox(page);
    const { moved, overlayBefore, overlayAfter } = await dragRectangleTo(
      page,
      box.x + box.width - 4,
    );
    const scrolled = overlayBefore.x - overlayAfter.x;
    expect(scrolled).toBeGreaterThan(100);
    expect(overlayAfter.y).toBeCloseTo(overlayBefore.y, 0);

    // 離したあとは流れない。
    await page.waitForTimeout(300);
    expect(await overlayOrigin(page)).toEqual(overlayAfter);

    // 要素は、ポインターが動いたぶんと、キャンバスが流れたぶんだけ動いている
    // （等倍なので画面の px とシーンの単位が同じ）。流れたぶんが抜けていれば、
    // 要素はポインターから離れてシーンに置き去りになっている。
    await saveScene(page);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    const endScene = savedRectangle(mock);
    expect(endScene.x - startScene.x).toBeCloseTo(moved + scrolled, -1);
    expect(endScene.y).toBeCloseTo(startScene.y, 0);
  });

  // 流すのは要素を運んでいるときだけ。何も無いところから押して運ぶのは
  // 範囲選択で、端で流すと選ぶ範囲が勝手に広がる。
  test("何も無いところから押して端へ運んでも（範囲選択）、流れない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    const box = await canvasBox(page);
    const before = await overlayOrigin(page);

    await page.mouse.move(box.x + box.width * 0.5, box.y + box.height * 0.85);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width - 4, box.y + box.height * 0.85, {
      steps: 10,
    });
    await page.waitForTimeout(400);
    expect(await overlayOrigin(page)).toEqual(before);
    await page.mouse.up();
  });
});

// 重ねる置き方（ADR 0075）。右のパネルはキャンバスの上に浮いている。
test.describe("重ねたパネルがあるときの端", () => {
  test.use({ viewport: { width: 1024, height: 768 } });

  // パネルの下に隠れたところを端にすると、パネルに重なるまで運ばないと流れない。
  test("パネルの手前まで運べば流れる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await expect(page.locator(".board")).toHaveAttribute("data-layout", "overlay");
    const panel = await page.locator(".side-panel").boundingBox();
    if (!panel) throw new Error("右のパネルが開いていない");
    await drawRectangle(page);

    const { overlayBefore, overlayAfter } = await dragRectangleTo(page, panel.x - 4);
    expect(overlayBefore.x - overlayAfter.x).toBeGreaterThan(100);
  });
});
