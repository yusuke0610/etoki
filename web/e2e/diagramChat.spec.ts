import { expect, test, type Page } from "@playwright/test";

import { installApi } from "./helpers/api";
import { openBoard } from "./helpers/board";
import { BOARD_ID, baseMock, board } from "./helpers/fixtures";

/**
 * プロンプトから図のドラフトを作るチャット（ADR 0041）。
 *
 * **確かめるのは画面側の約束。** 生成した瞬間にキャンバスへ流し込まないこと、
 * 置いても保存しないこと、未保存でも使えること、viewer に出さないこと。
 * LLM とのやりとりそのものはユースケース層の単体テストの担当。
 */

const BOARD_NAME = "認証まわりのブレスト";

/** チャットを開く。 */
async function openChat(page: Page): Promise<void> {
  await page.getByRole("button", { name: "図のドラフト", exact: true }).click();
  await expect(page.getByRole("heading", { name: "図のドラフト" })).toBeVisible();
}

test.describe("図のドラフト", () => {
  // #58 の原則。**AI が作るのはドラフトで、保存も構造分解もさせない。**
  // 生成した瞬間に流し込むと、開発者が見てから決める形（中核思想 3）が消える。
  test("生成しただけではキャンバスが変わらず、「置く」で初めて置かれる", async ({
    page,
  }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();

    // mermaid のまま見せる。置く前の確認は文字で足りる。
    await expect(page.locator(".diagram-mermaid")).toContainText("flowchart TD");
    // **まだキャンバスは変わっていない。** 変わっていれば未保存になる。
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    await page.getByRole("button", { name: "キャンバスに置く" }).click();

    // 置いたので未保存になる。**置くだけで保存はしない**（中核思想 3）。
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();
    expect(mock.details[BOARD_ID]?.scene).toBe(board().scene);

    // 保存して初めてシーンに載る。既存の frame 3 つはそのまま残っている
    // （**既存の要素には一切触らない**、#58 の原則）。
    await page.getByRole("button", { name: "保存", exact: true }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeHidden();

    const saved = JSON.parse(mock.details[BOARD_ID]?.scene ?? "{}") as {
      elements: { type: string; customData?: unknown }[];
    };
    const kinds = saved.elements.map((el) => el.type);
    expect(kinds.filter((t) => t === "frame")).toHaveLength(3);
    // ドラフトのぶんが増えている。矩形と矢印になるので、どちらも 1 つ以上。
    expect(kinds.filter((t) => t === "rectangle").length).toBeGreaterThan(0);
    expect(kinds.filter((t) => t === "arrow").length).toBeGreaterThan(0);
    // **生成物に customData.etoki を付けない**（#58 の原則）。付けると bot が
    // 描いた絵がそのまま解釈の対象になる。
    for (const el of saved.elements) {
      if (el.type === "frame") continue;
      expect(el.customData ?? null).toBeNull();
    }
  });

  // **未保存でも使える**（ADR 0041）。保存済みシーンを読まないので、解釈の
  // 「保存してから解釈できます」と同じ制約をかける理由が無い。
  test("未保存のままでも生成できる", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    // 置いて未保存にしたうえで、続けて直せること。
    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();
    await page.getByRole("button", { name: "キャンバスに置く" }).click();
    await expect(page.getByText("未保存", { exact: true })).toBeVisible();

    await page.getByLabel("図への指示").fill("返金の分岐も足して");
    await page.getByRole("button", { name: "直す" }).click();

    // **2 回目にしか出ない表示で待つ。** mermaid は 1 回目から出ているので、
    // それを待っても 2 回目が届いたことにはならない。
    await expect(page.getByText("返金の分岐も足して")).toBeVisible();
    expect(mock.diagramRequests).toHaveLength(2);
  });

  // **サーバーは会話を持たない**（ADR 0041）。続きを頼むときは、ここまでの
  // やりとりを毎回まるごと送る。送れていないと、直す土台が消える。
  test("直すときは、ここまでのやりとりをまるごと送る", async ({ page }) => {
    const mock = await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();
    await expect(page.locator(".diagram-mermaid")).toBeVisible();

    await page.getByLabel("図への指示").fill("返金の分岐も足して");
    await page.getByRole("button", { name: "直す" }).click();
    await expect(page.getByText("返金の分岐も足して")).toBeVisible();

    // 1 回目は履歴なし。2 回目は 1 回目の指示と返った図が載る。
    expect(mock.diagramRequests[0]?.history ?? []).toHaveLength(0);
    const second = mock.diagramRequests[1];
    expect(second?.prompt).toBe("返金の分岐も足して");
    expect(second?.history).toHaveLength(1);
    expect(second?.history?.[0]?.prompt).toBe("注文から出荷までの流れ");
    expect(second?.history?.[0]?.mermaid).toContain("flowchart TD");
  });

  // 積み上げた指示は前の種類の図に対するもの。引き継ぐと、シーケンス図への
  // 指示をフローチャートの続きとして送ることになる。
  test("種類を変えると会話ごと捨てる", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();
    await expect(page.locator(".diagram-mermaid")).toBeVisible();

    await page.getByLabel("図の種類").selectOption("sequence");

    await expect(page.locator(".diagram-mermaid")).toHaveCount(0);
    await expect(page.getByRole("button", { name: "生成", exact: true })).toBeVisible();
  });

  // 会話が長くなりすぎたら 413（ADR 0041）。**古いやりとりはこちらでは捨てない**
  // ので、やり直すかどうかを選べる文言にする。
  test("会話が長すぎると、やり直せることを言う", async ({ page }) => {
    const mock = baseMock();
    mock.diagramDraft = {
      status: 413,
      body: { code: "diagram_chat_too_long", error: "etoki: diagram chat is too long" },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();

    await expect(page.getByText("会話が長くなりすぎました")).toBeVisible();
    // **書いた指示は消さない。** 消すと、上限に当たったことを知った時点で
    // 打ち直しになる。やり直すかどうかを選ぶ材料も一緒に消える。
    await expect(page.getByLabel("図への指示")).toHaveValue("注文から出荷までの流れ");
  });

  // 実行の上限は解釈と 1 つの枠を共有する（ADR 0043）。**文言が「解釈」だけを
  // 指していると、こちら側で見たときに食い違う。**
  test("実行の上限に当たったら、解釈と共通の枠だと分かる文言を出す", async ({ page }) => {
    const mock = baseMock();
    mock.diagramDraft = {
      status: 429,
      body: {
        code: "concurrency_limited",
        error: "etoki: too many concurrent llm calls: 1 running, limit is 1",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    await page.getByLabel("図への指示").fill("注文から出荷までの流れ");
    await page.getByRole("button", { name: "生成", exact: true }).click();

    await expect(page.getByText("実行中です", { exact: false })).toBeVisible();
    // 書いた指示は消さない。413 と同じ扱いで、待って押し直せる。
    await expect(page.getByLabel("図への指示")).toHaveValue("注文から出荷までの流れ");
  });

  // 図が返らなかったのと、LLM を呼べなかったのは打ち手が違う。畳むと画面は
  // どちらも「失敗しました」としか言えない（ADR 0034）。
  test("図が返らなかったときは、頼み方を変えるよう案内する", async ({ page }) => {
    const mock = baseMock();
    mock.diagramDraft = {
      status: 502,
      body: {
        code: "diagram_failed",
        error: "etoki: llm output did not contain a diagram",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    await page.getByLabel("図への指示").fill("なにかいい感じに");
    await page.getByRole("button", { name: "生成", exact: true }).click();

    await expect(page.getByText("指示を具体的にして")).toBeVisible();
  });

  // 押す前に理由を出す（ADR 0030）。**ボタンを黙って消さない**（中核思想 3）。
  test("LLM が未設定なら、開いた先で理由を出して押させない", async ({ page }) => {
    const mock = baseMock();
    mock.capabilities = {
      status: 200,
      body: { interpretation: false, diagramDraft: false, creation: true, sharing: true },
    };
    mock.diagramDraft = {
      status: 503,
      body: {
        code: "llm_not_configured",
        error: "llm is not configured: set ETOKI_LLM_API_KEY or ETOKI_LLM_BASE_URL",
      },
    };
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);
    await openChat(page);

    const send = page.getByRole("button", { name: "生成", exact: true });
    await expect(send).toBeVisible();
    await expect(send).toBeDisabled();

    await expect(
      page.locator(".diagram-chat").getByText("ETOKI_LLM_API_KEY"),
    ).toBeVisible();
    // 押せないボタンはフォーカスも当たらない。読み上げに理由が届くよう指す。
    await expect(send).toHaveAttribute("aria-describedby", "diagram-unavailable");
  });

  // 生成は LLM を叩く外部呼び出しで課金も伴う。**viewer には出さない**
  // （ADR 0017、解釈と同じ理由）。
  test("viewer には出さない", async ({ page }) => {
    const mock = baseMock();
    const viewer = { ...board(), role: "viewer" as const };
    mock.details[BOARD_ID] = viewer;
    mock.boards = [{ ...(mock.boards[0] ?? {}), ...viewer, role: "viewer" }];
    await installApi(page, mock);

    await page.goto("/");
    await openBoard(page, BOARD_NAME);

    await expect(
      page.getByRole("button", { name: "図のドラフト", exact: true }),
    ).toHaveCount(0);
  });
});
