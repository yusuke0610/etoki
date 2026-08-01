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
};
