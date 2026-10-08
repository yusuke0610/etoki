import type { Page } from "@playwright/test";

import type {
  BoardAnnotations,
  Interpretation,
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
      await json(route, mock.interpret.status, mock.interpret.body);
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
