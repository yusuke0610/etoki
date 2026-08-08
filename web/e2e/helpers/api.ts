import type { Page, Route } from "@playwright/test";

import type {
  AnnotationStatus,
  BoardDetail,
  BoardSummary,
  BoardTarget,
  CreatedRun,
  ErrorResponse,
  Interpretation,
  LoginResponse,
  Project,
  Repository,
  SessionStatus,
} from "../../src/api/types";

/**
 * 応答を 1 つ表す。ステータスと本文を組で持つ。
 *
 * 本文の型は契約の生成物（`src/api/types.ts`）から取る。モックだけが古い形の
 * まま緑になる、という E2E の典型的な嘘を型で塞ぐのが狙い（ADR 0011）。
 */
export type Reply<T> = { status: number; body: T | ErrorResponse };

/**
 * 差し替える応答一式。
 *
 * `installApi` は毎リクエストここを読み直す。テストの途中で書き換えれば、
 * 「作成したら状態が変わる」といった時間軸のある振る舞いを表現できる。
 */
export type ApiMock = {
  boards: BoardSummary[];
  /** ボード ID をキーにした詳細。 */
  details: Record<string, BoardDetail>;
  /** ボード ID をキーにした注釈の状態。 */
  annotations: Record<string, AnnotationStatus[]>;
  interpret: Reply<Interpretation>;
  createItems: Reply<CreatedRun>;
  /** 作成先の候補。リポジトリ選択の画面が読む。 */
  repositories: Reply<Repository[]>;
  /** `owner/name` をキーにした Projects v2。 */
  projects: Record<string, Reply<Project[]>>;
  /**
   * ログイン状態。既定は「認証を設定していない」。
   *
   * これを足さないと、認証が入った時点で全 spec がキャッチオールの 500 に
   * 落ちる。アプリは起動時に必ずここを引く。
   */
  session: Reply<SessionStatus>;
  /** ログイン開始が返す URL。 */
  login: Reply<LoginResponse>;
  /** 一覧取得を失敗させたいときに指定する。 */
  boardsError?: Reply<never>;
  /** 作成先の設定を失敗させたいときに指定する。409 の見せ方を確かめる用。 */
  setTargetError?: Reply<never>;
};

async function json(route: Route, status: number, body: unknown): Promise<void> {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

/**
 * API をモックに差し替える。返り値を書き換えると次のリクエストから反映される。
 *
 * マッチングはパス名の述語で行う。パスの途中に api を含むグロブにすると、Vite が
 * 配信する `/src/api/types.ts` まで巻き込んで傍受してしまい、アプリが読み込め
 * なくなる。
 */
export async function installApi(page: Page, mock: ApiMock): Promise<ApiMock> {
  let issued = 0;

  // 新しいボードは作成先が未選択で始まる。開くとリポジトリ選択に入る
  // （ADR 0014）。
  const newBoard = (name: string): BoardDetail => {
    issued += 1;
    return {
      id: `board-new-${issued}`,
      name,
      createdAt: "2026-08-05T10:00:00Z",
      updatedAt: "2026-08-05T10:00:00Z",
      scene: emptyScene(),
      repositoryOwner: "",
      repositoryName: "",
      projectId: "",
      targetLocked: false,
    };
  };

  // 登録は必ず待つ。`page.goto` より前に済んでいないと、最初の一覧取得だけが
  // モックをすり抜ける。
  //
  // Playwright のルートは後勝ち。取りこぼしを気づけるよう、最初に全部を
  // 500 で受けるものを置いてから個別のルートを重ねる。素通しにすると Vite の
  // プロキシ越しに存在しないバックエンドへ飛び、失敗の原因が読めなくなる。
  await page.route(
    (url) => url.pathname.startsWith("/api/") || url.pathname === "/healthz",
    (route) =>
      json(route, 500, {
        error: `モックされていないリクエスト: ${route.request().method()} ${new URL(route.request().url()).pathname}`,
      } satisfies ErrorResponse),
  );

  await page.route(
    (url) => url.pathname === "/api/boards",
    async (route) => {
      if (route.request().method() === "POST") {
        const req = route.request().postDataJSON() as { name: string };
        const board = newBoard(req.name);
        mock.boards = [summarize(board), ...mock.boards];
        mock.details[board.id] = board;
        mock.annotations[board.id] ??= [];
        await json(route, 201, board);
        return;
      }

      if (mock.boardsError) {
        await json(route, mock.boardsError.status, mock.boardsError.body);
        return;
      }
      await json(route, 200, mock.boards);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+$/.test(url.pathname),
    async (route) => {
      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, { error: "not found" } satisfies ErrorResponse);
        return;
      }
      await json(route, 200, detail);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/scene$/.test(url.pathname),
    (route) => route.fulfill({ status: 204, body: "" }),
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/target$/.test(url.pathname),
    async (route) => {
      if (mock.setTargetError) {
        await json(route, mock.setTargetError.status, mock.setTargetError.body);
        return;
      }

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, { error: "not found" } satisfies ErrorResponse);
        return;
      }

      const target = route.request().postDataJSON() as BoardTarget;
      const next: BoardDetail = { ...detail, ...target };
      mock.details[id] = next;
      mock.boards = mock.boards.map((b) => (b.id === id ? summarize(next) : b));
      await json(route, 200, next);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations$/.test(url.pathname),
    async (route) => {
      await json(route, 200, mock.annotations[boardIdOf(route)] ?? []);
    },
  );

  // 末尾一致にしない。パスを間違えても一致してしまい、契約から外れた呼び出しが
  // 緑のまま通る。取りこぼしはキャッチオールが 500 で拾う。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/interpret$/.test(url.pathname),
    async (route) => {
      await json(route, mock.interpret.status, mock.interpret.body);
    },
  );

  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/items$/.test(url.pathname),
    async (route) => {
      await json(route, mock.createItems.status, mock.createItems.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/session",
    async (route) => {
      await json(route, mock.session.status, mock.session.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/login",
    async (route) => {
      await json(route, mock.login.status, mock.login.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/logout",
    async (route) => {
      // ログアウトしたら未ログインに戻す。次の session の問い合わせに効く。
      // authRequired は元のまま保つ。ここで true に固定すると、認証を
      // 設定していない構成のテストが黙って別の構成に変わる。
      const authRequired =
        "authRequired" in mock.session.body ? mock.session.body.authRequired : true;
      mock.session = { status: 200, body: { authRequired, authenticated: false } };
      await route.fulfill({ status: 204, body: "" });
    },
  );

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

  return mock;
}

function boardIdOf(route: Route): string {
  const segments = new URL(route.request().url()).pathname.split("/");
  // /api/boards/<id>/... の 4 番目が ID。
  return segments[3] ?? "";
}

function summarize(b: BoardDetail): BoardSummary {
  return {
    id: b.id,
    name: b.name,
    createdAt: b.createdAt,
    updatedAt: b.updatedAt,
  };
}

/** Excalidraw が復元できる最小のシーン。 */
export function emptyScene(): string {
  return JSON.stringify({
    type: "excalidraw",
    version: 2,
    source: "etoki-e2e",
    elements: [],
    appState: {},
    files: {},
  });
}
