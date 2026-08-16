import type { Page, Route } from "@playwright/test";

import type {
  AnnotationStatus,
  BoardAccess,
  BoardDetail,
  BoardMember,
  BoardRole,
  BoardSummary,
  BoardTarget,
  CreatedRun,
  ErrorResponse,
  Interpretation,
  InterpretRequest,
  LoginResponse,
  Project,
  Repository,
  SaveSceneRequest,
  SaveSceneResponse,
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
  /**
   * 解釈で受け取ったリクエストボディ。届いた順に積む。
   *
   * 画像を添えているかを確かめるために持つ。画像はフロントが画面から書き出す
   * ので、送れたかどうかは実ブラウザでしか分からない（ADR 0018）。
   */
  interpretRequests: InterpretRequest[];
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
  /**
   * そのボードで何ができるか。ボード ID をキーにする。
   *
   * 無いボードは role をボードの値から、projectAccess を unknown として返す。
   * 全 spec に権限を書かせないため。
   */
  access?: Record<string, Reply<BoardAccess>>;
  /** ボード ID をキーにしたメンバー一覧。 */
  members?: Record<string, BoardMember[]>;
  /** 招待を失敗させたいときに指定する。 */
  inviteError?: Reply<never>;
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

  // 新しいボードは作成先を持って生まれる。作成先を選ばないと作れない
  // （ADR 0017）。
  const newBoard = (name: string, target: BoardTarget): BoardDetail => {
    issued += 1;
    return {
      id: `board-new-${issued}`,
      name,
      // 作った本人は必ず owner（ADR 0017）。
      role: "owner",
      createdAt: "2026-08-05T10:00:00Z",
      updatedAt: "2026-08-05T10:00:00Z",
      scene: emptyScene(),
      repositoryOwner: target.repositoryOwner,
      repositoryName: target.repositoryName,
      projectId: target.projectId,
      // 表示名は任意（ADR 0019）。送ってこなければ「名前を知らない」で残る。
      projectNumber: target.projectNumber ?? 0,
      projectTitle: target.projectTitle ?? "",
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
        const req = route.request().postDataJSON() as { name: string } & BoardTarget;
        const board = newBoard(req.name, req);
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

      const id = boardIdOf(route);
      const detail = mock.details[id];
      if (!detail) {
        await json(route, 404, { error: "not found" } satisfies ErrorResponse);
        return;
      }

      const req = route.request().postDataJSON() as SaveSceneRequest;
      if (req.baseUpdatedAt !== detail.updatedAt) {
        await json(route, 409, {
          error: "他の人がこのボードを保存しています",
        } satisfies ErrorResponse);
        return;
      }

      // 版を進める。据え置くと、基準を更新し損ねたフロントでも保存し続けられて
      // しまい、照合が効いているように見えるだけになる。
      const next: BoardDetail = {
        ...detail,
        scene: req.scene,
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
        await json(route, 404, { error: "not found" } satisfies ErrorResponse);
        return;
      }

      const target = route.request().postDataJSON() as BoardTarget;
      const next: BoardDetail = {
        ...detail,
        ...target,
        // 表示名は任意なので、送られてこなければ「名前を知らない」に落とす。
        // undefined のまま混ぜると、契約では必須の項目が消える。
        projectNumber: target.projectNumber ?? 0,
        projectTitle: target.projectTitle ?? "",
      };
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

  // 権限はボードの取得とは別に訊かれる。GitHub が未設定・不通でもボードは
  // 開ける必要があるため（ADR 0017）。
  await page.route(
    (url) => /^\/api\/boards\/[^/]+\/access$/.test(url.pathname),
    async (route) => {
      const id = boardIdOf(route);
      const configured = mock.access?.[id];
      if (configured) {
        await json(route, configured.status, configured.body);
        return;
      }

      await json(route, 200, {
        role: mock.details[id]?.role ?? "owner",
        projectAccess: "unknown",
      } satisfies BoardAccess);
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

        const req = route.request().postDataJSON() as { login: string; role: BoardRole };
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

      await json(route, 200, mock.members[id]);
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
        await json(route, 404, { error: "not found" } satisfies ErrorResponse);
        return;
      }
      await json(route, 200, updated);
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
      await json(route, mock.createItems.status, mock.createItems.body);
    },
  );

  // 認証の 3 本も契約のメソッドだけを受ける。何でも受けると、フロントが違う
  // メソッドで叩いていても E2E は緑のまま通り、実物で初めて落ちる。
  await page.route(
    (url) => url.pathname === "/api/auth/session",
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      await json(route, mock.session.status, mock.session.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/login",
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }
      await json(route, mock.login.status, mock.login.body);
    },
  );

  await page.route(
    (url) => url.pathname === "/api/auth/logout",
    async (route) => {
      if (route.request().method() !== "POST") {
        await route.fallback();
        return;
      }

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

/**
 * 詳細から一覧の 1 件を作る。
 *
 * 一覧は作成先も返す（ADR 0019）。詰め替えを spec ごとに手で書くと、契約に
 * 項目が増えたときに直す場所が散る。
 */
export function summarize(b: BoardDetail): BoardSummary {
  return {
    id: b.id,
    name: b.name,
    role: b.role,
    createdAt: b.createdAt,
    updatedAt: b.updatedAt,
    repositoryOwner: b.repositoryOwner,
    repositoryName: b.repositoryName,
    projectId: b.projectId,
    projectNumber: b.projectNumber,
    projectTitle: b.projectTitle,
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
