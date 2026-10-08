import type { Page } from "@playwright/test";

import type {
  BoardDeletion,
  BoardDetail,
  BoardTarget,
  BoardTargetDisplay,
  ErrorResponse,
  SaveSceneRequest,
  SaveSceneResponse,
} from "../../../src/api/types";

import type { ApiMock } from "../api";
import { annotationsOfScene, emptyScene, listEntry, summarize } from "../boardData";
import { boardIdOf, json, ownerOnly } from "./respond";

/** ボードそのもの（一覧・作成・取得・改名・削除）と、シーンの保存・作成先。 */
export async function installBoardRoutes(page: Page, mock: ApiMock): Promise<void> {
  let issued = 0;

  // 新しいボードは作成先を持って生まれる。作成先を選ばないと作れない
  // （ADR 0017）。
  /**
   * 作成のリクエストからボードを組み立てる。
   *
   * **送られてきたシーンをそのまま返す。** ひな形から作ったボードは、開いた
   * ときにその絵が出ていなければ「作れた」と言えない。空のシーンに固定すると、
   * シーンを送り忘れていても緑になる。
   */
  const newBoard = (name: string, target: BoardTarget, scene?: string): BoardDetail => {
    issued += 1;
    return {
      id: `board-new-${issued}`,
      name,
      // 作った本人は必ず owner（ADR 0017）。
      role: "owner",
      createdAt: "2026-08-05T10:00:00Z",
      updatedAt: "2026-08-05T10:00:00Z",
      scene: scene ?? emptyScene(),
      repositoryOwner: target.repositoryOwner,
      repositoryName: target.repositoryName,
      projectId: target.projectId,
      // 表示用の値は任意（ADR 0019 / 0025）。送ってこなければ「知らない」で
      // 残り、リンクはリポジトリの Projects へ落ちる。
      projectNumber: target.projectNumber ?? 0,
      projectTitle: target.projectTitle ?? "",
      projectUrl: target.projectUrl ?? "",
      targetLocked: false,
      sceneOverLimit: false,
    };
  };

  await page.route(
    (url) => url.pathname === "/api/boards",
    async (route) => {
      if (route.request().method() === "POST") {
        const req = route.request().postDataJSON() as {
          name: string;
          scene?: string;
        } & BoardTarget;
        const board = newBoard(req.name, req, req.scene);
        mock.boards = [summarize(board), ...mock.boards];
        mock.details[board.id] = board;
        // **保存されたシーンから注釈を立てる。** サーバーは保存済みシーンを
        // 読んで状態を返す（`internal/CLAUDE.md` の 3 状態のデータフロー）ので、
        // ひな形つきで作ったボードは開いた時点で注釈を 1 つ持つ。空に固定すると、
        // ひな形が注釈になっていなくても緑になる。
        mock.annotations[board.id] ??= annotationsOfScene(board.scene);
        mock.detached[board.id] ??= [];
        await json(route, 201, board);
        return;
      }

      if (mock.boardsError) {
        await json(route, mock.boardsError.status, mock.boardsError.body);
        return;
      }
      await json(
        route,
        200,
        mock.boards.map((b) =>
          mock.unreadableCounts?.includes(b.id)
            ? { ...b, annotationCounts: null }
            : listEntry(b, mock.annotations[b.id] ?? []),
        ),
      );
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+$/.test(url.pathname),
    async (route) => {
      const method = route.request().method();
      // 取得と改名と削除だけを受ける。何でも受けると、フロントが契約と違う
      // メソッドで叩いていても緑になる。
      if (method !== "GET" && method !== "PATCH" && method !== "DELETE") {
        await route.fallback();
        return;
      }

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }

      if (method === "GET") {
        await json(route, 200, detail);
        return;
      }

      if (method === "DELETE") {
        if (mock.deleteError) {
          await json(route, mock.deleteError.status, mock.deleteError.body);
          return;
        }
        if (detail.role !== "owner") {
          await ownerOnly(route);
          return;
        }

        // **本当に消す。** 204 を返すだけのモックにすると、一覧を引き直して
        // いないフロントでも緑になる（ADR 0042）。
        delete mock.details[id];
        delete mock.annotations[id];
        delete mock.detached[id];
        delete mock.deletion?.[id];
        mock.boards = mock.boards.filter((b) => b.id !== id);
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      if (mock.renameError) {
        await json(route, mock.renameError.status, mock.renameError.body);
        return;
      }

      const req = route.request().postDataJSON() as { name?: string };
      const name = (req.name ?? "").trim();
      if (name === "") {
        await json(route, 400, {
          code: "invalid_input",
          error: "name is required",
        } satisfies ErrorResponse);
        return;
      }

      // **版は動かさない**（ADR 0020）。動かすモックにすると、改名のあとに
      // 保存が 409 になる実装でも E2E が緑のままになる。
      const next: BoardDetail = { ...detail, name };
      mock.details[id] = next;
      mock.boards = mock.boards.map((b) => (b.id === id ? summarize(next) : b));
      await json(route, 200, next);
    },
  );

  // 削除で失われるものは、削除とは別の口で引く。押す前に見せるためのもの
  // なので、削除の応答に混ぜられない（ADR 0042）。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/deletion$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }

      // 削除そのものと同じく owner だけ（ADR 0042）。モックだけ緩くすると、
      // 導線が owner 以外に漏れても E2E が緑のまま通る。
      if (detail.role !== "owner") {
        await ownerOnly(route);
        return;
      }

      const reply = mock.deletion?.[id];
      if (reply) {
        await json(route, reply.status, reply.body);
        return;
      }
      await json(route, 200, { recordedItemCount: 0 } satisfies BoardDeletion);
    },
  );

  // 保存は版を照合する。素通しにすると、フロントが基準を送っていなくても、
  // 古い基準を送り続けていても E2E は緑のまま通り、2 回目の保存が実物で初めて
  // 落ちる（ADR 0020）。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/scene$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "PUT") {
        await route.fallback();
        return;
      }

      if (mock.saveSceneError) {
        await json(route, mock.saveSceneError.status, mock.saveSceneError.body);
        return;
      }

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }

      // 基準が無いのは「古い」ではなく「契約から外れている」。サーバーは 400 を
      // 返すので、モックも同じにする。409 に混ぜると、フロントが必須項目を
      // 落としても衝突のテストが通ってしまう。
      const req = route.request().postDataJSON() as Partial<SaveSceneRequest>;
      if (typeof req.baseUpdatedAt !== "string" || req.baseUpdatedAt === "") {
        await json(route, 400, {
          code: "invalid_input",
          error: "baseUpdatedAt is required",
        } satisfies ErrorResponse);
        return;
      }
      if (typeof req.scene !== "string") {
        await json(route, 400, {
          code: "invalid_input",
          error: "scene is required",
        } satisfies ErrorResponse);
        return;
      }

      if (req.baseUpdatedAt !== detail.updatedAt) {
        await json(route, 409, {
          code: "scene_conflict",
          error: "他の人がこのボードを保存しています",
        } satisfies ErrorResponse);
        return;
      }

      // 版を進める。据え置くと、基準を更新し損ねたフロントでも保存し続けられて
      // しまい、照合が効いているように見えるだけになる。
      const next: BoardDetail = {
        ...detail,
        scene: req.scene,
        // 実 API は上限を超えるシーンの保存を拒むので、保存に通ったシーンは
        // 上限内。据え置くと、開き直しても警告が消えない食い違いが残る。
        sceneOverLimit: false,
        updatedAt: new Date(Date.parse(detail.updatedAt) + 1000).toISOString(),
      };
      mock.details[id] = next;
      mock.boards = mock.boards.map((b) => (b.id === id ? summarize(next) : b));
      await json(route, 200, { updatedAt: next.updatedAt } satisfies SaveSceneResponse);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/target$/.test(url.pathname),
    async (route) => {
      // メソッドが違うものは捕まえず、キャッチオールの 500 に落とす。何でも
      // 受けると、フロントが契約と違うメソッドで叩いていても緑になる。
      if (route.request().method() !== "PUT") {
        await route.fallback();
        return;
      }

      if (mock.setTargetError) {
        await json(route, mock.setTargetError.status, mock.setTargetError.body);
        return;
      }

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }

      const target = route.request().postDataJSON() as BoardTarget;
      const next: BoardDetail = {
        ...detail,
        ...target,
        // 表示用の値は任意なので、送られてこなければ「知らない」に落とす。
        // **spread に任せない。** 送られてこないキーは上書きされないので、
        // 前の作成先の値が残る。サーバーは空文字で保存するので、モックだけが
        // 前の Project を指し続けることになる（ADR 0012）。
        projectNumber: target.projectNumber ?? 0,
        projectTitle: target.projectTitle ?? "",
        projectUrl: target.projectUrl ?? "",
      };
      mock.details[id] = next;
      mock.boards = mock.boards.map((b) => (b.id === id ? summarize(next) : b));
      await json(route, 200, next);
    },
  );

  // 表示名だけを取り直す口（ADR 0037）。**作成先そのものは動かさない。**
  // ここで動かせるようにすると、サーバーが固定している値をモックだけが
  // 書き換えられることになり、固定の意味が E2E から消える。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/target\/display$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "PUT") {
        await route.fallback();
        return;
      }

      if (mock.refreshTargetDisplayError) {
        const reply = mock.refreshTargetDisplayError;
        await json(route, reply.status, reply.body);
        return;
      }

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }

      const display = route.request().postDataJSON() as BoardTargetDisplay;
      if (display.projectId !== detail.projectId) {
        await json(route, 409, {
          code: "target_mismatch",
          error: "etoki: board target does not match",
        } satisfies ErrorResponse);
        return;
      }

      const next: BoardDetail = {
        ...detail,
        projectNumber: display.projectNumber ?? 0,
        projectTitle: display.projectTitle ?? "",
        projectUrl: display.projectUrl ?? "",
      };
      mock.details[id] = next;
      mock.boards = mock.boards.map((b) => (b.id === id ? summarize(next) : b));
      await json(route, 200, next);
    },
  );
}
