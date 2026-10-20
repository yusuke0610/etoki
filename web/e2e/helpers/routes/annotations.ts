import type { Page } from "@playwright/test";

import type {
  BoardAnnotations,
  Interpretation,
  InterpretationJob,
  InterpretRequest,
} from "../../../src/api/types";

import type { ApiMock } from "../api";
import { boardIdOf, json } from "./respond";

/** 注釈の状態と、注釈ごとの解釈・作成・実行の履歴。 */
export async function installAnnotationRoutes(page: Page, mock: ApiMock): Promise<void> {
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations$/.test(url.pathname),
    async (route) => {
      const id = boardIdOf(route);
      // **応答は 1 つ。** サーバーは畳み込みをボード全体で引いており、シーンに
      // 残っていないぶんも同じ問い合わせで返る（#111）。
      const body: BoardAnnotations = {
        annotations: mock.annotations[id] ?? [],
        detached: mock.detached[id] ?? [],
      };
      await json(route, 200, body);
    },
  );

  // 実行の履歴（ADR 0007）。畳み込み（注釈の items）とは別の口。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/runs$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }

      const annotationId = new URL(route.request().url()).pathname.split("/")[5] ?? "";
      const reply = mock.runs?.[annotationId] ?? { status: 200, body: [] };
      await json(route, reply.status, reply.body);
    },
  );

  // 解釈は始めるのと受け取るのが別のリクエスト（ADR 0085）。始めたジョブは
  // ここで覚え、取りに来たら `mock.interpret` の応答で終わらせる。
  // サーバーと同じく、ジョブはボードで絞る。始めたときのボードを控える。
  const jobs = new Map<string, { boardId: string; annotationId: string }>();
  let seq = 0;
  const jobOf = (
    id: string,
    annotationId: string,
    withResult: boolean,
  ): InterpretationJob => {
    if (mock.interpretJobRunning) {
      return { id, annotationId, status: "running", startedAt: "2026-10-19T09:00:00Z" };
    }
    return {
      id,
      annotationId,
      status: "succeeded",
      startedAt: "2026-10-19T09:00:00Z",
      // 2xx のときだけジョブが作られるので、本文は解釈の結果。
      ...(withResult ? { result: mock.interpret.body as Interpretation } : {}),
    };
  };

  // 末尾一致にしない。パスを間違えても一致してしまい、契約から外れた呼び出しが
  // 緑のまま通る。取りこぼしはキャッチオールが 500 で拾う。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/interpret$/.test(url.pathname),
    async (route) => {
      // POST 以外は捕まえず、キャッチオールの 500 に落とす。何でも受けると、
      // フロントが契約と違うメソッドで叩いていても緑になる。
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

      mock.interpretRequests.push(
        (route.request().postDataJSON() ?? {}) as InterpretRequest,
      );
      // 始める時点で弾かれる失敗（429 など）は、そのまま返す。
      if (mock.interpret.status < 200 || mock.interpret.status >= 300) {
        await json(route, mock.interpret.status, mock.interpret.body);
        return;
      }

      const segments = new URL(route.request().url()).pathname.split("/");
      const boardId = segments[3] ?? "";
      const annotationId = segments[5] ?? "";
      // 実行中の同じ注釈には、新しく始めず走っているジョブを返す（実物と同じ）。
      const running = mock.interpretJobRunning
        ? [...jobs].find(
            ([, job]) => job.boardId === boardId && job.annotationId === annotationId,
          )?.[0]
        : undefined;
      let id = running;
      if (id === undefined) {
        seq += 1;
        id = `job-${seq}`;
        jobs.set(id, { boardId, annotationId });
      }
      await json(route, 202, {
        id,
        annotationId,
        status: "running",
        startedAt: "2026-10-19T09:00:00Z",
      } satisfies InterpretationJob);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/interpretation-jobs$/.test(url.pathname),
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
          .map(([id, job]) => jobOf(id, job.annotationId, false)),
      );
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/interpretation-jobs\/[^/]+$/.test(url.pathname),
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
        mock.jobDiscards.push(id);
        jobs.delete(id);
        await route.fulfill({ status: 204 });
        return;
      }
      await json(route, 200, jobOf(id, job.annotationId, true));
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/items$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

      mock.createRequests.push((route.request().postDataJSON() ?? {}) as Interpretation);
      await json(route, mock.createItems.status, mock.createItems.body);
    },
  );
}
