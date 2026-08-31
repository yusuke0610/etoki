import { expect, test } from "@playwright/test";

import { expectBlockedReason, expectNoAxeViolations } from "./helpers/a11y";
import { installApi } from "./helpers/api";
import { annotationCard, drawRectangle, openBoard } from "./helpers/board";
import { BOARD_ID, annotations, baseMock } from "./helpers/fixtures";

/**
 * アクセシビリティの判断が壊れたことに気づくための spec（#80、ADR 0039）。
 *
 * **形の規約は eslint（jsx-a11y）が見る。ここが見るのは判断の規約。**
 * 「押せない理由を `title` に隠さず本文として出し、ボタンから
 * `aria-describedby` で指す」は etoki 固有の判断なので、既製のルールでは
 * 検知できない。**そしてこちらのほうが重い。**
 *
 * **押せないボタンは 1 箇所ずつ数える。** ここに並んでいないボタンは守られて
 * いない。押せないボタンを足したら、ここにも足す。
 */

const BOARD_NAME = "認証まわりのブレスト";

test.describe("押せない理由が本文として読める", () => {
  // 未保存のあいだは解釈させない（ADR 0018）。テキストは保存済みシーンから、
  // 画像は画面から取るので、揃っていないと 1 回の解釈の入力が食い違う。
  test("解釈する：未保存のとき", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    await expectBlockedReason(
      annotationCard(page, "ログイン").getByRole("button", { name: "解釈する" }),
      "保存してから解釈できます",
    );
  });

  // LLM が未設定の構成（ADR 0030）。理由はパネルに 1 つだけ置いて、各カードの
  // ボタンがそこを指す。**capabilities.spec.ts は id の値を見ている。**
  // こちらが見るのは、その先が実在して読めること。
  test("解釈する：LLM が未設定のとき", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, creation: true, sharing: true },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expectBlockedReason(
      annotationCard(page, "ログイン").getByRole("button", { name: "解釈する" }),
      "ETOKI_LLM_API_KEY",
    );
  });

  // 作成は取り消せない（ADR 0009）。何が足りなくて押せないのかが読めないと、
  // 開発者は選び直しようがない。
  test("GitHub に作成する：作るものが 1 件も無いとき", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();

    // epic を外すと、それを親に持つ issue も一緒に外れる。3 件とも外れる。
    await card.getByLabel("e1 を作成する").uncheck();

    await expectBlockedReason(
      card.getByRole("button", { name: "GitHub に作成する" }),
      "作るものが 1 件も選ばれていません。",
    );
  });

  // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる（ADR 0021）。
  test("作成先を変更：未保存のとき", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    await expectBlockedReason(
      page.getByRole("button", { name: "作成先を変更" }),
      "保存してから作成先を変更できます",
    );
  });

  // 状態は保存済みシーンが基準なので、未保存で消したフレームの注釈が一覧に
  // 残る（ADR 0022）。押しても飛び先が無い。
  test("注釈の見出し：フレームがキャンバスに無いとき", async ({ page }) => {
    const mock = baseMock();
    mock.annotations[BOARD_ID] = [
      ...annotations(),
      // シーンにこの ID の frame は無い。保存前に消したフレームの注釈。
      { id: "frame-gone", name: "消したフレーム", granularity: "", state: "uncreated" },
    ];
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expectBlockedReason(
      annotationCard(page, "消したフレーム").getByRole("button", {
        name: "消したフレーム",
      }),
      "このフレームはキャンバスにありません。",
    );
  });

  // 理由を出す側が壊れたら落ちること自体を確かめる。**ここが落ちなければ、
  // 上の 5 つは何も守っていない。**
  test("理由の要素が消えたら落ちる", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await drawRectangle(page);

    const button = page.getByRole("button", { name: "作成先を変更" });
    await expectBlockedReason(button, "保存してから作成先を変更できます");

    // 理由の本文だけを消す。ボタンは押せないまま、指す先が無くなる。
    await page.locator("#target-change-blocked").evaluate((el) => el.remove());

    await expect(
      expectBlockedReason(button, "保存してから作成先を変更できます"),
    ).rejects.toThrow();
  });
});

/**
 * 形の規約のうち、静的解析では見えないもの。
 *
 * **jsx-a11y と重ならない。** あちらは JSX の属性しか見ないので、実際に描いた
 * 色のコントラストは見えない。入れた時点で `.hint` が 4.48:1（AA は 4.5:1）で
 * 落ちていた。**押せない理由を出しているのが、その `.hint` だった。**
 */
test.describe("axe（etoki が書いた DOM）", () => {
  test("ボードの一覧", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await page.locator(".board-list").waitFor();

    await expectNoAxeViolations(page);
  });

  test("ボードを開いた状態", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expectNoAxeViolations(page);
  });

  // 解釈結果は画面の中でいちばん要素が多い。作る前に読ませる場所なので
  // （ADR 0024）、読めないものが混じっていないかをここで見る。
  test("解釈結果を出した状態", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();
    // 畳んだままでは中を見られない。作成前に読ませる本文まで含めて掛ける。
    for (const summary of await card.getByText("本文", { exact: true }).all()) {
      await summary.click();
    }

    await expectNoAxeViolations(page);
  });
});
