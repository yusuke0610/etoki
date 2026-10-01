import { expect, test, type Page } from "@playwright/test";

import { installApi, type ApiMock } from "./helpers/api";
import { drawRectangle, openBoard, waitForBoard } from "./helpers/board";
import { BOARD_ID, BOARD_NAME, baseMock } from "./helpers/fixtures";

type SavedElement = {
  type: string;
  isDeleted?: boolean;
  groupIds?: string[];
  customData?: Record<string, unknown>;
};

/** 保存して、送られたシーンの要素を返す。消した要素は数えない。 */
async function saveAndRead(page: Page, mock: ApiMock): Promise<SavedElement[]> {
  const before = mock.details[BOARD_ID]?.updatedAt;
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect.poll(() => mock.details[BOARD_ID]?.updatedAt).not.toBe(before);
  const scene = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
    elements: SavedElement[];
  };
  return scene.elements.filter((el) => !el.isDeleted);
}

async function placeTable(page: Page): Promise<void> {
  await page.getByRole("button", { name: "表", exact: true }).click();
  await expect(page.getByText("未保存", { exact: true })).toBeVisible();
}

// 表は矩形 9 枚を group にまとめて置く（ADR 0068）。行・列として読ませる
// 実装は無いので、確かめるのは「置ける・まとまって動く・戻せる・残る」まで。
test.describe("表", () => {
  test("押すと矩形が 9 枚、1 つの group で置かれる", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await placeTable(page);
    const elements = await saveAndRead(page, mock);
    const cells = elements.filter((el) => el.type === "rectangle");

    expect(cells).toHaveLength(9);
    expect(new Set(cells.map((el) => el.groupIds?.join(","))).size).toBe(1);
    // 手で描いた矩形と区別しない。印も frame も付けない。
    expect(cells.some((el) => el.customData !== undefined)).toBe(false);
    expect(elements.filter((el) => el.type === "frame")).toHaveLength(3);
  });

  // 置いた直後は group ごと選ばれている。9 枚とも消えるなら、そう選ばれている。
  test("置いた直後に消すと、9 枚とも消える", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await placeTable(page);
    // フォーカスは「表」ボタンに残っていて、Delete キーはキャンバスに届かない。
    // 選択に対する削除は Excalidraw 自身のボタンで行う。
    await page
      .locator(".excalidraw")
      .getByRole("button", { name: "削除", exact: true })
      .click();

    const elements = await saveAndRead(page, mock);
    expect(elements.filter((el) => el.type === "rectangle")).toHaveLength(0);
  });

  // 1 回の「元に戻す」で全部消える（#144）。9 回押さないと戻らないと、置き間違いを
  // 戻す手段にならない。描いた矩形は残る。
  test("元に戻すと、置いた表だけが 1 手で消える", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);
    await placeTable(page);
    await page.getByRole("button", { name: "元に戻す" }).click();

    const elements = await saveAndRead(page, mock);
    expect(elements.filter((el) => el.type === "rectangle")).toHaveLength(1);
  });

  test("保存して開き直しても 9 枚残る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await placeTable(page);
    await saveAndRead(page, mock);

    // リロードしても URL が開いていたボードを復元する（ADR 0059）。一覧からは
    // 開き直さない。
    await page.reload();
    await waitForBoard(page, BOARD_NAME);
    // 開き直して読めたことを、描き足して保存し直したシーンで確かめる。
    await drawRectangle(page);
    const elements = await saveAndRead(page, mock);
    expect(elements.filter((el) => el.type === "rectangle")).toHaveLength(10);
  });
});
