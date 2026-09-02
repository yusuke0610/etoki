import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { matchedInterpretationMock } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

// 3 状態判定の changed には、これまで「重複を作るか、何もしないか」しか出口が
// 無かった（ADR 0026）。作る前に何が起きるのかを見せる。
test.describe("changed の注釈を更新する", () => {
  test("書き換える項目には更新の印が付く", async ({ page }) => {
    await installApi(page, matchedInterpretationMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    // 更新は前の内容を消す。作成と同じ見た目にしない。
    //
    // **印だけに絞って引く。** 同じ行には切り替えの選択肢（「更新する」）も
    // 並ぶので、前方一致で引くと 2 つ見つかる。
    const updating = card.locator(".draft-item").filter({
      has: page.getByLabel("i1 のタイトル"),
    });
    await expect(updating.getByText("更新", { exact: true })).toBeVisible();

    const creating = card.locator(".draft-item").filter({
      has: page.getByLabel("i2 のタイトル"),
    });
    await expect(creating.getByText("更新", { exact: true })).toBeHidden();
  });

  // draft issue は削除できない。etoki にできるのは「残ります」と見せるところまで。
  test("今回書き換わらないものを取り残しとして出す", async ({ page }) => {
    await installApi(page, matchedInterpretationMock());
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
    await installApi(page, matchedInterpretationMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    await card.getByLabel("i1 を作成する").uncheck();

    const leftBehind = card.locator(".left-behind");
    await expect(leftBehind).toContainText("2 件");
    await expect(leftBehind).toContainText("セッションの有効期限");
  });

  // 対応づけを解釈させるのは LLM でも、決めるのは開発者（ADR 0026）。指す先が
  // GitHub から消えていると、更新のままでは作成が必ず 502 になり、解釈を
  // やり直しても同じところで止まりうる。ここが無いと出口が項目ごと外すことしか
  // 無くなる。
  test("更新をやめて新しく作るに切り替えられる", async ({ page }) => {
    const mock = await installApi(page, matchedInterpretationMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    const updating = card.locator(".draft-item").filter({
      has: page.getByLabel("i1 のタイトル"),
    });
    await expect(updating.getByText("更新", { exact: true })).toBeVisible();

    await card
      .getByLabel("i1 を更新するか新しく作るか")
      .selectOption({ label: "新しく作る" });

    // 印は消える。LLM が言ったこととの差は残す。
    await expect(updating.getByText("更新", { exact: true })).toBeHidden();
    await expect(
      updating.getByText("解釈では既存の draft issue の更新でした"),
    ).toBeVisible();

    // そこへは書かないので、更新先は取り残しに戻る（押す前に見せる）。
    const leftBehind = card.locator(".left-behind");
    await expect(leftBehind).toContainText("2 件");
    await expect(leftBehind).toContainText("セッションの有効期限");

    await card.getByRole("button", { name: "GitHub に作成する" }).click();
    await expect(card.getByText("件を作成しました。")).toBeVisible();

    expect(mock.createRequests).toHaveLength(1);
    const sent = mock.createRequests[0]?.items ?? [];
    expect(sent.map((it) => [it.localId, it.previousItemId])).toEqual([
      ["i1", undefined],
      ["i2", undefined],
    ]);
  });

  // LLM が「新しく作る」と答えた項目には指す先が無い。選ばせるものが無い。
  test("新規の項目には切り替えを出さない", async ({ page }) => {
    await installApi(page, matchedInterpretationMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "セッション管理");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByLabel("i1 を更新するか新しく作るか")).toBeVisible();
    await expect(card.getByLabel("i2 を更新するか新しく作るか")).toBeHidden();
  });

  // 件数だけでは GitHub 側に何が増えたのか分からない。更新は増えない。
  test("作成結果に作成と更新の内訳が出る", async ({ page }) => {
    const mock = matchedInterpretationMock();
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
    const mock = await installApi(page, matchedInterpretationMock());
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
