import type { Page } from "@playwright/test";

import type {
  DiagramDraft,
  DiagramJob,
  ErrorResponse,
  GenerateDiagramRequest,
} from "../../../src/api/types";

import type { ApiMock } from "../api";
import { json } from "./respond";

/**
 * 図のドラフトの生成（ADR 0041）。始めるのと受け取るのが別のリクエスト
 * （ADR 0091）。
 *
 * 始めたジョブはここで覚え、取りに来たら `mock.diagramDraft` の応答で終わらせる。
 * **502 は始める時点では返さず、ジョブの失敗にする。** 実物では LLM を叩いてから
 * 分かる失敗で、始める応答の契約に 502 は無い。それ以外の失敗（400 / 413 / 429 /
 * 503 など）は始める時点で返す。
 */
export async function installDiagramRoutes(page: Page, mock: ApiMock): Promise<void> {
  // サーバーと同じく、ジョブはボードで絞る。始めたときのボードと入力を控える。
  const jobs = new Map<string, { boardId: string; request: GenerateDiagramRequest }>();
  let seq = 0;

  const jobOf = (
    id: string,
    request: GenerateDiagramRequest,
    withResult: boolean,
  ): DiagramJob => {
    const base = { id, request, startedAt: "2026-10-22T09:00:00Z" };
    if (mock.diagramJobRunning) return { ...base, status: "running" };
    if (mock.diagramDraft.status === 502) {
      return {
        ...base,
        status: "failed",
        error: mock.diagramDraft.body as ErrorResponse,
      };
    }
    return {
      ...base,
      status: "succeeded",
      ...(withResult ? { result: mock.diagramDraft.body as DiagramDraft } : {}),
    };
  };

  // 末尾一致にしない。パスを間違えても一致してしまい、契約から外れた呼び出しが
  // 緑のまま通る。取りこぼしはキャッチオールが 500 で拾う。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/diagram-draft$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

      const request = (route.request().postDataJSON() ?? {}) as GenerateDiagramRequest;
      mock.diagramRequests.push(request);
      // 始める時点で弾かれる失敗は、そのまま返す。
      const { status } = mock.diagramDraft;
      if (status !== 502 && (status < 200 || status >= 300)) {
        await json(route, status, mock.diagramDraft.body);
        return;
      }

      const boardId = new URL(route.request().url()).pathname.split("/")[3] ?? "";
      // 同じ入力が走っていれば、新しく始めず走っているジョブを返す（実物と同じ）。
      const same = JSON.stringify(request);
      const running = mock.diagramJobRunning
        ? [...jobs].find(
            ([, job]) => job.boardId === boardId && JSON.stringify(job.request) === same,
          )?.[0]
        : undefined;
      let id = running;
      if (id === undefined) {
        seq += 1;
        id = `diagram-job-${seq}`;
        jobs.set(id, { boardId, request });
      }
      await json(route, 202, {
        id,
        request,
        status: "running",
        startedAt: "2026-10-22T09:00:00Z",
      } satisfies DiagramJob);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/diagram-jobs$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      const boardId = new URL(route.request().url()).pathname.split("/")[3] ?? "";
      await json(
        route,
        200,
        [...jobs]
          .filter(([, job]) => job.boardId === boardId)
          .map(([id, job]) => jobOf(id, job.request, false)),
      );
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/diagram-jobs\/[^/]+$/.test(url.pathname),
    async (route) => {
      const segments = new URL(route.request().url()).pathname.split("/");
      const id = segments[5] ?? "";
      const job = jobs.get(id);
      const method = route.request().method();

      if (method !== "GET" && method !== "DELETE") {
        await route.fallback();
        return;
      }
      // 知らない ID も別のボードのジョブも 404（実物の find と同じ）。
      if (job === undefined || job.boardId !== segments[3]) {
        await json(route, 404, { code: "not_found", error: "not found" });
        return;
      }
      if (method === "DELETE") {
        mock.diagramJobDiscards.push(id);
        jobs.delete(id);
        await route.fulfill({ status: 204 });
        return;
      }
      await json(route, 200, jobOf(id, job.request, true));
    },
  );
}
