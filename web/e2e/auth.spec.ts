import { expect, test } from "@playwright/test";

import { installApi } from "./helpers/api";
import { chooseTarget } from "./helpers/board";
import { AUTHORIZE_URL, baseMock, signedIn } from "./helpers/fixtures";

/** 認証を設定した構成のモック。既定は未ログイン。 */
function withAuth() {
  const mock = baseMock();
  mock.session = { status: 200, body: { authRequired: true, authenticated: false } };
  return mock;
}

test.describe("ログイン", () => {
  // 認証を設定していない構成の挙動は変えない。PAT だけで動く（ADR 0015）。
  test("認証を設定していなければログインを求めない", async ({ page }) => {
    await installApi(page, baseMock());
    await page.goto("/");

    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toHaveCount(0);
    await expect(page.getByLabel("ボード名")).toBeVisible();
  });

  test("認証を設定していて未ログインなら、ログイン画面を出す", async ({ page }) => {
    await installApi(page, withAuth());
    await page.goto("/");

    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toBeVisible();
    // ボードには触らせない。裏で 401 が出るだけになる。
    await expect(page.getByLabel("ボード名")).toHaveCount(0);
  });

  // 認可画面そのものは外部。遷移したことだけを確かめる（ADR 0012）。
  test("ログインを押すと認可画面へ送り出す", async ({ page }) => {
    await installApi(page, withAuth());

    // github.test には行かせない。要求が出たことだけを見て止める。
    let sentTo = "";
    await page.route(
      (url) => url.host === "github.test",
      async (route) => {
        sentTo = route.request().url();
        await route.fulfill({
          status: 200,
          contentType: "text/html",
          body: "<html></html>",
        });
      },
    );

    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).click();

    await expect.poll(() => sentTo).toBe(AUTHORIZE_URL);
  });

  test("ログイン済みなら、ボード一覧が使える", async ({ page }) => {
    const mock = withAuth();
    mock.session = { status: 200, body: signedIn() };

    await installApi(page, mock);
    await page.goto("/");

    await expect(page.getByLabel("ボード名")).toBeVisible();
    await expect(page.getByText("Octo Cat")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "認証まわりのブレスト" }),
    ).toBeVisible();
  });

  test("ログアウトするとログイン画面に戻る", async ({ page }) => {
    const mock = withAuth();
    mock.session = { status: 200, body: signedIn() };

    await installApi(page, mock);
    await page.goto("/");
    await expect(page.getByLabel("ボード名")).toBeVisible();

    await page.getByRole("button", { name: "ログアウト" }).click();

    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toBeVisible();
    await expect(page.getByLabel("ボード名")).toHaveCount(0);
  });

  // 状態が分からないならログインを求めない側に倒す。求める側に倒すと、認証を
  // 設定していない構成が API の一時的な失敗で使えなくなる。
  test("ログイン状態を取れなくても、画面は使える状態にする", async ({ page }) => {
    const mock = baseMock();
    mock.session = { status: 500, body: { code: "internal", error: "internal error" } };

    await installApi(page, mock);
    await page.goto("/");

    await expect(page.getByRole("alert")).toContainText(
      "ログイン状態を取得できませんでした",
    );
    await expect(page.getByLabel("ボード名")).toBeVisible();
  });

  test("ログインを開始できなければ、その旨をログイン画面に出す", async ({ page }) => {
    const mock = withAuth();
    mock.login = {
      status: 503,
      body: { code: "auth_not_configured", error: "authentication is not configured" },
    };

    await installApi(page, mock);
    await page.goto("/");
    await page.getByRole("button", { name: "GitHub でログイン" }).click();

    await expect(page.getByRole("alert")).toContainText("ログインを開始できませんでした");
  });

  // 使っている最中に失効すると、画面はログイン済みのまま操作できなくなる。
  // 401 を掴んだら状態を読み直し、ログイン画面へ戻す。
  test("使っている最中にセッションが切れたら、ログイン画面へ戻す", async ({ page }) => {
    const mock = baseMock();
    mock.session = { status: 200, body: signedIn() };

    await installApi(page, mock);
    await page.goto("/");

    // まずログイン済みで入れること。ここを確かめないと、最初から未ログイン
    // だっただけのテストになる。
    await expect(page.getByLabel("ボード名")).toBeVisible();
    // **一覧が読めたことまで待つ。** フォームは一覧の取得を待たずに出るので、
    // ここで待たないと最初の取得が下の失効の後に着地しうる。そうなると
    // ログイン画面へ落ちるのは「読み直す操作をさせたから」ではなくなり、
    // このテストが見たい経路を通らないまま緑にも赤にもなる。
    await expect(
      page.locator(".board-list").getByRole("button", { name: "認証まわりのブレスト" }),
    ).toBeVisible();

    // ここで失効させる。一覧の取得だけが 401 を返し、状態を読み直すと
    // 未ログインになる、という実際の並びを作る。
    mock.boardsError = {
      status: 401,
      body: { code: "login_required", error: "login required" },
    };
    mock.session = { status: 200, body: { authRequired: true, authenticated: false } };

    // 状態はサーバーに訊き直す。画面だけログイン画面に切り替える実装でも
    // 見た目は同じになるので、問い合わせが起きたことを別に縛る。
    const sessionRefetch = page.waitForRequest(
      (req) =>
        req.method() === "GET" && new URL(req.url()).pathname === "/api/auth/session",
    );

    // 一覧を読み直す操作をさせる。作成そのものは通り、続く再取得で失効に気づく。
    // 作成先を選ばないとボードは作られないので、選択まで進める（ADR 0017）。
    await page.getByLabel("ボード名").fill("失効の確認");
    await page.getByRole("button", { name: "次へ" }).click();
    await chooseTarget(page, "acme/web", "#1 ロードマップ");
    await sessionRefetch;

    await expect(page.getByRole("button", { name: "GitHub でログイン" })).toBeVisible();
    // 失効はエラーではないので、赤い帯を出したまま残さない。
    await expect(page.getByRole("alert")).toHaveCount(0);
  });
});
