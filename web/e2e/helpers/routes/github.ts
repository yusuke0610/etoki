import type { Page } from "@playwright/test";

import type { ApiMock } from "../api";
import { json } from "./respond";

/** 作成先の候補（リポジトリと Projects v2）。 */
export async function installGitHubRoutes(page: Page, mock: ApiMock): Promise<void> {
  // 作成先の候補一覧。ボードには紐づかないので、ボードのルートとは分けて置く。
  await page.route(
    (url) => url.pathname === "/api/github/repositories",
    async (route) => {
      await json(route, mock.repositories.status, mock.repositories.body);
    },
  );

  await page.route(
    (url) => /^\/api\/github\/repositories\/[^/]+\/[^/]+\/projects$/.test(url.pathname),
    async (route) => {
      const segments = new URL(route.request().url()).pathname.split("/");
      // /api/github/repositories/<owner>/<repo>/projects
      const key = `${segments[4]}/${segments[5]}`;
      const reply = mock.projects[key] ?? { status: 200, body: [] };
      await json(route, reply.status, reply.body);
    },
  );

  // 作成に要るフィールドの確認（ADR 0086）。**GET だけを受ける。** ほかの
  // メソッドはキャッチオールに落とす（`.claude/rules/e2e-mocks.md`）。
  await page.route(
    (url) => /^\/api\/github\/projects\/[^/]+\/fields$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") return route.fallback();

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
