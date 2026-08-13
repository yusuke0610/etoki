import { expect, type Locator, type Page } from "@playwright/test";

/**
 * サイドバーからボードを開き、キャンバスと注釈パネルが出るまで待つ。
 *
 * Excalidraw のマウントはキャンバスの描画を伴い、注釈パネルより遅れる。
 * ここで揃うまで待たないと、後続の操作がマウント途中の DOM に当たる。
 */
export async function openBoard(page: Page, name: string): Promise<void> {
  await page.locator(".board-list").getByRole("button", { name }).click();
  await expect(page.getByRole("heading", { name, level: 1 })).toBeVisible();
  await expect(page.locator(".excalidraw canvas").first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "注釈" })).toBeVisible();
}

/**
 * 作成先の選択画面。
 *
 * **リポジトリを押すときは必ずここで絞る。** サイドバーの木にも同じ
 * `acme/web` という名前のボタンが並ぶので（ADR 0019）、ページ全体から
 * 名前で引くと 2 つ見つかって落ちる。
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
