import { expect, test } from "@playwright/test";

import { installApi, summarize, type ApiMock } from "./helpers/api";
import { drawRectangle, openBoard } from "./helpers/board";
import { baseMock, board } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";
const OTHER_NAME = "課金まわりのブレスト";

/** 切り替え先のあるボード一覧。離脱の確認は 2 枚ないと確かめられない。 */
function twoBoards(): ApiMock {
  const mock = baseMock();
  const other = { ...board(), id: "board-other", name: OTHER_NAME };

  mock.boards = [...mock.boards, summarize(other)];
  mock.details[other.id] = other;
  mock.annotations[other.id] = [];

  return mock;
}

test.describe("シーンの保存", () => {
  // Excalidraw はマウント時にも onChange を発火する。それを編集として扱うと、
  // 開いただけで未保存になり、表示が信用できなくなる。
  test("開いた直後は未保存にならない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("未保存の変更あり")).toBeHidden();
  });

  // 選択やスクロールでも onChange は発火する。見ただけで未保存になってはならない。
  test("キャンバスを触っただけでは未保存にならない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const canvas = page.locator(".excalidraw canvas").first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error("キャンバスが表示されていない");

    // 何も無いところをクリックし、選択矩形をドラッグする。要素は増えない。
    await page.mouse.click(box.x + 200, box.y + 200);
    await page.mouse.move(box.x + 150, box.y + 150);
    await page.mouse.down();
    await page.mouse.move(box.x + 400, box.y + 320, { steps: 8 });
    await page.mouse.up();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  test("編集すると未保存が出て、保存すると消える", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    await drawRectangle(page);

    // 3 状態の判定は保存済みシーンが基準。編集中の内容は反映されないので、
    // 反映されていないことが画面から分かる必要がある。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    await expect(page.getByText("未保存の変更あり")).toBeVisible();

    await page.getByRole("button", { name: "保存" }).click();

    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
    await expect(page.getByText("未保存の変更あり")).toBeHidden();
  });

  // ブレストは最初のフェーズなので、ここで失うと後段が全部やり直しになる。
  // 保存が明示操作である以上、押し忘れは構造的に起きる。
  test("未保存のままボードを切り替えようとすると、確認が出て残る", async ({ page }) => {
    await installApi(page, twoBoards());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    const messages: string[] = [];
    // 断る側。confirm を出しただけで移動していては止めたことにならない。
    page.on("dialog", (dialog) => {
      messages.push(dialog.message());
      void dialog.dismiss();
    });
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();

    await expect(page.getByRole("heading", { name: BOARD_NAME, level: 1 })).toBeVisible();
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    expect(messages).toHaveLength(1);
    expect(messages[0]).toContain("未保存の変更があります");
  });

  // リロードとタブを閉じる操作はアプリ側で止められない。beforeunload を登録して
  // ブラウザに確認させる。**登録漏れは画面の見た目に出ない**ので、ここで固定する。
  test("未保存のままタブを閉じようとすると、ブラウザの確認が出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    const types: string[] = [];
    page.on("dialog", (dialog) => {
      types.push(dialog.type());
      void dialog.dismiss();
    });

    // runBeforeUnload を付けないとハンドラごと飛ばして閉じる。閉じ終わるのは
    // 待たないので、確認が出たかどうかは後から見る。
    await page.close({ runBeforeUnload: true });
    await expect.poll(() => types).toContain("beforeunload");
  });

  // 止めるのは「知らせずに捨てる」ことだけ。捨てると決めたなら通す（中核思想 3）。
  test("確認を承諾すれば、ボードは切り替わる", async ({ page }) => {
    await installApi(page, twoBoards());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    page.on("dialog", (dialog) => void dialog.accept());
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();

    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    // 切り替えた先は未保存ではない。持ち越すと、開いただけで止められる。
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // 保存済みなら黙って切り替わる。毎回確認を出すと、確認そのものが読まれなくなる。
  test("保存してあれば、確認なしで切り替わる", async ({ page }) => {
    await installApi(page, twoBoards());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    // 出た確認は控えておく。Playwright は既定で dialog を閉じるので、拾わずに
    // おくと「出たが自動で閉じられた」と「出なかった」を取り違える。
    const messages: string[] = [];
    page.on("dialog", (dialog) => {
      messages.push(dialog.message());
      void dialog.dismiss();
    });
    await page.locator(".board-list").getByRole("button", { name: OTHER_NAME }).click();

    await expect(page.getByRole("heading", { name: OTHER_NAME, level: 1 })).toBeVisible();
    expect(messages).toEqual([]);
  });

  test("未保存のまま解釈しようとすると、保存を促す", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await drawRectangle(page);

    await expect(
      page.getByText("保存してから解釈できます", { exact: false }).first(),
    ).toBeVisible();
  });

  test("保存すると、それまでの解釈結果は捨てられる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = page.locator("li.annotation").filter({ hasText: "ログイン" });
    await card.getByRole("button", { name: "解釈する" }).click();
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();

    await drawRectangle(page);
    await page.getByRole("button", { name: "保存" }).click();

    // 解釈は保存済みシーンに対する結果。保存したら対象が変わっているので、
    // 古い結果のまま作成させてはならない。
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);
  });
});
