import { expect, test, type Page } from "@playwright/test";

import {
  annotationCard,
  annotationDetail,
  boardListHeading,
  chooseFromMenu,
  drawRectangle,
  ekidokiButton,
  interpret,
  openBoardWithMock,
  saveButton,
  saveScene,
} from "./helpers/board";
import { annotationsOfScene } from "./helpers/boardData";
import { BOARD_ID, annotatedScene, baseMock, board } from "./helpers/fixtures";

/**
 * 手直しした下書きを、確認なしに失わない（#432、ADR 0021 の追記）。
 *
 * 下書きは保存しない。代わりに、離れる前と保存の前に何を失うかを言って訊く。
 * **訊くのは手直ししたときだけ。** 絵解いたままの下書きでは訊かない。
 */

/** 「ログイン」を絵解き、1 件目のタイトルを手直しする。手直しした詳細を返す。 */
async function editDraft(page: Page) {
  await interpret(annotationCard(page, "ログイン"));
  const detail = annotationDetail(page, "ログイン");
  const title = detail.getByLabel("1 件目のタイトル");
  await title.fill("手直ししたタイトル");
  return detail;
}

/** 出た確認の文を控え、answer で答える。 */
function answerDialogs(page: Page, answer: "accept" | "dismiss"): string[] {
  const messages: string[] = [];
  page.on("dialog", (dialog) => {
    messages.push(dialog.message());
    void (answer === "accept" ? dialog.accept() : dialog.dismiss());
  });
  return messages;
}

test.describe("手直しした下書き", () => {
  test("一覧へ戻る前に手直しを失うと言って訊き、断ればボードに残る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    const detail = await editDraft(page);
    const messages = answerDialogs(page, "dismiss");

    await chooseFromMenu(page, "ボード一覧へ戻る");

    await expect
      .poll(() => messages)
      .toEqual(["手直しした下書きがあります。このまま移動すると、手直しは失われます。"]);
    // 断ったので、ボードと手直しが残っている。
    await expect(detail.getByLabel("1 件目のタイトル")).toHaveValue("手直ししたタイトル");
    await expect(boardListHeading(page)).toHaveCount(0);
  });

  test("承諾すれば一覧へ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await editDraft(page);
    answerDialogs(page, "accept");

    await chooseFromMenu(page, "ボード一覧へ戻る");

    await expect(boardListHeading(page)).toBeVisible();
  });

  // 絵解いたままなら失う手直しは無い。訊くと、確認に慣れて読まなくなる。
  test("手直ししていなければ訊かずに一覧へ戻る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await interpret(annotationCard(page, "ログイン"));
    await annotationDetail(page, "ログイン").getByLabel("1 件目のタイトル").waitFor();
    const messages = answerDialogs(page, "dismiss");

    await chooseFromMenu(page, "ボード一覧へ戻る");

    await expect(boardListHeading(page)).toBeVisible();
    expect(messages).toEqual([]);
  });

  // 読み込み直しとタブを閉じる操作はアプリ側で止められない。ブラウザに訊かせる。
  test("タブを閉じようとすると、ブラウザの確認が出る", async ({ page }) => {
    await openBoardWithMock(page, baseMock());
    await editDraft(page);
    // 手直しが確認の登録まで届くのは、描き直したあとの effect。手直しは画面に
    // 印を出さないので、登録されたこと（合成の beforeunload を止める）を待つ。
    await expect
      .poll(() =>
        page.evaluate(() => {
          const e = new Event("beforeunload", { cancelable: true });
          window.dispatchEvent(e);
          return e.defaultPrevented;
        }),
      )
      .toBe(true);

    const types: string[] = [];
    page.on("dialog", (dialog) => {
      types.push(dialog.type());
      void dialog.dismiss();
    });
    await page.close({ runBeforeUnload: true });
    await expect.poll(() => types).toContain("beforeunload");
  });

  // 保存は引いた解釈を捨てるので、手直しも消える。訊いてから保存する。
  test("保存の前に訊き、断れば保存しない。承諾すれば保存して下書きは消える", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());
    const detail = await editDraft(page);

    const declined = answerDialogs(page, "dismiss");
    await saveScene(page);
    await expect
      .poll(() => declined)
      .toEqual([
        "保存すると、絵解いた結果は捨てられ、手直しした下書きも消えます。保存しますか？",
      ]);
    expect(mock.saveRequests).toHaveLength(0);
    await expect(detail.getByLabel("1 件目のタイトル")).toHaveValue("手直ししたタイトル");

    page.removeAllListeners("dialog");
    const accepted = answerDialogs(page, "accept");
    const saved = page.waitForRequest(
      (req) => req.method() === "PUT" && new URL(req.url()).pathname.endsWith("/scene"),
    );
    await saveScene(page);
    await saved;

    await expect(detail.getByLabel("1 件目のタイトル")).toHaveCount(0);
    expect(mock.saveRequests).toHaveLength(1);

    // 下書きは保存で消えたので、もう失う手直しは無い。離れても訊かない。
    await chooseFromMenu(page, "ボード一覧へ戻る");
    await expect(boardListHeading(page)).toBeVisible();
    expect(accepted).toHaveLength(1);
  });

  // 保存を呼ぶ口はほかにもある。どれも同じ確認を通す（保存の入口、`useSceneSave` の `save`）。
  // 未保存のあいだだけ出る「保存」と、未保存なら保存を挟む「絵解き」。
  test("未保存のあいだの「保存」も「絵解き」も、保存の前に訊き、断れば保存しない", async ({
    page,
  }) => {
    // 注釈が 1 件の構成。保存が通れば「絵解き」はその詳細を開く（一覧ではなく）
    // ので、開かないことを詳細で見られる。保存済みの一覧もシーンに揃える
    // （`ekidoki.spec.ts` と同じ）。
    const single = baseMock();
    single.details[BOARD_ID] = { ...board(), scene: annotatedScene() };
    single.annotations[BOARD_ID] = annotationsOfScene(annotatedScene());
    const mock = await openBoardWithMock(page, single);
    const detail = await editDraft(page);
    await detail.getByRole("button", { name: "閉じる" }).click();
    await drawRectangle(page);
    const declined = answerDialogs(page, "dismiss");

    await saveButton(page).click();
    await expect.poll(() => declined).toHaveLength(1);
    await ekidokiButton(page).click();
    await expect.poll(() => declined).toHaveLength(2);

    expect(declined).toEqual([
      "保存すると、絵解いた結果は捨てられ、手直しした下書きも消えます。保存しますか？",
      "保存すると、絵解いた結果は捨てられ、手直しした下書きも消えます。保存しますか？",
    ]);
    expect(mock.saveRequests).toHaveLength(0);
    // 断ったので、絵解きの面も開かない（保存できなかったら進まない、ADR 0077）。
    // **面そのものを見る。** 「未保存」の表示だけだと、断ったのに面を開く退行でも
    // 残るので通る（#485 の full review）。
    await expect(detail).toHaveCount(0);
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
  });

  // 保存の失敗の通知にある「再試行」も保存の口。確認を通らずに送ると、失敗の
  // あとに残っていた手直しが黙って消える（#485 の full review）。
  test("保存に失敗したあとの「再試行」も、保存の前に訊き、断れば保存しない", async ({
    page,
  }) => {
    const mock = await openBoardWithMock(page, baseMock());
    const detail = await editDraft(page);
    mock.saveSceneError = {
      status: 500,
      body: { code: "internal", error: "internal error" },
    };
    const accepted = answerDialogs(page, "accept");

    await saveScene(page);
    const alert = page.getByRole("alert");
    await expect(alert).toContainText("保存できませんでした");
    expect(accepted).toHaveLength(1);
    expect(mock.saveRequests).toHaveLength(1);

    // 失敗を外しておく。確認を通らずに送る実装なら、ここで保存が通り、手直しが消える。
    delete mock.saveSceneError;
    page.removeAllListeners("dialog");
    const declined = answerDialogs(page, "dismiss");
    await alert.getByRole("button", { name: "再試行" }).click();

    await expect
      .poll(() => declined)
      .toEqual([
        "保存すると、絵解いた結果は捨てられ、手直しした下書きも消えます。保存しますか？",
      ]);
    expect(mock.saveRequests).toHaveLength(1);
    await expect(detail.getByLabel("1 件目のタイトル")).toHaveValue("手直ししたタイトル");
  });
});
