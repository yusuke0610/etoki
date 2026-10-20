import { expect, test, type Page } from "@playwright/test";

import { openAnnotationDetail, openBoardWithMock, saveScene } from "./helpers/board";
import { ANNOTATION_IDS, BOARD_ID, baseMock } from "./helpers/fixtures";
import type { ApiMock } from "./helpers/api";

/**
 * 囲みの名前（#249、ADR 0079）。注釈の詳細の見出しの名前を押して、frame の `name` を書き換える（#305）。
 *
 * **守りたいのは、名前がシーンの frame に載ること。** 表示だけ変えて `name` に
 * 書かないと、キャンバスのラベルとも、解釈の入力（`content_hash`）とも食い違う。
 */
test.describe("囲みの名前", () => {
  /** 最後に保存したシーンの、注釈「ログイン」の frame の名前。 */
  function savedName(mock: ApiMock): string | null | undefined {
    const scene = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements?: { id: string; name?: string | null }[];
    };
    return scene.elements?.find((el) => el.id === ANNOTATION_IDS.uncreated)?.name;
  }

  async function rename(page: Page, name: string): Promise<void> {
    const detail = await openAnnotationDetail(page, "ログイン");
    await detail.getByRole("button", { name: "ログイン" }).click();
    await detail.getByLabel("名前").fill(name);
    await detail.getByLabel("名前").press("Enter");
    // 確定すると見出しの名前が新しい名前になる（保存を待たない）。ダイアログの
    // 名前は見出しの名前から引くので、同じ面が新しい名前で見つかる。
    await expect(
      page.getByRole("dialog", { name: `絵解き ${name}`, exact: true }),
    ).toBeVisible();
  }

  test("名前を確定すると未保存になり、保存したシーンの frame に載る", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());

    await rename(page, "ログインの入口");
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    await saveScene(page);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    expect(savedName(mock)).toBe("ログインの入口");
  });

  // 欄から離れたときにも確定する。そのまま「絵解く」を押した人の名前が、
  // 保存にも解釈にも入らないと、押した結果が欄の表示と食い違う。
  test("名前を変えてそのまま絵解くと、新しい名前で保存してから読む", async ({ page }) => {
    const mock = await openBoardWithMock(page, baseMock());

    const detail = await openAnnotationDetail(page, "ログイン");
    await detail.getByRole("button", { name: "ログイン" }).click();
    await detail.getByLabel("名前").fill("ログインの入口");
    await detail.getByRole("button", { name: "絵解く" }).click();

    // 押した処理が名前を確定するので、面の名前は新しい名前になっている。
    const renamed = page.getByRole("dialog", {
      name: "絵解き ログインの入口",
      exact: true,
    });
    await expect(
      renamed.getByRole("button", { name: "GitHub に作成する" }),
    ).toBeVisible();
    expect(savedName(mock)).toBe("ログインの入口");
    expect(mock.interpretRequests).toHaveLength(1);
  });
});
