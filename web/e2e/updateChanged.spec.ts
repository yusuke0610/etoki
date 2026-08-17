import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { ANNOTATION_IDS, BOARD_ID, baseMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

/**
 * `changed` の注釈で、LLM が前回ぶんとの対応づけを返した解釈。
 *
 * `i1` は前回の `PVTI_old` を書き換える。`i2` は新しく作る。前回ぶんには
 * `PVTI_kept` もあるが、今回はどこからも指されないので取り残しになる。
 */
function withMatchedInterpretation() {
  const mock = baseMock();

  mock.annotations[BOARD_ID] = (mock.annotations[BOARD_ID] ?? []).map((a) =>
    a.id !== ANNOTATION_IDS.changed
      ? a
      : {
          ...a,
          items: [
            {
              itemId: "PVTI_old",
              kind: "issue",
              title: "セッションの有効期限",
              body: "",
              localId: "i9",
              action: "created",
            },
            {
              itemId: "PVTI_kept",
              kind: "issue",
              title: "触らないほう",
              body: "",
              localId: "i8",
              action: "created",
            },
          ],
        },
  );

  mock.interpret = {
    status: 200,
    body: {
      summary: "前回の続きとして読みました。",
      contentHash: "sha256:e2e",
      items: [
        {
          localId: "i1",
          kind: "issue",
          title: "セッションの有効期限を延ばす",
          body: "書き直した本文",
          previousItemId: "PVTI_old",
        },
        { localId: "i2", kind: "issue", title: "新しく足す issue", body: "" },
      ],
    },
  };

  return mock;
}

// 3 状態判定の changed には、これまで「重複を作るか、何もしないか」しか出口が
// 無かった（ADR 0026）。作る前に何が起きるのかを見せる。
test.describe("changed の注釈を更新する", () => {
  test("書き換える項目には更新の印が付く", async ({ page }) => {
    await installApi(page, withMatchedInterpretation());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    // 更新は前の内容を消す。作成と同じ見た目にしない。
    const updating = card.locator(".draft-item").filter({
      has: page.getByLabel("i1 のタイトル"),
    });
    await expect(updating.getByText("更新")).toBeVisible();

    const creating = card.locator(".draft-item").filter({
      has: page.getByLabel("i2 のタイトル"),
    });
    await expect(creating.getByText("更新")).toBeHidden();
  });

  // draft issue は削除できない。etoki にできるのは「残ります」と見せるところまで。
  test("今回書き換わらないものを取り残しとして出す", async ({ page }) => {
    await installApi(page, withMatchedInterpretation());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    const leftBehind = card.locator(".left-behind");
    await expect(leftBehind).toContainText("1 件");
    await expect(leftBehind).toContainText("触らないほう");
    // 書き換わるほうは取り残しではない。
    await expect(leftBehind).not.toContainText("セッションの有効期限");
  });

  // 外した項目は作られないので、その更新先は取り残しに戻る。押す前に見せる。
  test("更新する項目を外すと取り残しが増える", async ({ page }) => {
    await installApi(page, withMatchedInterpretation());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("i1 を作成する").uncheck();

    const leftBehind = card.locator(".left-behind");
    await expect(leftBehind).toContainText("2 件");
    await expect(leftBehind).toContainText("セッションの有効期限");
  });

  // 件数だけでは GitHub 側に何が増えたのか分からない。更新は増えない。
  test("作成結果に作成と更新の内訳が出る", async ({ page }) => {
    const mock = withMatchedInterpretation();
    mock.createItems = {
      status: 201,
      body: {
        runId: 9,
        createdAt: "2026-08-05T12:00:00Z",
        items: [
          {
            itemId: "PVTI_old",
            kind: "issue",
            title: "セッションの有効期限を延ばす",
            body: "書き直した本文",
            localId: "i1",
            action: "updated",
          },
          {
            itemId: "PVTI_new",
            kind: "issue",
            title: "新しく足す issue",
            body: "",
            localId: "i2",
            action: "created",
          },
        ],
      },
    };

    await installApi(page, mock);
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    const result = card.locator(".creation-result");
    await expect(result).toContainText("1 件を作成し、1 件を更新しました");
    // 書き換えた行にだけ印が付く。
    await expect(
      result
        .locator("li")
        .filter({ hasText: "セッションの有効期限を延ばす" })
        .getByText("更新"),
    ).toBeVisible();
  });

  test("対応づけは作成リクエストにそのまま載る", async ({ page }) => {
    const mock = await installApi(page, withMatchedInterpretation());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();
    await expect(card.getByText("件を作成しました。")).toBeVisible();

    expect(mock.createRequests).toHaveLength(1);
    const sent = mock.createRequests[0]?.items ?? [];
    expect(sent.map((it) => [it.localId, it.previousItemId])).toEqual([
      ["i1", "PVTI_old"],
      ["i2", undefined],
    ]);
  });
});
