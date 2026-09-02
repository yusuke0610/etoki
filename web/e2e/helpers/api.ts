import type { Page, Route } from "@playwright/test";

import type {
  AnnotationStatus,
  BoardAccess,
  BoardAnnotations,
  BoardDeletion,
  BoardDetail,
  BoardMember,
  BoardRole,
  BoardSummary,
  BoardTarget,
  BoardTargetDisplay,
  Capabilities,
  CreatedRun,
  DetachedAnnotation,
  DiagramDraft,
  ErrorResponse,
  GenerateDiagramRequest,
  Interpretation,
  InterpretRequest,
  LoginResponse,
  Project,
  Repository,
  SaveSceneRequest,
  SaveSceneResponse,
  SessionStatus,
  SyncRun,
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
  /**
   * ボード ID をキーにした「シーンから消えた注釈」（#111）。
   *
   * **`annotations` と分けて持つ。** 契約でも別のリストなので（3 状態も名前も
   * 無い）、混ぜて持つとモックだけが混ざった形を返せてしまう。
   */
  detached: Record<string, DetachedAnnotation[]>;
  interpret: Reply<Interpretation>;
  /**
   * 解釈で受け取ったリクエストボディ。届いた順に積む。
   *
   * 画像を添えているかを確かめるために持つ。画像はフロントが画面から書き出す
   * ので、送れたかどうかは実ブラウザでしか分からない（ADR 0018）。
   */
  interpretRequests: InterpretRequest[];
  createItems: Reply<CreatedRun>;
  /**
   * 作成で受け取ったリクエストボディ。届いた順に積む。
   *
   * 何を作らせたのかはここにしか現れない。画面で外した項目や手直しした本文が
   * GitHub に届く形になっているかは、送ったボディを見ないと確かめられない
   * （ADR 0024）。
   */
  createRequests: Interpretation[];
  /**
   * 図のドラフト生成の応答（ADR 0041）。
   *
   * **注釈で引かない。** この口はボードの直下にあり、囲みとは無関係。
   */
  diagramDraft: Reply<DiagramDraft>;
  /**
   * 生成で受け取ったリクエストボディ。届いた順に積む。
   *
   * **サーバーは会話を持たない。** 続きを頼むときに会話をまるごと送れて
   * いるかは、送ったボディを見ないと確かめられない。
   */
  diagramRequests: GenerateDiagramRequest[];
  /** 作成先の候補。リポジトリ選択の画面が読む。 */
  repositories: Reply<Repository[]>;
  /** `owner/name` をキーにした Projects v2。 */
  projects: Record<string, Reply<Project[]>>;
  /**
   * いま使える機能。既定は全部そろった構成。
   *
   * 落とすと、押す前に理由が出る側の見せ方になる（ADR 0030）。**エンドポイント
   * 側も 503 に揃えること。** 片方だけ落とすと、画面が案内しないのに 503 が
   * 返る（またはその逆）という、実物では起きない組み合わせを緑にしてしまう。
   */
  capabilities: Reply<Capabilities>;
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
  /** 表示名の取り直しを失敗させたいときに指定する（ADR 0037）。 */
  refreshTargetDisplayError?: Reply<never>;
  /**
   * 保存を失敗させたいときに指定する。413 の見せ方を確かめる用（ADR 0038）。
   *
   * 版の照合より先に返す。大きさで断られる場面は基準が合っていても起きる。
   */
  saveSceneError?: Reply<never>;
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
  /** 改名を失敗させたいときに指定する。 */
  renameError?: Reply<never>;
  /**
   * 削除で失われるもの。ボード ID をキーにする（ADR 0042）。
   *
   * 指定が無ければ 0 件を返す。**件数は画面が出す文言そのものなので、
   * 「作成済みのボードを消す」場面のテストはここを埋める。**
   */
  deletion?: Record<string, Reply<BoardDeletion>>;
  /** 削除を失敗させたいときに指定する。 */
  deleteError?: Reply<never>;
  /**
   * 注釈 ID をキーにした実行履歴（ADR 0007）。
   *
   * **ボードではなく注釈で引く。** 履歴を出す画面は注釈のカードの中にあり、
   * spec が見たいのも「その注釈で何回作ったか」なので、キーを揃えておく。
   * 指定が無ければ空配列を返す。
   */
  runs?: Record<string, Reply<SyncRun[]>>;
};

/**
 * owner 以外に閉じている操作の応答。
 *
 * **404 ではなく 403。** メンバーはボードの存在をすでに知っているので、何が
 * 足りないのかを隠す理由が無い（ADR 0017）。非メンバーの 404 と混ぜない。
 */
async function ownerOnly(route: Route): Promise<void> {
  await json(route, 403, {
    code: "forbidden_role",
    error: "owner only",
  } satisfies ErrorResponse);
}

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
      // 表示用の値は任意（ADR 0019 / 0025）。送ってこなければ「知らない」で
      // 残り、リンクはリポジトリの Projects へ落ちる。
      projectNumber: target.projectNumber ?? 0,
      projectTitle: target.projectTitle ?? "",
      projectUrl: target.projectUrl ?? "",
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
        code: "internal",
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
        mock.detached[board.id] ??= [];
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
        await json(route, 404, {
          code: "not_found",
          error: "not found",
        } satisfies ErrorResponse);
        return;
      }
      await json(route, 200, updated);
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

  await page.route(
    (url) => url.pathname === "/api/capabilities",
    async (route) => {
      if (route.request().method() !== "GET") {
        await route.fallback();
        return;
      }
      await json(route, mock.capabilities.status, mock.capabilities.body);
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

/**
 * 応答を壊して、描画中に落ちる状況を作る。
 *
 * **ここだけ本文を生成型で書かない**（`web/CLAUDE.md`）。壊れていること自体が
 * 入力なので、型に合わせると再現しない。フロントは応答を検証せずに型として
 * 扱うので、契約から外れた本文はそのまま render まで届く。
 *
 * `installApi` の**後**に呼ぶ。Playwright のルートは後勝ち。
 */
async function breakList(
  page: Page,
  match: (url: URL) => boolean,
  hold?: Promise<void>,
): Promise<void> {
  await page.route(match, async (route) => {
    // 壊すのは一覧の取得だけ。同じパスの POST（ボードの作成）まで奪うと、
    // installApi が組み立てた振る舞いが静かに消える。
    if (route.request().method() !== "GET") {
      await route.fallback();
      return;
    }

    // 落ちる時刻をテストに決めさせる。マウント直後にしか落とせないと、
    // 「落ちる前の描き込みが残るか」を確かめられない。
    await hold;

    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([null]),
    });
  });
}

/**
 * 注釈の一覧を壊す。落ちるのは注釈パネルの中だけ。
 *
 * `hold` を渡すと、それが解決するまで応答を返さない。ボードを開いて描いた
 * あとで落とす、という順番を作るために使う。
 */
export function breakAnnotations(page: Page, hold?: Promise<void>): Promise<void> {
  return breakList(
    page,
    (url) => /^\/api\/boards\/[^/]+\/annotations$/.test(url.pathname),
    hold,
  );
}

/** ボードの一覧を壊す。キャンバスへ入る前の画面ごと落ちる。 */
export function breakBoards(page: Page): Promise<void> {
  return breakList(page, (url) => url.pathname === "/api/boards");
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
    projectUrl: b.projectUrl,
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
