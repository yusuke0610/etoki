import type {
  AnnotationStatus,
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
} from "./types";

/** API が返したエラー。呼び出し側でステータスに応じて分岐するために持つ。 */
export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
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
    try {
      message = (JSON.parse(body) as Partial<ErrorResponse>).error ?? body;
    } catch {
      // JSON でなければ本文をそのまま使う
    }
    throw new ApiError(res.status, message || res.statusText);
  }

  if (res.status === 204) {
    return undefined as T;
  }
  return (await res.json()) as T;
}

export const boardsApi = {
  list: () => request<BoardSummary[]>("/api/boards"),

  create: (name: string) =>
    request<BoardDetail>("/api/boards", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),

  get: (id: string) => request<BoardDetail>(`/api/boards/${id}`),

  saveScene: (id: string, scene: string) =>
    request<void>(`/api/boards/${id}/scene`, {
      method: "PUT",
      body: JSON.stringify({ scene }),
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
   * 読むのは保存済みシーン。GitHub には何も作らない。
   */
  interpret: (boardId: string, annotationId: string) =>
    request<Interpretation>(
      `/api/boards/${boardId}/annotations/${annotationId}/interpret`,
      { method: "POST" },
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
