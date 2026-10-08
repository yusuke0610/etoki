import type { Page } from "@playwright/test";

import type { GenerateDiagramRequest } from "../../../src/api/types";

import type { ApiMock } from "../api";
import { json } from "./respond";

/** 図のドラフトの生成（ADR 0041）。 */
export async function installDiagramRoutes(page: Page, mock: ApiMock): Promise<void> {
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/diagram-draft$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

      mock.diagramRequests.push(
        (route.request().postDataJSON() ?? {}) as GenerateDiagramRequest,
      );
      await json(route, mock.diagramDraft.status, mock.diagramDraft.body);
    },
  );
}
