import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import {
  ANNOTATION_IDS,
  BOARD_ID,
  baseMock,
  mixedFramesMock,
  multiFrameMock,
} from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

test.describe("注釈の状態", () => {
  test("3 状態がバッジとして出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(annotationCard(page, "ログイン").getByText("未作成")).toBeVisible();
    await expect(
      annotationCard(page, "パスワード再設定").getByText("作成済み"),
    ).toBeVisible();
    await expect(
      annotationCard(page, "セッション管理").getByText("変更あり"),
    ).toBeVisible();
  });

  test("粒度はサーバーが返した値が選ばれている", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 空文字は「指定なし」。粒度の判断を LLM に任せることを表す。
    await expect(annotationCard(page, "ログイン").getByLabel("粒度")).toHaveValue("");
    await expect(annotationCard(page, "パスワード再設定").getByLabel("粒度")).toHaveValue(
      "epic",
    );
    await expect(annotationCard(page, "セッション管理").getByLabel("粒度")).toHaveValue(
      "issue",
    );
  });

  test("GitHub にある項目は畳まれていて、開くと中身が出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    const item = card.getByText("再設定メールを送る");

    await expect(item).toBeHidden();
    await card.getByText("GitHub にある 2 件").click();
    await expect(item).toBeVisible();
  });

  // 逆方向同期は実装しないので、作成時に記録したものが唯一の手がかり
  // （ADR 0023）。
  test("GitHub にある項目の本文が読める", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await card.getByText("GitHub にある 2 件").click();

    const item = card.locator("li").filter({ hasText: "再設定メールを送る" }).last();
    await item.getByText("本文", { exact: true }).click();
    await expect(item.getByText("有効期限つきのリンクを送る")).toBeVisible();
  });

  // 記録を始める前に作った item は本文を持たない。GitHub からは取り直せない
  // ので、無いことをそのまま出す。
  test("本文を記録していない項目は、無いことが分かる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByText("GitHub にある 1 件").click();

    await expect(card.getByText("本文なし")).toBeVisible();
  });

  // 名前を付けていない frame は Excalidraw 側も `Frame` としか描かないので、
  // 名前を頼りにすると全部同じ見出しで並ぶ（ADR 0022）。
  test("名前の無い注釈は一覧上の位置で採番して出す", async ({ page }) => {
    await installApi(page, multiFrameMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(annotationCard(page, "注釈 2")).toBeVisible();
  });

  // 押す → updateScene → onChange → 選択の反映、の一周が実ブラウザで
  // 通ることを見る。ここが切れると、どのカードがどのフレームか分からない
  // という元の状態に戻る。
  test("カードを押すとキャンバスでそのフレームが選ばれ、カードが強調される", async ({
    page,
  }) => {
    await installApi(page, multiFrameMock());

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "注釈 2");
    await expect(card).not.toHaveAttribute("aria-current", "true");

    await card.getByRole("button", { name: "注釈 2" }).click();

    await expect(card).toHaveAttribute("aria-current", "true");
    await expect(annotationCard(page, "ログイン")).not.toHaveAttribute(
      "aria-current",
      "true",
    );
    // 選択とスクロールは appState の話で、保存すべき変更ではない。
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();
  });

  // 注釈の frame とただの frame は混在するのが前提（ルートの CLAUDE.md）。
  // 見分ける手段が無いと、状態を見せるという中核思想 3 に届かない。
  test("注釈にした frame だけがキャンバス上で印を持つ", async ({ page }) => {
    await installApi(page, mixedFramesMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const marks = page.locator(".annotation-overlay-frame");
    await expect(marks).toHaveCount(2);
    // 粒度も見分けられる。パネルを開かないと epic か issue か分からない状態に
    // しない。
    await expect(marks.getByText("注釈 epic")).toBeVisible();
    await expect(marks.getByText("注釈", { exact: true })).toBeVisible();
  });

  // 印はキャンバスの外に重ねているので、スクロールやズームに自分では追従
  // しない。Excalidraw の onChange で引き直している。ここが切れると、枠だけが
  // 元の位置に取り残される。
  test("キャンバスをスクロールすると印も一緒に動く", async ({ page }) => {
    await installApi(page, mixedFramesMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const mark = page.locator(".annotation-overlay-frame").first();
    const before = await mark.boundingBox();
    if (!before) throw new Error("印が出ていない");

    const canvas = page.locator(".excalidraw canvas").first();
    const box = await canvas.boundingBox();
    if (!box) throw new Error("キャンバスが表示されていない");
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.wheel(0, 200);

    await expect.poll(async () => (await mark.boundingBox())?.y).toBeLessThan(before.y);
  });

  // 印は重ねているだけで、frame そのものは変えていない。外した跡が残らない
  // ことも同じ理由で確かめる。
  test("注釈を外すと印も消える", async ({ page }) => {
    await installApi(page, mixedFramesMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // カードを押すとキャンバスでそのフレームが選ばれ、パネルに外す口が出る。
    await annotationCard(page, "ログイン")
      .getByRole("button", { name: "ログイン" })
      .click();
    await page.getByRole("button", { name: /の注釈を外す/ }).click();

    await expect(page.locator(".annotation-overlay-frame")).toHaveCount(1);
  });

  test("注釈が無いボードでは案内を出す", async ({ page }) => {
    const mock = baseMock();
    mock.annotations[BOARD_ID] = [];
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByText("保存済みの注釈はありません。")).toBeVisible();
  });
});

/**
 * 実行の履歴（ADR 0007）。
 *
 * **「GitHub にある N 件」とは別物。** あちらは run 履歴を畳んだ「いま在るもの」で、
 * こちらは 1 回ずつの記録。答えている問いが違う（ADR 0026）。
 */
test.describe("実行の履歴", () => {
  /** 2 回に分けて作った注釈の履歴。新しい順で返る。 */
  function withRuns() {
    const mock = baseMock();
    mock.runs = {
      [ANNOTATION_IDS.created]: {
        status: 200,
        body: [
          {
            id: 2,
            createdAt: "2026-08-04T12:00:00Z",
            items: [
              {
                itemId: "PVTI_issue",
                kind: "issue",
                title: "再設定メールを送る",
                body: "有効期限つきのリンクを送る",
                localId: "i1",
                action: "created",
              },
            ],
          },
          {
            id: 1,
            createdAt: "2026-08-03T12:00:00Z",
            items: [
              {
                itemId: "PVTI_epic",
                kind: "epic",
                title: "パスワード再設定",
                body: "忘れたときの導線をまとめる",
                localId: "e1",
                action: "created",
              },
            ],
          },
        ],
      },
    };
    return mock;
  }

  // **押されるまで引かない**（中核思想 3）。開いただけで引くと、注釈の数だけ
  // 問い合わせが増える。
  test("履歴は押したときだけ引かれる", async ({ page }) => {
    let requests = 0;
    page.on("request", (req) => {
      if (new URL(req.url()).pathname.endsWith("/runs")) requests += 1;
    });

    await installApi(page, withRuns());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "パスワード再設定");
    await expect(card.getByText("実行の履歴")).toBeVisible();
    expect(requests).toBe(0);

    await card.getByText("実行の履歴").click();
    await card.getByRole("button", { name: "履歴を読み込む" }).click();

    // 畳み込み（「GitHub にある N 件」）にも同じタイトルが並ぶので、履歴の中に
    // 絞って見る。**同じ文字列を別の問いに対する答えとして出している**ことが
    // ここで分かる（ADR 0026）。
    const history = card.locator(".run-history");
    await expect(history.getByText("再設定メールを送る")).toBeVisible();
    await expect(history.getByText("パスワード再設定")).toBeVisible();
    expect(requests).toBe(1);
  });

  // 一度も実行していない注釈に履歴の枠を出さない。常に出すと、空の枠が
  // 注釈の数だけ並ぶ。
  test("未実行の注釈には履歴を出さない", async ({ page }) => {
    await installApi(page, withRuns());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(annotationCard(page, "ログイン").getByText("実行の履歴")).toHaveCount(0);
  });
});
