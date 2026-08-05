/** バックエンドの 3 状態。 */
export type SyncState = "uncreated" | "created" | "changed";

/** 注釈の粒度。空文字は「指定なし（LLM に任せる）」。 */
export type Granularity = "" | "epic" | "issue";

export type BoardSummary = {
  id: string;
  name: string;
  createdAt: string;
  updatedAt: string;
};

export type BoardDetail = BoardSummary & {
  scene: string;
};

export type SyncItem = {
  itemId: string;
  kind: "epic" | "issue";
  title: string;
  localId: string;
  parentLocalId?: string;
};

export type AnnotationStatus = {
  id: string;
  name: string;
  granularity: Granularity;
  state: SyncState;
  lastSyncedAt?: string;
  items?: SyncItem[];
};

/** 解釈結果に含まれる draft issue 1 件。まだ作成はしていない。 */
export type InterpretedItem = {
  localId: string;
  kind: "epic" | "issue";
  title: string;
  body: string;
  parentLocalId?: string;
};

/**
 * LLM が注釈をどう解釈したか。
 *
 * summary は GitHub には作らない。作成前に「こう読んだ」を見せるためだけに使う。
 */
export type Interpretation = {
  summary: string;
  items: InterpretedItem[];
};

/** 作成した run。途中で失敗しても、作れたぶんは items に入る。 */
export type CreatedRun = {
  runId: number;
  createdAt: string;
  items: SyncItem[];
  /** 途中で失敗したことを表す。 */
  incomplete?: boolean;
  error?: string;
};

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
      message = (JSON.parse(body) as { error?: string }).error ?? body;
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
