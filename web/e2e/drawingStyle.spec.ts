import { expect, test } from "@playwright/test";

import { drawRectangle, openBoardWithMock, saveScene } from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

/** Excalidraw の書体の番号。`FONT_FAMILY` は E2E から import できないので写す。 */
const NUNITO = 6;

type SavedElement = {
  type: string;
  roughness?: number;
  fontFamily?: number;
  text?: string;
};

test.describe("線と書体（ADR 0095）", () => {
  // 開くたびに渡すツールの既定が効いているか。保存したシーンには載らない値
  // なので、渡し忘れると手書き風のまま描き始めて、ほかでは気づけない。
  test("人が描いた図形と文字は、揺れない線と清書調の書体で保存される", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await drawRectangle(page);

    const canvas = page.locator(".excalidraw canvas").first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error("キャンバスが表示されていない");
    await page.keyboard.press("t");
    await page.mouse.click(box.x + box.width * 0.2, box.y + box.height * 0.2);
    await page.keyboard.type("メモ");
    await page.keyboard.press("Escape");

    const saved = page.waitForRequest(
      (req) =>
        req.method() === "PUT" &&
        new URL(req.url()).pathname === `/api/boards/${BOARD_ID}/scene`,
    );
    await saveScene(page);
    await saved;
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    const scene = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements: SavedElement[];
    };
    const rectangle = scene.elements.find((el) => el.type === "rectangle");
    const text = scene.elements.find((el) => el.type === "text" && el.text === "メモ");

    expect(rectangle?.roughness).toBe(0);
    expect(text?.fontFamily).toBe(NUNITO);
  });
});
