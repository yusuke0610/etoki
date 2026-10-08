import type { Page } from "@playwright/test";

import type {
  AnnotationStatus,
  BoardAccess,
  BoardDeletion,
  BoardDetail,
  BoardMember,
  BoardSummary,
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
  RepositoryList,
  SaveSceneRequest,
  SessionStatus,
  SyncRun,
} from "../../src/api/types";

import { installAnnotationRoutes } from "./routes/annotations";
import { installBoardRoutes } from "./routes/boards";
import { installDiagramRoutes } from "./routes/diagram";
import { installGitHubRoutes } from "./routes/github";
import { installMemberRoutes } from "./routes/members";
import { json } from "./routes/respond";
import { installSessionRoutes } from "./routes/session";

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
  /**
   * ボード ID をキーにした、サーバーが持っている貼った画像（ADR 0074）。
   * 画像の ID → 画像データの JSON。
   *
   * **`details` のシーンに入れない。** 実物は画像をシーンとは別に持ち、開く口
   * だけが返す。シーンに入れたモックは、画像を受け取れないフロントでも緑になる。
   */
  files: Record<string, Record<string, string>>;
  /**
   * 保存で受け取ったリクエストボディ。届いた順に積む。
   *
   * 何の画像を送ったかはここにしか現れない。2 回目以降の保存で画像を送り直して
   * いないかは、送ったボディを見ないと確かめられない（ADR 0074）。
   */
  saveRequests: SaveSceneRequest[];
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
  /** 作成先の候補と、取り切ったかどうか（ADR 0054）。 */
  repositories: Reply<RepositoryList>;
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
  /**
   * 一覧で件数を読めなかったことにするボードの ID（#200）。サーバーはシーンを
   * 読めなかったボードの件数を null で返す（#207）。
   */
  unreadableCounts?: string[];
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
  /** メンバー一覧の取得を失敗させたいときに指定する。 */
  membersListError?: Reply<never>;
  /** 招待を失敗させたいときに指定する。 */
  inviteError?: Reply<never>;
  /**
   * 招待する前の引き当てを失敗させたいときに指定する（ADR 0053）。
   *
   * 無ければ、どの login も `user-<login>` として引き当てる。一度ログインした
   * 相手を spec ごとに並べさせないため。
   */
  lookupInviteeError?: Reply<never>;
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
 * API をモックに差し替える。返り値を書き換えると次のリクエストから反映される。
 *
 * マッチングはパス名の述語で行う。パスの途中に api を含むグロブにすると、Vite が
 * 配信する `/src/api/types.ts` まで巻き込んで傍受してしまい、アプリが読み込め
 * なくなる。
 */
export async function installApi(page: Page, mock: ApiMock): Promise<ApiMock> {
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

  // 口のまとまりごとに重ねる。述語はどれも完全一致で互いに重ならないので、
  // まとまりの順番は結果を変えない。要るのはキャッチオールより後であることだけ。
  await installBoardRoutes(page, mock);
  await installAnnotationRoutes(page, mock);
  await installMemberRoutes(page, mock);
  await installDiagramRoutes(page, mock);
  await installSessionRoutes(page, mock);
  await installGitHubRoutes(page, mock);

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
  body: unknown,
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
      body: JSON.stringify(body),
    });
  });
}

/**
 * 注釈の一覧を壊す。落ちるのは注釈パネルの中だけ。
 *
 * `hold` を渡すと、それが解決するまで応答を返さない。ボードを開いて描いた
 * あとで落とす、という順番を作るために使う。
 *
 * **応答はトップレベル配列ではなくオブジェクト。** `GET .../annotations` は
 * `BoardAnnotations`（`annotations` / `detached`）を返すので、壊すのは
 * `annotations` の要素であって応答そのものの形ではない。
 */
export function breakAnnotations(page: Page, hold?: Promise<void>): Promise<void> {
  return breakList(
    page,
    (url) => /^\/api\/boards\/[^/]+\/annotations$/.test(url.pathname),
    { annotations: [null], detached: [] },
    hold,
  );
}

/** ボードの一覧を壊す。キャンバスへ入る前の画面ごと落ちる。 */
export function breakBoards(page: Page): Promise<void> {
  return breakList(page, (url) => url.pathname === "/api/boards", [null]);
}

/**
 * 応答を、渡した Promise が解決するまで返さない。
 *
 * 「保存中」「作成中」は普通は一瞬で終わるので、その隙に画面を確かめられない。
 * **遅らせるのは応答だけで、返す中身は `installApi` のものをそのまま使う**
 * （`route.fallback()`）。ここで本文まで書くと、版の照合を通らない保存の
 * モックが 1 つ増える（ADR 0012）。
 *
 * `installApi` の**後**に呼ぶ。Playwright のルートは後勝ち。
 */
async function holdRoute(
  page: Page,
  match: (url: URL) => boolean,
  method: string,
  hold: Promise<void>,
): Promise<void> {
  await page.route(match, async (route) => {
    // 止めるのは 1 つのメソッドだけ。同じパスの他のメソッドまで抱えると、
    // installApi が組み立てた振る舞いが静かに消える。
    if (route.request().method() !== method) {
      await route.fallback();
      return;
    }

    await hold;
    await route.fallback();
  });
}

/** 保存を「保存中」のまま止める。解決するまで応答を返さない。 */
export function holdSave(page: Page, hold: Promise<void>): Promise<void> {
  return holdRoute(
    page,
    (url) => /^\/api\/boards\/[^/]+\/scene$/.test(url.pathname),
    "PUT",
    hold,
  );
}

/**
 * 指定したボードの取得だけを止める。解決するまで応答を返さない。
 *
 * **1 枚だけ止める。** 全部止めると、追い越す側の取得まで待つことになり、
 * 確かめたい「古い応答があとから着く」並びを作れない。
 */
export function holdBoardDetail(
  page: Page,
  boardId: string,
  hold: Promise<void>,
): Promise<void> {
  return holdRoute(page, (url) => url.pathname === `/api/boards/${boardId}`, "GET", hold);
}

/** 作成を「作成中」のまま止める。解決するまで応答を返さない。 */
export function holdCreate(page: Page, hold: Promise<void>): Promise<void> {
  return holdRoute(
    page,
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/items$/.test(url.pathname),
    "POST",
    hold,
  );
}

/** 解釈を「解釈中」のまま止める。解決するまで応答を返さない。 */
export function holdInterpret(page: Page, hold: Promise<void>): Promise<void> {
  return holdRoute(
    page,
    (url) => /^\/api\/boards\/[^/]+\/annotations\/[^/]+\/interpret$/.test(url.pathname),
    "POST",
    hold,
  );
}
