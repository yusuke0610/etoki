import {
  annotationCard,
  drawRectangle,
  importBoardFile,
  saveScene,
  waitForBoard,
} from "../e2e/helpers/board";
import {
  ANNOTATION_NAME,
  IMAGE_DATA_URL,
  brainstormFile,
  expect,
  exportScene,
  newBoard,
  test,
  uniqueWord,
} from "./stack";

/**
 * 保存の往復（ADR 0097）。ブラウザが送る本物のシーンを Go が読み、保存し、開き
 * 直したときに同じものとして返せるか。
 *
 * モックの E2E は保存の本文を記録するだけで、読むのは書いた人の想定したモック。
 * Go のテストは実 SQLite だが、ブラウザが作る本文を通らない。どちらでも、
 * 片方だけ形を変えたら緑のまま壊れる。
 */
test("描いて保存し、描き足してもう一度保存すると、開き直しても画像ごと残る", async ({
  page,
}, testInfo) => {
  const word = uniqueWord(testInfo);
  const name = `保存の往復 ${word}`;

  // 保存の応答を記録する。**2 回目も 200 で通ることを見る。** 応答の版を次の
  // 基準に差し替え損ねると、2 回目が必ず 409 になる（ADR 0020）。
  const saves: number[] = [];
  page.on("response", (response) => {
    const { pathname } = new URL(response.url());
    if (
      response.request().method() === "PUT" &&
      /^\/api\/boards\/[^/]+\/scene$/.test(pathname)
    ) {
      saves.push(response.status());
    }
  });

  // キャンバスのフォントを自分の配信元から取れたか（#114）。**外へ出ていないことは
  // `test` の土台が見る**が、それだけでは「1 つも取りに行っていない」でも通る。
  const fonts: number[] = [];
  page.on("response", (response) => {
    if (new URL(response.url()).pathname.startsWith("/excalidraw-assets/fonts/")) {
      fonts.push(response.status());
    }
  });

  await newBoard(page, name);
  const unsaved = page.getByText("未保存", { exact: true });

  await importBoardFile(page, brainstormFile(`${word} 保存`));
  await expect(unsaved).toBeVisible();
  await saveScene(page);
  await expect(unsaved).toBeHidden();

  // 描き足して、もう一度保存する。画像はサーバーがもう持っているので、2 回目は
  // 送らない（ADR 0074）。送らなくても消えないことを、開き直して確かめる。
  await drawRectangle(page);
  await expect(unsaved).toBeVisible();
  await saveScene(page);
  await expect(unsaved).toBeHidden();
  await expect(page.getByText("他の人がこのボードを保存しました")).toBeHidden();
  expect(saves).toEqual([200, 200]);

  // 開き直す。手元に残っているものではなく、サーバーが返したものを見る。
  await page.reload();
  await waitForBoard(page, name);
  await expect(annotationCard(page, ANNOTATION_NAME)).toContainText("未作成");

  const scene = await exportScene(page);
  const frame = scene.elements.find((element) => element.type === "frame");
  const text = scene.elements.find((element) => element.type === "text");
  const image = scene.elements.find((element) => element.type === "image");

  // 注釈のメタデータは Go 側が `json.RawMessage` で受けて要素ごとに読む。
  // 往復で落ちると、開き直したときに注釈でなくなる。
  expect(frame?.customData?.etoki?.granularity).toBe("epic");
  expect(text?.text).toBe(`${word} 保存`);
  expect(text?.frameId).toBe(frame?.id);
  // 画像は要素と実体に分かれて往復する。**両方を見る。** 要素だけ戻っても
  // 実体が欠けていれば、開いたときに画像が描けない。
  expect(image?.frameId).toBe(frame?.id);
  expect(scene.files?.[image?.fileId ?? ""]?.dataURL).toBe(IMAGE_DATA_URL);
  expect(scene.elements.filter((element) => element.type === "rectangle")).toHaveLength(
    1,
  );

  await expect.poll(() => fonts.length).toBeGreaterThan(0);
  // 開き直したあとはブラウザが手元の写しを確かめに来るので、304 も返りうる。
  // 落とすのは取れなかったときだけ。
  expect(fonts.filter((status) => status >= 400)).toEqual([]);
});
