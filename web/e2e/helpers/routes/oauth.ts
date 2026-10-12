import type { Page, Route } from "@playwright/test";

import type { ErrorResponse, OAuthDecisionRequest } from "../../../src/api/types";

import type { ApiMock } from "../api";
import { json } from "./respond";

/** 扱えない構成の応答。実物は認可サーバーを組み立てていなければ 503 を返す。 */
async function notConfigured(route: Route): Promise<void> {
  await json(route, 503, {
    code: "mcp_connections_not_configured",
    error: "mcp connections require authentication",
  } satisfies ErrorResponse);
}

/**
 * MCP のクライアントへの許可（ADR 0076）。
 *
 * **`mock.connections` が無ければ 503。** capabilities の `mcpConnections` と
 * 揃える（`ApiMock.capabilities` の約束）。片方だけ有効にすると、画面が案内
 * しないのに通る、という実物では起きない組み合わせを緑にする。
 */
export async function installOAuthRoutes(page: Page, mock: ApiMock): Promise<void> {
  await page.route(
    (url) => url.pathname === "/api/oauth/authorization",
    async (route) => {
      const c = mock.connections;
      const method = route.request().method();
      if (method !== "GET" && method !== "POST") {
        await route.fallback();
        return;
      }
      if (!c) {
        await notConfigured(route);
        return;
      }

      if (method === "GET") {
        // 要求はクエリ文字列のまま運ばれる。画面が中を読み替えていないかは
        // ここに積んだ値で見る。
        c.authorizationQueries.push(
          new URL(route.request().url()).searchParams.get("request") ?? "",
        );
        await json(route, c.authorization.status, c.authorization.body);
        return;
      }

      const body = route.request().postDataJSON() as OAuthDecisionRequest;
      // サーバーと同じく、要求と返事のどちらかが欠けた本文は受けない。
      if (typeof body.request !== "string" || typeof body.approve !== "boolean") {
        await json(route, 400, {
          code: "invalid_input",
          error: "request and approve are required",
        } satisfies ErrorResponse);
        return;
      }
      c.decisions.push(body);
      await json(route, c.decision.status, c.decision.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/oauth/grants",
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      const c = mock.connections;
      if (!c) {
        await notConfigured(route);
        return;
      }
      await json(route, 200, c.grants);
    },
  );

  await page.route(
    (url) => /^\/api\/oauth\/grants\/[^/]+$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.fallback();
        return;
      }
      const c = mock.connections;
      if (!c) {
        await notConfigured(route);
        return;
      }
      const id = decodeURIComponent(
        new URL(route.request().url()).pathname.split("/")[4] ?? "",
      );
      // 他人の許可や消えた許可は実物と同じく 404。
      if (!c.grants.some((g) => g.id === id)) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }
      c.grants = c.grants.filter((g) => g.id !== id);
      c.revoked.push(id);
      await route.fulfill({ status: 204, body: "" });
    },
  );
}
