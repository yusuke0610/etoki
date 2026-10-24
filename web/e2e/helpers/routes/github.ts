import type { Page, Route } from "@playwright/test";

import type { ErrorResponse } from "../../../src/api/types";

import type { ApiMock } from "../api";
import { json } from "./respond";

/**
 * GitHub が未設定の構成か。capabilities の `creation` で決める。
 *
 * **このファイルの口もそれに揃えて 503 を返す**（`ApiMock.capabilities` の約束）。
 * 実物は GitHub を設定していなければ候補もフィールドの確認も 503 を返す。成功を
 * 返したままにすると、未設定の構成で候補を引きにいく実装が緑になる。
 */
export function githubUnset(mock: ApiMock): boolean {
  const caps = mock.capabilities.body;
  return mock.capabilities.status === 200 && "creation" in caps && !caps.creation;
}

async function githubNotConfigured(route: Route): Promise<void> {
  await json(route, 503, {
    code: "github_not_configured",
    error: "github is not configured",
  } satisfies ErrorResponse);
}

/** 作成先の候補（リポジトリと Projects v2）。 */
export async function installGitHubRoutes(page: Page, mock: ApiMock): Promise<void> {
  // 作成先の候補一覧。ボードには紐づかないので、ボードのルートとは分けて置く。
  await page.route(
    (url) => url.pathname === "/api/github/repositories",
    async (route) => {
      // **GET だけを受ける**（`.claude/rules/e2e-mocks.md`）。下の 2 つも同じ。
      if (route.request().method() !== "GET") return route.fallback();
      if (githubUnset(mock)) return githubNotConfigured(route);
      await json(route, mock.repositories.status, mock.repositories.body);
    },
  );

  await page.route(
    (url) => /^\/api\/github\/repositories\/[^/]+\/[^/]+\/projects$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") return route.fallback();
      if (githubUnset(mock)) return githubNotConfigured(route);
      const segments = new URL(route.request().url()).pathname.split("/");
      // /api/github/repositories/<owner>/<repo>/projects
      const key = `${segments[4]}/${segments[5]}`;
      const reply = mock.projects[key] ?? { status: 200, body: [] };
      await json(route, reply.status, reply.body);
    },
  );

  // 作成に要るフィールドの確認（ADR 0088）。**GET だけを受ける。** ほかの
  // メソッドはキャッチオールに落とす（`.claude/rules/e2e-mocks.md`）。
  await page.route(
    (url) => /^\/api\/github\/projects\/[^/]+\/fields$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") return route.fallback();
      if (githubUnset(mock)) return githubNotConfigured(route);

      const segments = new URL(route.request().url()).pathname.split("/");
      // /api/github/projects/<projectId>/fields
      const projectId = decodeURIComponent(segments[4] ?? "");
      (mock.projectFieldsRequests ??= []).push(projectId);
      const reply = mock.projectFields?.[projectId] ?? {
        status: 200,
        body: { projectId, status: "ok", missingFields: [] },
      };
      await json(route, reply.status, reply.body);
    },
  );
}
