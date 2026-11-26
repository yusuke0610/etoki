import type { Page } from "@playwright/test";

import type {
  BoardAccess,
  BoardMember,
  BoardRole,
  ErrorResponse,
  Invitee,
  InviteMemberRequest,
} from "../../../src/api/types";

import type { ApiMock } from "../api";
import { boardIdOf, json } from "./respond";

/** ボードの権限とメンバー（ADR 0017）。 */
export async function installMemberRoutes(page: Page, mock: ApiMock): Promise<void> {
  // 権限はボードの取得とは別に訊かれる。GitHub が未設定・不通でもボードは
  // 開ける必要があるため（ADR 0017）。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/access$/.test(url.pathname),
    async (route) => {
      // 契約は GET だけ。ほかはキャッチオールに落とす（`.claude/rules/e2e-mocks.md`）。
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }

      const id = boardIdOf(route);
      const role = mock.details[id]?.role ?? "owner";
      const configured = mock.access?.[id];
      // **断る応答は、作成先が未選択でも先に返す**（サーバーと同じ）。サーバーは
      // 権限の判定が落ちると、作成先を見る前にその失敗を返す（`member.go` の
      // `h.fail`）。未選択の分岐より後に置くと、未選択のボードで 403 / 500 の画面を
      // 確かめられない（#481 の full review）。
      if (configured && configured.status >= 400) {
        await json(route, configured.status, configured.body);
        return;
      }
      // **作成先が未選択なら、決めておいた答えより先に unknown を返す**（サーバーと
      // 同じ、ADR 0017）。作成先はボードを開いたまま選ぶ（ADR 0100）ので、決めて
      // おいた答えを未選択のうちから返すと、選んだあとに訊き直さない画面でも緑になる。
      if (mock.details[id]?.projectId === "") {
        await json(route, 200, { role, projectAccess: "unknown" } satisfies BoardAccess);
        return;
      }

      if (configured) {
        await json(route, configured.status, configured.body);
        return;
      }

      await json(route, 200, { role, projectAccess: "unknown" } satisfies BoardAccess);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/members$/.test(url.pathname),
    async (route) => {
      const id = boardIdOf(route);
      mock.members ??= {};
      mock.members[id] ??= [];

      if (route.request().method() === "POST") {
        if (mock.inviteError) {
          await json(route, mock.inviteError.status, mock.inviteError.body);
          return;
        }

        const req = route.request().postDataJSON() as InviteMemberRequest;
        // サーバーと同じく、確認した相手を指さない招待は受けない。受けると、
        // 画面が確認を飛ばして送っていても緑になる。
        if (!req.userId) {
          await json(route, 400, {
            code: "invalid_input",
            error: "etoki: invalid input: userId is required",
          } satisfies ErrorResponse);
          return;
        }
        const member: BoardMember = {
          userId: `user-${req.login}`,
          login: req.login,
          displayName: req.login,
          role: req.role,
          createdAt: "2026-08-05T10:00:00Z",
        };
        mock.members[id] = [...mock.members[id], member];
        await json(route, 201, member);
        return;
      }

      // GET 以外を一覧で答えない。何でも受けると、フロントが契約と違う
      // メソッドで叩いていても緑になる。
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }

      if (mock.membersListError) {
        await json(route, mock.membersListError.status, mock.membersListError.body);
        return;
      }

      await json(route, 200, mock.members[id]);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/invitee$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      if (mock.lookupInviteeError) {
        await json(route, mock.lookupInviteeError.status, mock.lookupInviteeError.body);
        return;
      }

      const login = new URL(route.request().url()).searchParams.get("login") ?? "";
      const invitee: Invitee = {
        userId: `user-${login}`,
        login,
        displayName: login,
        lastSignedInAt: "2026-08-01T09:30:00Z",
      };
      await json(route, 200, invitee);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/members\/[^/]+$/.test(url.pathname),
    async (route) => {
      const id = boardIdOf(route);
      const userId = new URL(route.request().url()).pathname.split("/").pop() ?? "";
      mock.members ??= {};
      mock.members[id] ??= [];

      if (route.request().method() === "DELETE") {
        mock.members[id] = mock.members[id].filter((m) => m.userId !== userId);
        await route.fulfill({ status: 204, body: "" });
        return;
      }

      // ロールの変更は PUT だけ。他は捕まえずキャッチオールの 500 に落とす。
      if (route.request().method() !== "PUT") {
        await route.fallback();
        return;
      }

      const req = route.request().postDataJSON() as { role: BoardRole };
      let updated: BoardMember | undefined;
      mock.members[id] = mock.members[id].map((m) => {
        if (m.userId !== userId) return m;
        updated = { ...m, role: req.role };
        return updated;
      });
      if (!updated) {
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }
      await json(route, 200, updated);
    },
  );
}
