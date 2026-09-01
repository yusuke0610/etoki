import { readFile } from "node:fs/promises";

import { expect, test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { drawRectangle, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock } from "./helpers/fixtures";

/**
 * ボードの持ち出しと取り込み（#42、ADR 0042）。
 *
 * **ここでしか確かめられない。** 書き出しはブラウザのダウンロードを、取り込みは
 * `loadFromBlob` の実物と Excalidraw への差し込みを通る。どちらも jsdom では
 * 動かないので、vitest（`src/excalidraw/transfer.test.ts`）が見ているのは
 * ファイル名の作り方と、返ってきたものの詰め替えまで。
 */

const BOARD_NAME = "認証まわりのブレスト";

/** 取り込ませる `.excalidraw` の中身。背景色は往復で戻ることを見るために変える。 */
function importedFile(viewBackgroundColor = "#ffffff"): string {
  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "e2e",
    // **既定のボードとは別の frame ID にしてある。** 置き換わったことは、
    // 元の注釈がキャンバスから消えることでしか確かめられない。
    // 足りないフィールドはライブラリの復元が埋める。
    elements: [
      {
        id: "frame-imported",
        type: "frame",
        x: 0,
        y: 0,
        width: 400,
        height: 300,
        name: "取り込んだ範囲",
        customData: { etoki: { granularity: "epic" } },
      },
    ],
    appState: { viewBackgroundColor },
    files: {},
  });
}

/** ファイルを選ぶ。入力は隠してあるので、ボタンではなく入力に直接渡す。 */
async function chooseFile(page: Page, content: string): Promise<void> {
  await page.getByLabel("取り込む .excalidraw ファイル").setInputFiles({
    name: "持ってきたボード.excalidraw",
    mimeType: "application/json",
    buffer: Buffer.from(content),
  });
}

/** キャンバスに重なっている注釈の枠（ADR 0036）。取り込みで入れ替わる。 */
function annotationFrames(page: Page) {
  return page.locator(".annotation-overlay-frame");
}

test.describe("書き出し", () => {
  test("ボード名のファイルに、注釈つきのシーンが出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("button", { name: "書き出し" }).click(),
    ]);

    // どのボードのものか読める名前で出る。
    expect(download.suggestedFilename()).toBe(`${BOARD_NAME}.excalidraw`);

    const path = await download.path();
    const scene = JSON.parse(await readFile(path, "utf-8")) as {
      type: string;
      elements: { id: string; customData?: { etoki?: unknown } }[];
    };

    // Excalidraw 本体で開ける形であること。
    expect(scene.type).toBe("excalidraw");

    // **注釈の指定は customData に載ったまま出る。** ここが落ちると、書き出した
    // ファイルを取り込み直しても注釈が注釈でなくなる。
    const annotated = scene.elements.filter((el) => el.customData?.etoki !== undefined);
    expect(annotated.map((el) => el.id)).toEqual([
      "frame-uncreated",
      "frame-created",
      "frame-changed",
    ]);
  });

  // **保存済みシーンではなくキャンバスから出す**（ADR 0042）。保存済みから
  // 出すと、未保存の描き足しが黙って落ちる。
  test("未保存の描き足しも出る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const savedScene = mock.details[BOARD_ID]?.scene ?? "{}";
    const saved = (JSON.parse(savedScene) as { elements: [] }).elements.length;

    await drawRectangle(page);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("button", { name: "書き出し" }).click(),
    ]);

    const scene = JSON.parse(await readFile(await download.path(), "utf-8")) as {
      elements: unknown[];
    };
    expect(scene.elements.length).toBe(saved + 1);

    // 書き出しは保存ではない。サーバーの持ちものも未保存の表示も変わらない。
    expect(mock.details[BOARD_ID]?.scene).toBe(savedScene);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });
});

test.describe("取り込み", () => {
  // **載せるだけで、確定させるのは人間の保存操作だけ**（ADR 0042、中核思想 3）。
  test("キャンバスが置き換わり、未保存になる（サーバーには送らない）", async ({
    page,
  }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const before = mock.details[BOARD_ID]?.updatedAt;
    const size = await page.locator(".badge-size").innerText();
    await expect(annotationFrames(page)).toHaveCount(3);

    await chooseFile(page, importedFile());

    // 取り込んだ注釈 1 つに入れ替わる。
    await expect(annotationFrames(page)).toHaveCount(1);
    await expect(annotationFrames(page)).toContainText("注釈 epic");

    // 状態は保存済みシーンが基準なので、元の 3 つは一覧に残ったまま「キャンバスに
    // ありません」になる（ADR 0022）。**取り込みでサーバーは変わらない**ことが、
    // ここに出る。
    await expect(page.getByText("このフレームはキャンバスにありません。")).toHaveCount(3);

    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    expect(mock.details[BOARD_ID]?.updatedAt).toBe(before);

    // 保存に送る大きさも数え直す。**据え置くと、上限に当たるファイルを
    // 取り込んでも押す前には分からない**（ADR 0038 の見せ方が効かなくなる）。
    await expect(page.locator(".badge-size")).not.toHaveText(size);
  });

  // **持ち出して戻せることが #42 の要点。** 片道ずつ通っていても、往復で
  // 変わってしまうものがあれば持ち出す意味が薄い。背景色は `appState` に
  // あって要素には無いので、ここを落としても要素の検査は素通りする。
  test("取り込んで書き出すと、要素も背景色も戻る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await chooseFile(page, importedFile("#ffeb3b"));
    await expect(annotationFrames(page)).toHaveCount(1);

    const [download] = await Promise.all([
      page.waitForEvent("download"),
      page.getByRole("button", { name: "書き出し" }).click(),
    ]);

    const scene = JSON.parse(await readFile(await download.path(), "utf-8")) as {
      elements: { id: string; customData?: { etoki?: { granularity?: string } } }[];
      appState: { viewBackgroundColor?: string };
    };

    expect(scene.appState.viewBackgroundColor).toBe("#ffeb3b");
    expect(scene.elements.map((el) => el.id)).toEqual(["frame-imported"]);
    // 注釈の指定も同じまま。ここが落ちると、往復するたびに注釈が外れる。
    expect(scene.elements[0]?.customData?.etoki?.granularity).toBe("epic");
  });

  // 未保存の内容を黙って捨てない（ADR 0021 と同じ形）。
  test("未保存のときは確認を出し、断れば取り込まない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    // **待ってから読む。** `setInputFiles` はファイルを差し込むだけで、取り込み
    // の完了は待たない。待たずに読むと、確認が出る前の値を見て通る。
    const dialog = page.waitForEvent("dialog");
    await chooseFile(page, importedFile());

    const asked = await dialog;
    expect(asked.message()).toContain("置き換わります");
    await asked.dismiss();

    // 断ったので元のまま。
    await expect(annotationFrames(page)).toHaveCount(3);
  });

  test("未保存でも、承諾すれば取り込む", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    page.once("dialog", (dialog) => void dialog.accept());
    await chooseFile(page, importedFile());

    await expect(annotationFrames(page)).toHaveCount(1);
  });

  // 未保存でなければ失うものが無いので訊かない。訊くと、開いた直後の取り込みが
  // 毎回 1 手増える。
  test("未保存でなければ確認しない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    let asked = false;
    page.on("dialog", (dialog) => {
      asked = true;
      void dialog.dismiss();
    });

    await chooseFile(page, importedFile());

    await expect(annotationFrames(page)).toHaveCount(1);
    expect(asked).toBe(false);
  });

  // 読めないものをキャンバスに渡すと、そのボードが開けなくなる（#42）。
  test("読めないファイルは断り、キャンバスを変えない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await chooseFile(page, "これは Excalidraw のシーンではない");

    await expect(page.locator(".error-message")).toContainText(
      "Excalidraw のシーンとして読めませんでした",
    );
    await expect(annotationFrames(page)).toHaveCount(3);
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // 読み込みの入口を 1 つに保つ（ADR 0042）。**ライブラリのメニューに残っている
  // と、同じ画面に意味の違う「保存」が 2 つ並ぶ。**
  test("ライブラリのメニューに開く・名前を付けて保存が無い", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // ハンバーガーは `.dropdown-menu-button` を名乗る 2 つのうちの 1 つ
    // （もう 1 つはツールバーの「その他のツール」）。取り違えると、閉じたか
    // どうかを見ずに通る。
    await page.locator('[data-testid="main-menu-trigger"]').click();
    const menu = page.locator(".excalidraw .dropdown-menu");
    await expect(menu).toBeVisible();

    await expect(menu.getByText("開く", { exact: true })).toHaveCount(0);
    // `.excalidraw` の書き出し。日本語の表示が「名前を付けて保存...」で、下の
    // 画像のエクスポートと紛らわしいが別の口。
    await expect(menu.getByText("名前を付けて保存...")).toHaveCount(0);
    // 画像のエクスポートは閉じない。答えている問いが違う。
    await expect(menu.getByText("画像のエクスポート...")).toHaveCount(1);
  });
});
