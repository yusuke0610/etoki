import type {
  AnnotationStatus,
  BoardDetail,
  BoardListEntry,
  BoardSummary,
} from "../../src/api/types";

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

/**
 * 一覧の 1 件。注釈の 3 状態の件数を `mock.annotations` から数えて足す（#200）。
 *
 * **控えずに、返すたびに数える。** サーバーも一覧を返すたびに保存済みシーンから
 * 数える。控えにすると、テストの途中で注釈の状態を書き換えたときに、一覧だけが
 * 古い件数を返す。
 */
export function listEntry(
  b: BoardSummary,
  annotations: AnnotationStatus[],
): BoardListEntry {
  const count = (state: AnnotationStatus["state"]) =>
    annotations.filter((a) => a.state === state).length;
  return {
    ...b,
    annotationCounts: {
      uncreated: count("uncreated"),
      created: count("created"),
      changed: count("changed"),
    },
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

/**
 * シーンから注釈の状態を組み立てる。
 *
 * **サーバーと同じ順で同じ規則を使う。** 判定は「`type === "frame"` かつ
 * `customData.etoki` をメタデータとして読める形で持つ」で、これはルートの
 * `CLAUDE.md` が正本（Go 側は `internal/domain/scene.go`）。緩めると、注釈に
 * なっていない frame まで注釈として並ぶモックになり、判定の誤りを隠す。
 *
 * 3 状態は必ず `uncreated`。作ったばかりのボードには run が無い。
 */
export function annotationsOfScene(scene: string): AnnotationStatus[] {
  const parsed = JSON.parse(scene) as {
    elements?: {
      id: string;
      type: string;
      name?: string | null;
      isDeleted?: boolean;
      customData?: { etoki?: unknown };
    }[];
  };

  return (parsed.elements ?? [])
    .filter((el) => {
      if (el.type !== "frame" || el.isDeleted) return false;
      const meta = el.customData?.etoki;
      return typeof meta === "object" && meta !== null && !Array.isArray(meta);
    })
    .map((el) => {
      const meta = el.customData?.etoki as { granularity?: string; kind?: string };
      return {
        id: el.id,
        name: el.name ?? "",
        granularity: (meta.granularity ?? "") as AnnotationStatus["granularity"],
        // 種別はひな形から始めたときだけ載る。無ければキーごと省く。
        ...(meta.kind ? { kind: meta.kind as AnnotationStatus["kind"] } : {}),
        state: "uncreated" as const,
      };
    });
}
