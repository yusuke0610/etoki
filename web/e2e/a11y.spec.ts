import { expect, test } from "@playwright/test";

import { expectBlockedReason, expectNoAxeViolations } from "./helpers/a11y";
import { holdCreate, holdSave, installApi } from "./helpers/api";
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
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expectBlockedReason(
      annotationCard(page, "ログイン").getByRole("button", { name: "解釈する" }),
      "ETOKI_LLM_API_KEY",
    );
  });

  // 図のドラフトも同じ LLM の設定で決まるが、答えている問いは解釈と別
  // （ADR 0041）。**diagramChat.spec.ts は id の値を見ている。** こちらが見るのは、
  // その先が実在して読めること。
  test("生成する：LLM が未設定のとき", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await page.getByRole("button", { name: "図のドラフト", exact: true }).click();

    await expectBlockedReason(
      page.getByRole("button", { name: "生成", exact: true }),
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

  // 取り消せない操作と保存は相互に排他する（`.claude/rules/async-ui.md`）。
  // **一時的でも押せない理由。** 待てば押せるようになることは、待てると分かって
  // いる人にしか分からない。
  test("GitHub に作成する：保存中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdSave(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 解釈してからでないと作成ボタンが出ない。保存は解釈結果を捨てるが、
    // 捨てるのは応答が返ってからなので、止めているあいだは並んでいる。
    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).waitFor();

    await page.getByRole("button", { name: "保存", exact: true }).click();

    await expectBlockedReason(
      card.getByRole("button", { name: "GitHub に作成する" }),
      "保存が終わるまで作成できません",
    );

    // **理由が消えることまで見る。** 出しっぱなしの文でも上の検査は通る。
    release();
    await expect(page.getByText("保存が終わるまで作成できません")).toBeHidden();
  });

  // 「作成先を変更」は未保存でも保存中でも押せない。**未保存のほうだけ
  // 見ていると、変更なしで保存を押したあいだが空く。** ボタンは押せないのに
  // 理由が消える、という状態が残る。
  test("作成先を変更：保存中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdSave(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // **描かない。** 描くと未保存の理由のほうが出て、保存中の経路を通らない。
    await page.getByRole("button", { name: "保存", exact: true }).click();

    await expectBlockedReason(
      page.getByRole("button", { name: "作成先を変更" }),
      "保存が終わるまで作成先を変更できません",
    );

    release();
    await expect(page.getByRole("button", { name: "作成先を変更" })).toBeEnabled();
  });

  // 逆向き。作成中は保存させない。**押せない理由を `title` に置くと、この
  // テストが落ちる。** `disabled` なボタンはフォーカスも当たらないので、
  // ホバーできない利用者には届かない。
  test("保存：作成中のとき", async ({ page }) => {
    await installApi(page, baseMock());
    let release = () => {};
    await holdCreate(
      page,
      new Promise<void>((resolve) => {
        release = resolve;
      }),
    );

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    await expectBlockedReason(
      page.getByRole("button", { name: "保存", exact: true }),
      "作成が終わるまで保存できません",
    );

    release();
    await expect(page.getByText("作成が終わるまで保存できません")).toBeHidden();
  });

  // 理由を出す側が壊れたら落ちること自体を確かめる。**ここが落ちなければ、
  // 上のどれも何も守っていない。**
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

  // 図のドラフトのチャットは、キャンバスの左に開く独立した領域（ADR 0041）。
  // **開かないと DOM に出ない**ので、上の 2 つでは一度も掛かっていない。
  // 生成結果を出したところまで開けて、`.diagram-mermaid` と
  // 「ここまでのやりとり」まで含めて見る。
  test("図のドラフトを生成した状態", async ({ page }) => {
    await installApi(page, baseMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.getByRole("button", { name: "図のドラフト", exact: true }).click();
    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();
    await page.locator(".diagram-mermaid").waitFor();

    await expectNoAxeViolations(page);
  });

  // 削除の確認は etoki が自前で `role` を書いている唯一の場所（ADR 0042）。
  // **開かないと DOM に出ない**ので、上の 2 つでは一度も掛かっていない。
  test("削除の確認を開いた状態", async ({ page }) => {
    const mock = baseMock();
    // 件数は文言そのもの。0 件だと分岐の片方しか描かれない。
    mock.deletion = { [BOARD_ID]: { status: 200, body: { recordedItemCount: 3 } } };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.getByRole("button", { name: "ボードを削除" }).click();
    await page.getByRole("alertdialog").waitFor();

    await expectNoAxeViolations(page);
  });

  // メンバーのパネルも独立した領域で、開くまで DOM に出ない。行ごとのボタンが
  // 並ぶ唯一の画面でもある（`.claude/rules/async-ui.md` の「行固有の
  // accessible name」）。
  test("メンバーを開いた状態", async ({ page }) => {
    const mock = baseMock();
    mock.members = {
      [BOARD_ID]: [
        {
          userId: "user-alice",
          login: "alice",
          displayName: "Alice",
          role: "owner",
          createdAt: "2026-08-01T09:00:00Z",
        },
        {
          userId: "user-bob",
          login: "bob",
          displayName: "Bob",
          role: "editor",
          createdAt: "2026-08-03T09:00:00Z",
        },
      ],
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await page.getByRole("button", { name: "メンバー", exact: true }).click();
    await page.getByText("Bob").waitFor();

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
