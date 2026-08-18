import type {
  AnnotationImage,
  AnnotationStatus,
  InterpretRequest,
  LoginResponse,
  SessionStatus,
  BoardAccess,
  BoardDetail,
  BoardMember,
  BoardRole,
  BoardSummary,
  BoardTarget,
  CreatedRun,
  ErrorResponse,
  Interpretation,
  Project,
  Repository,
  SaveSceneRequest,
  SaveSceneResponse,
} from "./types";

/**
 * API が返したエラー。呼び出し側で分岐するために持つ。
 *
 * **分岐は `code` で行う。** ステータスだけでは打ち手が決まらない（409 には
 * 6 つの原因が同居し、打ち手は全部違う）。文言の照合で分けると、Go のエラー
 * 文言が事実上の API 契約になる。表示用の日本語は `errorMessage.ts` が引く。
 */
export class ApiError extends Error {
  readonly status: number;
  /**
   * 契約の `ErrorCode`。
   *
   * **`ErrorCode` ではなく `string` で持つ。** サーバーが画面より新しいと、
   * まだ知らない code が来る。型で締めると、知らない値を「知っている」ものとして
   * 扱うことになり、対応表から漏れたことに気づけない。
   */
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: { "Content-Type": "application/json", ...init?.headers },
  });

  if (!res.ok) {
    // エラーボディが JSON とは限らないので、読めなければ本文をそのまま使う。
    const body = await res.text();
    let message = body;
    // 契約では必須だが、プロキシが返した応答など契約の外から来ることもある。
    // その場合は internal に寄せる。画面は本文を畳んだ側に出す。
    let code = "internal";
    try {
      const parsed = JSON.parse(body) as Partial<ErrorResponse>;
      message = parsed.error ?? body;
      code = parsed.code ?? code;
    } catch {
      // JSON でなければ本文をそのまま使う
    }
    throw new ApiError(res.status, code, message || res.statusText);
  }

  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export const boardsApi = {
  list: () => request<BoardSummary[]>("/api/boards"),

  /**
   * ボードを作る。
   *
   * **作成先は必須。** 候補は書ける Project だけに絞ってあるので、書ける先を
   * 1 つも持たない人はここまで来られない（ADR 0017）。
   */
  create: (name: string, target: BoardTarget) =>
    request<BoardDetail>("/api/boards", {
      method: "POST",
      body: JSON.stringify({ name, ...target }),
    }),

  get: (id: string) => request<BoardDetail>(`/api/boards/${id}`),

  /**
   * シーンを保存する。
   *
   * `baseUpdatedAt` は編集の基準にした版。ボードは共有できるので、照合せずに
   * 書くと後勝ちで相手の作業がまるごと消える（ADR 0020）。食い違えば 409 が
   * 返り、サーバー側のシーンは書き換わらない。
   *
   * 返る `updatedAt` が次の保存の基準になる。捨てると、2 回目の保存が必ず
   * 409 になる。
   */
  saveScene: (id: string, scene: string, baseUpdatedAt: string) =>
    request<SaveSceneResponse>(`/api/boards/${id}/scene`, {
      method: "PUT",
      body: JSON.stringify({ scene, baseUpdatedAt } satisfies SaveSceneRequest),
    }),

  /**
   * draft issue の作成先をボードに設定する。
   *
   * 最初の draft issue を作った後は 409 が返る（ADR 0014）。
   */
  setTarget: (id: string, target: BoardTarget) =>
    request<BoardDetail>(`/api/boards/${id}/target`, {
      method: "PUT",
      body: JSON.stringify(target),
    }),

  annotations: (id: string) =>
    request<AnnotationStatus[]>(`/api/boards/${id}/annotations`),

  /**
   * そのボードで何ができるかを返す。
   *
   * ボード取得とは別に叩く。GitHub が未設定・不通でもボードは開ける必要が
   * あるため。`projectAccess` は状態であって判定ではない。これを見て作成を
   * 止めるのではなく、できない理由を先に見せるのに使う（ADR 0017）。
   */
  access: (id: string) => request<BoardAccess>(`/api/boards/${id}/access`),

  /**
   * 注釈を LLM に解釈させる。
   *
   * テキストを読むのは保存済みシーン。GitHub には何も作らない。
   *
   * `image` は注釈範囲を写した画像で、矢印やグルーピングのようにテキストに
   * 現れない構造を渡す（ADR 0018）。画像は画面から書き出すので、保存済みシーン
   * と揃っているあいだしか呼んではならない。
   */
  interpret: (boardId: string, annotationId: string, image?: AnnotationImage) =>
    request<Interpretation>(
      `/api/boards/${boardId}/annotations/${annotationId}/interpret`,
      { method: "POST", body: JSON.stringify({ image } satisfies InterpretRequest) },
    ),

  /**
   * 解釈結果から draft issue を作る。
   *
   * 解釈とは別の呼び出しに保つ。結果を見た開発者が明示的に叩く。
   */
  createItems: (boardId: string, annotationId: string, interpretation: Interpretation) =>
    request<CreatedRun>(`/api/boards/${boardId}/annotations/${annotationId}/items`, {
      method: "POST",
      body: JSON.stringify(interpretation),
    }),
};

/**
 * ボードの共有。
 *
 * 招待される側にリポジトリのアクセス権は要らない。ブレストに呼ぶ相手と
 * GitHub に書ける相手は同じではない（ADR 0017）。認証を設定していない構成では
 * すべて 503 が返る。
 */
export const membersApi = {
  list: (boardId: string) => request<BoardMember[]>(`/api/boards/${boardId}/members`),

  /** login で指す。相手は一度 etoki にログインしている必要がある。 */
  invite: (boardId: string, login: string, role: BoardRole) =>
    request<BoardMember>(`/api/boards/${boardId}/members`, {
      method: "POST",
      body: JSON.stringify({ login, role }),
    }),

  setRole: (boardId: string, userId: string, role: BoardRole) =>
    request<BoardMember>(`/api/boards/${boardId}/members/${encodeURIComponent(userId)}`, {
      method: "PUT",
      body: JSON.stringify({ role }),
    }),

  remove: (boardId: string, userId: string) =>
    request<void>(`/api/boards/${boardId}/members/${encodeURIComponent(userId)}`, {
      method: "DELETE",
    }),
};

/**
 * ログインとセッション。
 *
 * 認証を設定していない構成でも session は 200 を返す。authRequired が false
 * なら画面はログインを求めない（ADR 0015）。
 */
export const authApi = {
  session: () => request<SessionStatus>("/api/auth/session"),

  /**
   * 認可画面の URL を受け取る。遷移は呼び出し側が行う。
   *
   * POST なのは state の発行が書き込みだから。GET にすると外部ページから
   * 叩けてしまう。
   */
  start: () => request<LoginResponse>("/api/auth/login", { method: "POST" }),

  logout: () => request<void>("/api/auth/logout", { method: "POST" }),
};

/** 作成先を選ぶための一覧。ボードには紐づかない。 */
export const githubApi = {
  repositories: () => request<Repository[]>("/api/github/repositories"),

  projects: (owner: string, repo: string) =>
    request<Project[]>(
      `/api/github/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/projects`,
    ),
};
