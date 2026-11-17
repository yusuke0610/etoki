import type { Page, Route } from "@playwright/test";

import type { ErrorResponse } from "../../../src/api/types";

import type { ApiMock } from "../api";
import { json } from "./respond";

/**
 * セッションを未ログインに戻す。ログアウトとアカウントの削除が通る。
 *
 * authRequired は元のまま保つ。ここで true に固定すると、認証を設定していない
 * 構成のテストが黙って別の構成に変わる。**文書へのリンクも残す。** 実物は
 * ログインの有無にかかわらず、設定された URL を返す（ADR 0101）。
 */
function signOut(mock: ApiMock): void {
  const body = mock.session.body;
  if (!("authRequired" in body)) {
    mock.session = { status: 200, body: { authRequired: true, authenticated: false } };
    return;
  }
  mock.session = {
    status: 200,
    body: {
      authRequired: body.authRequired,
      authenticated: false,
      ...(body.termsUrl === undefined ? {} : { termsUrl: body.termsUrl }),
      ...(body.privacyUrl === undefined ? {} : { privacyUrl: body.privacyUrl }),
    },
  };
}

/** 利用者を消す口を扱えない構成の応答。実物は認証が無ければ 503 を返す。 */
async function accountDeletionNotConfigured(route: Route): Promise<void> {
  await json(route, 503, {
    code: "account_deletion_not_configured",
    error: "account deletion requires authentication",
  } satisfies ErrorResponse);
}

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

  // 認証の口も契約のメソッドだけを受ける。何でも受けると、フロントが違う
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
      signOut(mock);
      await route.fulfill({ status: 204, body: "" });
    },
  );

  // 利用者を消す口（ADR 0101）。**`mock.account` が無ければ 503。** capabilities の
  // `accountDeletion` と揃える（`withAccountDeletion`）。
  await page.route(
    (url) => url.pathname === "/api/account/deletion",
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      if (!mock.account) {
        await accountDeletionNotConfigured(route);
        return;
      }
      await json(route, mock.account.deletion.status, mock.account.deletion.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/account",
    async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.fallback();
        return;
      }
      const account = mock.account;
      if (!account) {
        await accountDeletionNotConfigured(route);
        return;
      }
      if (account.deleteError) {
        await json(route, account.deleteError.status, account.deleteError.body);
        return;
      }
      // 実物はセッションも消す。次の session の問い合わせはログインしていない。
      account.deleted++;
      signOut(mock);
      await route.fulfill({ status: 204, body: "" });
    },
  );
}
