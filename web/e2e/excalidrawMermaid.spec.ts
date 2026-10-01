import { expect, test } from "@playwright/test";

import { openBoardWithMock, pasteOnCanvas } from "./helpers/board";
import { baseMock } from "./helpers/fixtures";

/**
 * Excalidraw 自身が持つ mermaid の入口が閉じていること（ADR 0067、#187）。
 *
 * どれもライブラリの UI で、etoki はそこに props か CSS で手を入れている。
 * **ライブラリを上げると黙って戻る**ので、出ていないことをここで見る。
 * 開いたら etoki の守り（受け付ける種類・`secure`・大きさの上限・画像を
 * 置かない）を通らない mermaid が描ける。
 */

const GANTT =
  "gantt\n  title 計画\n  dateFormat YYYY-MM-DD\n  section A\n  作業 :a1, 2024-01-01, 30d";

test.describe("Excalidraw 自身の mermaid の入口", () => {
  test("「その他」メニューに Mermaid to Excalidraw が出ない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await page.locator(".App-toolbar__extra-tools-trigger").click();
    const menu = page.locator(".App-toolbar__extra-tools-dropdown");
    // 対照。メニューそのものは開けていて、隣の項目は見えている。
    await expect(menu.getByRole("button", { name: "フレームツール" })).toBeVisible();

    await expect(
      menu.getByRole("button", { name: "Mermaid to Excalidraw" }),
    ).toBeHidden();
    // 項目だけ消すと、中身の無い見出しが残る。
    await expect(menu.getByText("Generate", { exact: true })).toBeHidden();
  });

  // Excalidraw 0.18.1 はコマンドパレットを持っているが、自分では置かず export も
  // していないので、etoki の画面には出ない。中には「Mermaid to Excalidraw」が
  // あり、`aiEnabled={false}` で出し分けられている。**開くようになったら、
  // その項目が出ていないことをここで見るように書き換える。**
  test("コマンドパレットが開かない", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await page
      .locator(".excalidraw canvas")
      .last()
      .click({ position: { x: 400, y: 300 } });
    // 開閉を切り替えるショートカットなので 1 回だけ押す。2 つ続けると、
    // 置かれていても開いて閉じるだけで通ってしまう。
    await page.keyboard.press("ControlOrMeta+/");

    await expect(page.locator(".command-palette-dialog")).toHaveCount(0);
  });

  test("キャンバスに mermaid を貼っても描かれず、止めたことが出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await pasteOnCanvas(page, GANTT);

    await expect(
      page.getByText("mermaid の図はキャンバスに直接貼れません"),
    ).toBeVisible();
    // 何も置かれていない。置かれていれば要素が増えて未保存になる。
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // 止めすぎていないことの対照。onPaste が全部を止めていても上の検査は通る。
  test("mermaid でない文字列はそのまま貼れる", async ({ page }) => {
    await openBoardWithMock(page, baseMock());

    await pasteOnCanvas(page, "ログイン画面の文言を直す");

    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(page.getByText("mermaid の図はキャンバスに直接貼れません")).toBeHidden();
  });
});
