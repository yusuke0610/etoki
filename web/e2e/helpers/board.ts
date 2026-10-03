import { expect, type Locator, type Page } from "@playwright/test";

import { installApi, type ApiMock } from "./api";
import { BOARD_NAME } from "./fixtures";

/**
 * 開いているボードを閉じて一覧へ戻る（ADR 0064）。
 *
 * **一覧はボードと別の画面にある。** 別のボードを開くのも、一覧の中身を
 * 確かめるのも、ここを通ってから。未保存なら確認が出るので、それを見る spec は
 * 先に `dialog` を拾っておく。
 */
export async function backToList(page: Page): Promise<void> {
  await page.getByRole("button", { name: "ボード一覧", exact: true }).click();
  // **見出しは完全一致で引く。** 同じ画面に「新しいボード」のボタンも並ぶ。
  await expect(
    page.getByRole("heading", { name: "ボード", exact: true, level: 2 }),
  ).toBeVisible();
}

/**
 * 一覧の「新しいボード」からダイアログを開き、名前（とひな形）を入れて「次へ」を
 * 押す（#200）。押した先は作成先の選択画面で、**まだボードは作られていない**。
 *
 * ダイアログの中身を確かめる spec（押せない「次へ」、やめたときの入力）は、
 * これを使わずに `newBoardDialog` で開く。
 */
export async function startNewBoard(
  page: Page,
  name: string,
  template?: string,
): Promise<void> {
  const dialog = await newBoardDialog(page);
  await dialog.getByLabel("ボード名").fill(name);
  if (template !== undefined) await dialog.getByLabel("ひな形").selectOption(template);
  await dialog.getByRole("button", { name: "次へ" }).click();
}

/** 一覧の「新しいボード」を押して、開いたダイアログを返す。 */
export async function newBoardDialog(page: Page): Promise<Locator> {
  await page.getByRole("button", { name: "新しいボード", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "新しいボード" });
  await expect(dialog).toBeVisible();
  return dialog;
}

/**
 * 一覧の画面が出るまで待つ。**ログインの前後を見る spec の目印。**
 *
 * 「ボード名」の入力は、ダイアログを開くまで画面に無い（#200）。目印にすると、
 * 一覧が出ていても見つからない。
 */
export function boardListHeading(page: Page): Locator {
  return page.getByRole("heading", { name: "ボード", exact: true, level: 2 });
}

/** 一覧からボードを開き、キャンバスと注釈パネルが出るまで待つ。 */
export async function openBoard(page: Page, name: string): Promise<void> {
  await page.locator(".board-list").getByRole("button", { name }).click();
  await waitForBoard(page, name);
}

/**
 * ボードの画面が出て、キャンバスと注釈パネルが揃うまで待つ。
 *
 * **押さずに開く経路ではこちらを使う。** 読み込み直しや URL から入ると、一覧を
 * 通らずにボードが出る（ADR 0059）。そこで `openBoard` を呼ぶと、無い一覧を
 * 押しにいって落ちる。
 *
 * Excalidraw のマウントはキャンバスの描画を伴い、注釈パネルより遅れる。
 * ここで揃うまで待たないと、後続の操作がマウント途中の DOM に当たる。
 */
export async function waitForBoard(page: Page, name: string): Promise<void> {
  await expect(page.getByRole("heading", { name, level: 1 })).toBeVisible();
  await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
  // **見出しは階層まで絞る。** パネルの中には「キャンバスに無い注釈」
  // （#111）のような h3 も並ぶので、名前だけで引くと 2 つ見つかって落ちる。
  await expect(page.getByRole("heading", { name: "注釈", level: 2 })).toBeVisible();
}

/**
 * モックを入れてトップを開き、ボードを開くところまで進める。
 *
 * ほとんどの spec が同じ 3 手から始まるので、ここ 1 つに置く（#156）。**途中に
 * 何かを挟む spec（開く前の画面を撮る、モックを差し替えてから開く）は、
 * これを使わずに 3 手を書く。** 返すのは `installApi` と同じく、記録を読むための
 * モック。
 */
export async function openBoardWithMock(
  page: Page,
  mock: ApiMock,
  name: string = BOARD_NAME,
): Promise<ApiMock> {
  const installed = await installApi(page, mock);
  await page.goto("/");
  await openBoard(page, name);
  return installed;
}

/**
 * 作成先の選択画面。
 *
 * **リポジトリを押すときは必ずここで絞る。** 一覧が同じ画面にあったころは、
 * 木にも同じ `acme/web` という名前のボタンが並んだ（ADR 0019）。一覧が別の
 * 画面になって（ADR 0064）今は衝突しないが、戻す判断が出た日に黙って壊れない
 * ように絞り続ける。
 */
export function picker(page: Page): Locator {
  return page.locator(".picker");
}

/** リポジトリと Project を順に選んで作成先を決める。 */
export async function chooseTarget(
  page: Page,
  repository: string | RegExp,
  project: string,
): Promise<void> {
  await picker(page).getByRole("button", { name: repository }).click();
  await picker(page).getByRole("button", { name: project }).click();
}

/** 注釈 1 つぶんのカード。名前で絞り込む。 */
export function annotationCard(page: Page, name: string): Locator {
  return page.locator("li.annotation").filter({ hasText: name });
}

/**
 * キャンバスに矩形を 1 つ描いて、シーンを変更した状態にする。
 *
 * 座標はキャンバスからの相対で取る。左上に固定オフセットで打つと、ツールバーや
 * 案内文が重なっていてポインタが canvas に届かない。実際それで 1 つも描けて
 * いなかったが、当時は onChange が発火しただけで未保存になっていたためテストは
 * 通っていた。描けたことを undo の活性で確かめてから返す。
 */
export async function drawRectangle(page: Page): Promise<void> {
  const canvas = page.locator(".excalidraw canvas").first();
  const box = await canvas.boundingBox();
  if (!box) throw new Error("キャンバスが表示されていない");

  // 図形ツールはショートカットで選ぶ。ツールバーのラベルは Excalidraw の翻訳に
  // 依存する。ただしショートカットはキャンバスにフォーカスが無いと届かないので、
  // 先に何も無いところをクリックしておく。
  await page.mouse.click(box.x + box.width * 0.5, box.y + box.height * 0.85);
  await page.keyboard.press("r");

  await page.mouse.move(box.x + box.width * 0.35, box.y + box.height * 0.4);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width * 0.6, box.y + box.height * 0.65, {
    steps: 10,
  });
  await page.mouse.up();

  await expect(page.getByRole("button", { name: "元に戻す" })).toBeEnabled();
}

/**
 * キャンバスに文字列を貼る。
 *
 * 本物のクリップボードは権限と OS に依存するので、`ClipboardEvent` を直接
 * 投げる。Excalidraw は `document` の paste を拾い、カーソルの下がキャンバスで
 * あることを確かめるので、先にキャンバスの上をクリックしておく。
 */
export async function pasteOnCanvas(page: Page, text: string): Promise<void> {
  await page
    .locator(".excalidraw canvas")
    .last()
    .click({ position: { x: 400, y: 300 } });
  await page.evaluate((t) => {
    const data = new DataTransfer();
    data.setData("text/plain", t);
    document.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: data,
        bubbles: true,
        cancelable: true,
      }),
    );
  }, text);
}
