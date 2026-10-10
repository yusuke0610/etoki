import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { boardListHeading } from "./helpers/board";
import {
  AUTHORIZE_URL,
  CONSENT_PATH,
  CONSENT_REDIRECT_URI,
  CONSENT_REQUEST,
  authRequiredMock,
  baseMock,
  withConnections,
} from "./helpers/fixtures";

/**
 * MCP のクライアントへの許可（ADR 0076）の画面側の約束。
 *
 * 認可サーバーそのもの（コード・トークン・失効の規則）は Go のテストが持つ。
 * ここで見るのは、同意を人に選ばせることと、取り消しの口があること。
 */

/** クライアントの戻り先には行かせない。届いたことだけを見る。 */
async function stubClientCallback(
  page: import("@playwright/test").Page,
): Promise<string[]> {
  const reached: string[] = [];
  await page.route(
    (url) => url.origin === new URL(CONSENT_REDIRECT_URI).origin,
    async (route) => {
      reached.push(route.request().url());
      await route.fulfill({
        status: 200,
        contentType: "text/html",
        body: "<html></html>",
      });
    },
  );
  return reached;
}

test.describe("同意の画面", () => {
  // 未ログインで来た人は、ログインを経て同意の画面に戻る。**戻り先に要求ごと
  // 渡す。** 渡さないと、ログインした先が一覧になり、クライアントは待ちぼうけになる。
  test("未ログインなら、要求ごと戻り先にしてログインへ送る", async ({ page }) => {
    await installApi(page, authRequiredMock());
    await page.route(
      (url) => url.host === "github.test",
      (route) =>
        route.fulfill({ status: 200, contentType: "text/html", body: "<html></html>" }),
    );

    await page.goto(CONSENT_PATH);
    const login = page.waitForRequest(
      (req) =>
        req.method() === "POST" && new URL(req.url()).pathname === "/api/auth/login",
    );
    await page.getByRole("button", { name: "GitHub でログイン" }).click();

    expect((await login).postDataJSON()).toEqual({ returnTo: CONSENT_PATH });
    await page.waitForURL(AUTHORIZE_URL);
  });

  test("誰が何をしようとしているかを見せ、許可したらクライアントへ戻す", async ({
    page,
  }) => {
    const mock = await installApi(page, withConnections(baseMock()));
    const reached = await stubClientCallback(page);

    await page.goto(CONSENT_PATH);

    await expect(
      page.getByRole("heading", { name: "MCP のクライアントからの接続" }),
    ).toBeVisible();
    await expect(page.locator(".consent-client")).toHaveText("Claude Code");
    // 名前は自称であることを隠さない。
    await expect(
      page.getByText("名前はクライアントが名乗ったもので、etoki は確かめていません。"),
    ).toBeVisible();
    await expect(page.getByText(CONSENT_REDIRECT_URI)).toBeVisible();
    await expect(
      page.getByText("自分で始めた接続でなければ許可しないでください。", {
        exact: false,
      }),
    ).toBeVisible();
    // 同意の画面ではボードを開かず、URL も書き換えない。
    await expect(boardListHeading(page)).toHaveCount(0);
    expect(new URL(page.url()).search).toBe(new URL(CONSENT_PATH, page.url()).search);
    // 要求は中を読まずにそのまま送る。開発時は StrictMode が effect を 2 回
    // 走らせるので、回数ではなく中身を見る。
    const queries = mock.connections?.authorizationQueries ?? [];
    expect(queries.length).toBeGreaterThan(0);
    expect(new Set(queries)).toEqual(new Set([CONSENT_REQUEST]));

    const decided = page.waitForRequest(
      (req) =>
        req.method() === "POST" &&
        new URL(req.url()).pathname === "/api/oauth/authorization",
    );
    await page.getByRole("button", { name: "許可する" }).click();
    await decided;

    await expect.poll(() => reached.length).toBe(1);
    expect(reached[0]).toBe(`${CONSENT_REDIRECT_URI}?code=c1&state=st-1`);
    expect(mock.connections?.decisions).toEqual([
      { request: CONSENT_REQUEST, approve: true },
    ]);
  });

  test("許可しなければ、断ったことを送ってクライアントへ戻す", async ({ page }) => {
    const mock = withConnections(baseMock());
    mock.connections!.decision = {
      status: 200,
      body: { redirectTo: `${CONSENT_REDIRECT_URI}?error=access_denied&state=st-1` },
    };
    await installApi(page, mock);
    const reached = await stubClientCallback(page);

    await page.goto(CONSENT_PATH);
    await page.getByRole("button", { name: "許可しない" }).click();

    await expect.poll(() => reached.length).toBe(1);
    expect(reached[0]).toContain("error=access_denied");
    expect(mock.connections?.decisions).toEqual([
      { request: CONSENT_REQUEST, approve: false },
    ]);
  });

  // URL で名乗ったクライアントは、取りに行った先のドメインを出せる。
  test("URL で名乗ったクライアントは出どころを見せる", async ({ page }) => {
    const mock = withConnections(baseMock());
    mock.connections!.authorization = {
      status: 200,
      body: {
        clientId: "https://client.example/oauth/metadata.json",
        clientName: "Example Agent",
        clientIdIsUrl: true,
        redirectUri: CONSENT_REDIRECT_URI,
        scope: "read",
      },
    };
    await installApi(page, mock);

    await page.goto(CONSENT_PATH);

    await expect(page.getByText("client.example が名乗っています。")).toBeVisible();
  });

  // 要求の誤りはクライアントに戻さず画面に出す（戻り先を確かめる前に戻すと
  // オープンリダイレクトになる）。行き止まりにしないよう、一覧へ戻る口を置く。
  test("受けられない要求は理由を出し、一覧へ戻る口を置く", async ({ page }) => {
    const mock = withConnections(baseMock());
    mock.connections!.authorization = {
      status: 400,
      body: {
        code: "invalid_input",
        error: "etoki: invalid input: code_challenge_method must be S256",
      },
    };
    await installApi(page, mock);

    await page.goto(CONSENT_PATH);

    await expect(page.getByRole("alert")).toContainText("接続の要求を読めませんでした");
    await expect(page.getByRole("button", { name: "許可する" })).toHaveCount(0);
    await page.getByRole("button", { name: "ボード一覧へ" }).click();
    await expect(boardListHeading(page)).toBeVisible();
    expect(new URL(page.url()).search).not.toContain("authorize");
  });
});

test.describe("MCP の接続", () => {
  test("利用者のメニューから開き、接続を取り消せる", async ({ page }) => {
    const mock = await installApi(page, withConnections(baseMock()));
    await page.goto("/");

    await page.getByRole("button", { name: "Octo Cat" }).click();
    await page.getByRole("button", { name: "MCP の接続" }).click();

    const dialog = page.getByRole("dialog", { name: "MCP の接続" });
    await expect(dialog.getByText("Claude Code")).toBeVisible();
    await expect(dialog.getByText("Example Agent")).toBeVisible();
    // URL で名乗ったものは出どころを、そうでないものは自称であることを出す。
    await expect(
      dialog.getByText("client.example が名乗る", { exact: false }),
    ).toBeVisible();
    await expect(dialog.getByText("名前は自称", { exact: false })).toBeVisible();

    const revoked = page.waitForRequest(
      (req) =>
        req.method() === "DELETE" &&
        new URL(req.url()).pathname === "/api/oauth/grants/grant-1",
    );
    await dialog.getByRole("button", { name: "Claude Code の接続を取り消す" }).click();
    await revoked;

    await expect(dialog.getByText("Claude Code")).toHaveCount(0);
    await expect(dialog.getByText("Example Agent")).toBeVisible();
    expect(mock.connections?.revoked).toEqual(["grant-1"]);

    // 閉じたら、メニューを開くボタンへ焦点を戻す（押した項目はしまったメニューの中）。
    await dialog.getByRole("button", { name: "閉じる" }).click();
    await expect(dialog).toBeHidden();
    await expect(page.getByRole("button", { name: "Octo Cat" })).toBeFocused();
  });

  // 開くたびに引き直す。接続はブラウザの外で増える。
  test("開き直すと一覧を引き直す", async ({ page }) => {
    const mock = await installApi(page, withConnections(baseMock()));
    await page.goto("/");
    await page.getByRole("button", { name: "Octo Cat" }).click();
    await page.getByRole("button", { name: "MCP の接続" }).click();
    const dialog = page.getByRole("dialog", { name: "MCP の接続" });
    await expect(dialog.getByText("Claude Code")).toBeVisible();
    await dialog.getByRole("button", { name: "閉じる" }).click();

    mock.connections!.grants = [];
    await page.getByRole("button", { name: "Octo Cat" }).click();
    await page.getByRole("button", { name: "MCP の接続" }).click();

    await expect(dialog.getByText("許可している接続はありません。")).toBeVisible();
  });
});
