import type { Page } from "@playwright/test";

import type { ApiMock } from "../api";
import { json } from "./respond";

/** 使える機能とログイン状態。どの画面よりも先に引かれる。 */
export async function installSessionRoutes(page: Page, mock: ApiMock): Promise<void> {
  await page.route(
    (url) => url.pathname === "/api/capabilities",
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      await json(route, mock.capabilities.status, mock.capabilities.body);
    },
  );

  // 認証の 3 本も契約のメソッドだけを受ける。何でも受けると、フロントが違う
  // メソッドで叩いていても E2E は緑のまま通り、実物で初めて落ちる。
  await page.route(
    (url) => url.pathname === "/api/auth/session",
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      await json(route, mock.session.status, mock.session.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/login",
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }
      await json(route, mock.login.status, mock.login.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/logout",
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

      // ログアウトしたら未ログインに戻す。次の session の問い合わせに効く。
      // authRequired は元のまま保つ。ここで true に固定すると、認証を
      // 設定していない構成のテストが黙って別の構成に変わる。
      const authRequired =
        "authRequired" in mock.session.body ? mock.session.body.authRequired : true;
      mock.session = { status: 200, body: { authRequired, authenticated: false } };
      await route.fulfill({ status: 204, body: "" });
    },
  );
}
