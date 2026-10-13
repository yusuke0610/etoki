import { expect, test, type Page } from "@playwright/test";

import { installApi, type ApiMock } from "./helpers/api";
import {
  drawRectangle,
  openAnnotationDetail,
  openBoard,
  openMermaidPaste,
  openPanelTab,
  saveScene,
} from "./helpers/board";
import { BOARD_ID, baseMock, mixedFramesMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

type SavedElement = {
  type: string;
  isDeleted?: boolean;
  customData?: Record<string, unknown>;
};

/** 保存して、送られたシーンの要素を返す。消した要素は数えない。 */
async function saveAndRead(page: Page, mock: ApiMock): Promise<SavedElement[]> {
  const before = mock.details[BOARD_ID]?.updatedAt;
  await saveScene(page);
  await expect.poll(() => mock.details[BOARD_ID]?.updatedAt).not.toBe(before);
  const scene = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
    elements: SavedElement[];
  };
  return scene.elements.filter((el) => !el.isDeleted);
}

function undo(page: Page) {
  return page.getByRole("button", { name: "元に戻す" }).click();
}

// etoki がキャンバスに加えた変更（図のドラフト・注釈の付け外し）は、
// 人の操作として「元に戻す」で 1 手ずつ戻る（#144）。**戻らないだけでなく、
// 押すと直前に自分が描いたものが消えていた。** ブレスト中の置き間違いを戻す
// 手段が無いと手が止まる（中核思想 1）。
test.describe("元に戻す", () => {
  // いちばん失いたくないものを直接見る。「置いたものが消える」だけでは、描いた
  // 矩形も一緒に消える実装でも通る。
  test("図のドラフトを置いてから戻すと、置いたものが消える", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);
    await openPanelTab(page, "etoki AI");
    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();
    await expect(page.locator(".diagram-mermaid")).toContainText("flowchart TD");
    await page.getByRole("button", { name: "キャンバスに置く" }).click();
    // 変換は非同期なので、置けたことを保存したシーンで確かめてから戻す。置かれる
    // 前に戻すと、置く前の操作を戻すことになる。
    await expect
      .poll(async () => (await saveAndRead(page, mock)).some((el) => el.type === "arrow"))
      .toBe(true);
    await undo(page);

    const elements = await saveAndRead(page, mock);
    expect(elements.filter((el) => el.type === "arrow")).toHaveLength(0);
    expect(elements.filter((el) => el.type === "rectangle")).toHaveLength(1);
    expect(elements.filter((el) => el.type === "frame")).toHaveLength(3);
  });

  // 貼った mermaid も置き方は図のドラフトと同じ（ADR 0062）。貼り間違いを
  // 1 手で戻せないと、属性の断片まで 1 つずつ消すことになる。
  test("貼った mermaid を置いてから戻すと、置いたものが消える", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);
    await openMermaidPaste(page);
    await page.getByLabel("貼る mermaid").fill("flowchart TD\n  A[注文] --> B[出荷]");
    await page.getByRole("button", { name: "キャンバスに置く" }).click();
    await expect
      .poll(async () => (await saveAndRead(page, mock)).some((el) => el.type === "arrow"))
      .toBe(true);
    await undo(page);

    const elements = await saveAndRead(page, mock);
    expect(elements.filter((el) => el.type === "arrow")).toHaveLength(0);
    expect(elements.filter((el) => el.type === "rectangle")).toHaveLength(1);
    expect(elements.filter((el) => el.type === "frame")).toHaveLength(3);
  });

  // 誤って外すと、粒度や種別を選び直すことになる。
  test("注釈を外してから戻すと、注釈に戻る", async ({ page }) => {
    const mock = await installApi(page, mixedFramesMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.locator(".annotation-overlay-frame")).toHaveCount(2);
    // 「キャンバスで見る」で詳細を閉じてフレームを選び、パネルの下から外す（#201）。
    const detail = await openAnnotationDetail(page, "ログイン");
    await detail.getByRole("button", { name: "キャンバスで見る" }).click();
    await page.getByRole("button", { name: /の注釈を外す/ }).click();
    await expect(page.locator(".annotation-overlay-frame")).toHaveCount(1);

    await undo(page);
    await expect(page.locator(".annotation-overlay-frame")).toHaveCount(2);

    const elements = await saveAndRead(page, mock);
    const marked = elements.filter((el) => el.type === "frame" && el.customData?.etoki);
    expect(marked).toHaveLength(2);
  });

  // 戻したら「未保存」も追いつく。署名を取り直さないと、保存した状態に戻した
  // のに未保存のまま残り、離れるときに理由の無い確認が出る。
  //
  // 置くのは新しい要素だけの変更にする。既存の要素を書き換える変更（注釈の
  // 付け外し）は、戻しても version が上がって未保存が残る（`web/CLAUDE.md`）。
  test("保存した直後に図のドラフトを置いて戻すと、未保存が消える", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await openPanelTab(page, "etoki AI");
    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();
    await expect(page.locator(".diagram-mermaid")).toContainText("flowchart TD");
    await page.getByRole("button", { name: "キャンバスに置く" }).click();
    // 変換は非同期なので、置けたこと（未保存になったこと）を待ってから戻す。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    await undo(page);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });
});
