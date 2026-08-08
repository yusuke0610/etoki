import type {
  AnnotationStatus,
  BoardDetail,
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

/** 作成先を選ぶための一覧。ボードには紐づかない。 */
export const githubApi = {
  repositories: () => request<Repository[]>("/api/github/repositories"),

  projects: (owner: string, repo: string) =>
    request<Project[]>(
      `/api/github/repositories/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}/projects`,
    ),
};
