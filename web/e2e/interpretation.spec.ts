import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, createdRun } from "./helpers/fixtures";

const BOARD_NAME = "認証まわりのブレスト";

test.describe("解釈と作成", () => {
  test("解釈するまで作成のボタンは出ない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    // 中核思想 3。開くだけで GitHub に何かが起きる導線があってはならない。
    await expect(page.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toBeVisible();
  });

  test("解釈すると summary と epic / issue の階層が出る", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    // summary は GitHub には作らない。どう読んだかを見せるためだけに出す。
    await expect(
      card.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();

    const epic = card.locator("li").filter({ hasText: "ログイン基盤" }).first();
    await expect(epic.getByText("epic", { exact: true }).first()).toBeVisible();
    await expect(epic.getByText("メールとパスワードでログインする")).toBeVisible();
    await expect(epic.getByText("ログイン失敗を数える")).toBeVisible();
  });

  test("作成すると件数が出て、状態が作成済みに変わる", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await expect(card.getByText("未作成")).toBeVisible();

    await card.getByRole("button", { name: "解釈する" }).click();

    // 作成後の再取得で返る状態を先に差し替えておく。
    mock.annotations[BOARD_ID] = [
      {
        id: "frame-uncreated",
        name: "ログイン",
        granularity: "",
        state: "created",
        lastSyncedAt: "2026-08-05T10:00:00Z",
        items: createdRun().items,
      },
    ];

    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    await expect(card.getByText("3 件を作成しました。")).toBeVisible();
    await expect(card.getByText("作成済み")).toBeVisible();
  });

  test("途中で失敗した run は、作れた件数と理由を両方出す", async ({ page }) => {
    const mock = baseMock();
    mock.createItems = {
      status: 201,
      body: {
        ...createdRun(),
        items: createdRun().items.slice(0, 1),
        incomplete: true,
        error: "github: rate limited",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // 何も作られていないと誤解させると、再実行で draft issue が重複する。
    const result = card.locator(".creation-result");
    await expect(result.getByText("途中で失敗しました（1 件は作成済み）")).toBeVisible();
    await expect(result.getByText("github: rate limited")).toBeVisible();
    // 解釈結果にも同じタイトルが並ぶので、作成結果の側だけを見る。
    await expect(result.getByText("ログイン基盤")).toBeVisible();
    await expect(result.getByText("ログイン失敗を数える")).toHaveCount(0);
  });

  test("LLM が未設定なら、その注釈の中だけにエラーを出す", async ({ page }) => {
    const mock = baseMock();
    mock.interpret = {
      status: 503,
      body: {
        error: "llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    await expect(card.getByText("llm is not configured", { exact: false })).toBeVisible();
    // 画面全体のエラー表示に流すと、どの注釈で起きたか分からなくなる。
    await expect(page.getByRole("alert")).toHaveCount(0);
  });

  test("解釈時点とシーンが食い違うと、作成は 409 で止まる", async ({ page }) => {
    const mock = baseMock();
    mock.createItems = {
      status: 409,
      body: { error: "etoki: content hash mismatch" },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();
    await card.getByRole("button", { name: "GitHub に作成する" }).click();

    // 解釈のやり直しは開発者が決める。ここで勝手に解釈し直して作成を続けない。
    await expect(card.getByText("etoki: content hash mismatch")).toBeVisible();
    await expect(card.getByText("未作成")).toBeVisible();
  });
});
