import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { annotationCard, openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

/**
 * 設定していない機能の見せ方（ADR 0030）。
 *
 * LLM や GitHub を設定しなくても etoki は起動する（ADR 0008）。**その構成で
 * 使っている人に、押す前に「いまできないこと」を見せる。** 押してから 503 の
 * 生の文字列が出るのでは、何を設定すればよいか決められない。
 *
 * 押した後の 503 と同じ文言が出ることまで見る。別々に持つと片方だけ古くなる。
 */

const BOARD_NAME = "認証まわりのブレスト";

test.describe("設定していない機能", () => {
  test("LLM が未設定なら、押す前に解釈できないことと理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    // 画面が案内するだけでなく、叩けば 503 が返る構成そのものを再現する。
    mock.interpret = {
      status: 503,
      body: {
        code: "llm_not_configured",
        error: "llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    const interpret = card.getByRole("button", { name: "解釈する" });

    // 黙って消さない。押せないことと、何を設定すればよいかを両方出す。
    await expect(interpret).toBeVisible();
    await expect(interpret).toBeDisabled();

    // 理由はパネルに 1 つ。注釈の数だけ並べない。
    const reason = page.getByText("ETOKI_LLM_API_KEY");
    await expect(reason).toBeVisible();
    await expect(reason).toHaveCount(1);
    // 押せないボタンはフォーカスも当たらない。読み上げに理由が届くよう、
    // ボタンからこの文を指しておく（既存の「保存してから」と同じ形）。
    await expect(interpret).toHaveAttribute(
      "aria-describedby",
      "interpretation-unavailable",
    );
  });

  test("GitHub が未設定なら、作成の代わりに理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: true, diagramDraft: true, creation: false, sharing: true },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await card.getByRole("button", { name: "解釈する" }).click();

    // 解釈はできる。**ブレストと解釈まで進めることが読めている**必要がある。
    await expect(
      card.getByText("ログインの入口まわりを 1 つの epic として読みました。"),
    ).toBeVisible();
    await expect(card.getByRole("button", { name: "GitHub に作成する" })).toHaveCount(0);
    await expect(card.getByText("ETOKI_GITHUB_TOKEN")).toBeVisible();
    await expect(card.getByText("ブレストと解釈はこのまま続けられます。")).toBeVisible();
  });

  // 表示名の取り直しは GitHub の Project 一覧を引く（ADR 0037）。設定して
  // いない構成では押しても引けないので、押させずに理由を出す。
  test("GitHub が未設定なら、作成先の名前も取り直させない", async ({ page }) => {
    const mock = baseMock();
    mock.details[BOARD_ID] = { ...board(), targetLocked: true };
    mock.capabilities = {
      status: 200,
      body: { interpretation: true, diagramDraft: true, creation: false, sharing: true },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(
      page.getByRole("button", { name: "作成先の名前を取り直す" }),
    ).toHaveCount(0);
    // 確定していることは変わらず読める。理由は押した後に返る 503 と同じ文言。
    await expect(page.getByText("作成先は確定（draft issue を作成済み）")).toBeVisible();
    await expect(page.getByText("ETOKI_GITHUB_TOKEN").first()).toBeVisible();
  });

  test("共有が未設定なら、メンバーのボタンの代わりに理由を出す", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: true, diagramDraft: true, creation: true, sharing: false },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(page.getByRole("button", { name: "メンバー", exact: true })).toHaveCount(
      0,
    );
    await expect(page.getByText("共有には認証の設定が必要です")).toBeVisible();
  });

  // 確かめられなかったことを「使えない」として見せない（中核思想 3）。
  // これまでどおり押せて、押した後に理由が出る。
  test("使える機能を引けなくても、操作は止めない", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = { status: 500, body: { code: "internal", error: "boom" } };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    const card = annotationCard(page, "ログイン");
    await expect(card.getByRole("button", { name: "解釈する" })).toBeEnabled();
    await expect(
      page.getByRole("button", { name: "メンバー", exact: true }),
    ).toBeVisible();
  });
});
