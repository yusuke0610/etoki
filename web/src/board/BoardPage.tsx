import { Excalidraw, serializeAsJSON } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useState } from "react";

import {
  boardsApi,
  type AnnotationStatus,
  type BoardDetail,
  type Granularity,
  type Interpretation,
} from "../api/boards";
import {
  isAnnotation,
  markAsAnnotation,
  selectableFrameIds,
  unmarkAnnotation,
  type SceneElement,
} from "../excalidraw/annotation";
import {
  AnnotationPanel,
  type CreationState,
  type InterpretationState,
} from "./AnnotationPanel";
import { createGenerations } from "./generation";

type Props = {
  board: BoardDetail;
  onError: (message: string) => void;
};

export function BoardPage({ board, onError }: Props) {
  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
  const [annotations, setAnnotations] = useState<AnnotationStatus[]>([]);
  const [selectedFrames, setSelectedFrames] = useState<string[]>([]);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [interpretations, setInterpretations] = useState<
    Record<string, InterpretationState>
  >({});
  const [creations, setCreations] = useState<Record<string, CreationState>>({});
  // 実行中の解釈を無効にするための世代。useState の初期化関数で 1 度だけ作る。
  const [generations] = useState(createGenerations);
  // 作成は解釈とは別の世代で管理する。共有すると、片方の実行がもう片方の
  // 応答まで無効にしてしまう。
  const [creationGenerations] = useState(createGenerations);

  const initialData = useMemo(() => {
    try {
      return JSON.parse(board.scene) as { elements?: unknown; appState?: unknown };
    } catch {
      // 保存時に検証しているのでここには来ないはずだが、来たら空で開く。
      onError("シーンを読み込めませんでした。空のボードとして開きます。");
      return { elements: [], appState: {} };
    }
  }, [board.scene, onError]);

  const refreshAnnotations = useCallback(async () => {
    try {
      setAnnotations(await boardsApi.annotations(board.id));
    } catch (e) {
      onError(`注釈の状態を取得できませんでした: ${String(e)}`);
    }
  }, [board.id, onError]);

  useEffect(() => {
    void refreshAnnotations();
  }, [refreshAnnotations]);

  /** 選択状態の変化を拾い、注釈にできる frame を割り出す。 */
  const handleChange = useCallback(
    (
      elements: readonly unknown[],
      appState: { selectedElementIds: Record<string, boolean> },
    ) => {
      setDirty(true);
      setSelectedFrames(
        selectableFrameIds(elements as SceneElement[], appState.selectedElementIds),
      );
    },
    [],
  );

  /** 現在のシーンを elements ごと差し替える。 */
  const updateElements = useCallback(
    (next: SceneElement[]) => {
      api?.updateScene({ elements: next as never });
      setDirty(true);
    },
    [api],
  );

  const currentElements = useCallback(
    () => (api?.getSceneElements() ?? []) as unknown as SceneElement[],
    [api],
  );

  const handleMark = useCallback(
    (frameId: string, granularity: Granularity) => {
      updateElements(markAsAnnotation(currentElements(), frameId, granularity));
    },
    [currentElements, updateElements],
  );

  const handleUnmark = useCallback(
    (frameId: string) => {
      updateElements(unmarkAnnotation(currentElements(), frameId));
    },
    [currentElements, updateElements],
  );

  const save = useCallback(async () => {
    if (!api) return;

    setSaving(true);
    try {
      const scene = serializeAsJSON(
        api.getSceneElements(),
        api.getAppState(),
        api.getFiles(),
        "local",
      );
      await boardsApi.saveScene(board.id, scene);
      setDirty(false);
      // 解釈は保存済みシーンに対する結果。保存したら対象が変わったので捨てる。
      // 実行中のものも無効にする。後から返ってきて結果が復活すると、いまの
      // 内容を解釈したものだと誤読される。
      generations.invalidateAll();
      creationGenerations.invalidateAll();
      setInterpretations({});
      setCreations({});
      await refreshAnnotations();
    } catch (e) {
      onError(`保存できませんでした: ${String(e)}`);
    } finally {
      setSaving(false);
    }
  }, [api, board.id, creationGenerations, generations, onError, refreshAnnotations]);

  /**
   * 注釈を解釈させる。
   *
   * エラーはパネル内に残す。どの注釈で何が起きたか分からなくなるので、
   * 画面全体のエラー表示には流さない。
   */
  const interpret = useCallback(
    async (annotationId: string) => {
      // 応答を受け取ったとき、これがまだ最新の要求かを判断できるようにする。
      const generation = generations.start(annotationId);

      setInterpretations((prev) => ({
        ...prev,
        [annotationId]: { status: "running" },
      }));

      try {
        const result = await boardsApi.interpret(board.id, annotationId);
        if (!generations.isCurrent(annotationId, generation)) return;
        setInterpretations((prev) => ({
          ...prev,
          [annotationId]: { status: "done", result },
        }));
      } catch (e) {
        if (!generations.isCurrent(annotationId, generation)) return;
        setInterpretations((prev) => ({
          ...prev,
          [annotationId]: {
            status: "error",
            message: e instanceof Error ? e.message : String(e),
          },
        }));
      }
    },
    [board.id, generations],
  );

  /**
   * 解釈結果から draft issue を作る。
   *
   * 作成後は状態が created に変わるので、注釈の状態を取り直す。
   */
  const create = useCallback(
    async (annotationId: string, interpretation: Interpretation) => {
      const generation = creationGenerations.start(annotationId);
      setCreations((prev) => ({ ...prev, [annotationId]: { status: "running" } }));

      try {
        const run = await boardsApi.createItems(board.id, annotationId, interpretation);
        // 保存が挟まっていたら、この結果は保存前の解釈に対するもの。表示すると
        // いまの内容に対して作られたと誤読される。
        if (!creationGenerations.isCurrent(annotationId, generation)) return;
        setCreations((prev) => ({
          ...prev,
          [annotationId]: { status: "done", run },
        }));
        await refreshAnnotations();
      } catch (e) {
        if (!creationGenerations.isCurrent(annotationId, generation)) return;
        setCreations((prev) => ({
          ...prev,
          [annotationId]: {
            status: "error",
            message: e instanceof Error ? e.message : String(e),
          },
        }));
      }
    },
    [board.id, creationGenerations, refreshAnnotations],
  );

  // 作成中は保存させない。GitHub への作成は取り消せないので、実行中にシーンが
  // 変わると、作られた内容と記録されるハッシュが食い違いうる。
  const creating = Object.values(creations).some((c) => c.status === "running");

  const markable = selectedFrames.filter(
    (id) => !currentElements().some((el) => el.id === id && isAnnotation(el)),
  );
  const unmarkable = selectedFrames.filter((id) =>
    currentElements().some((el) => el.id === id && isAnnotation(el)),
  );

  return (
    <div className="board">
      <header className="board-header">
        <h1>{board.name}</h1>
        <div className="board-actions">
          {dirty && <span className="dirty">未保存</span>}
          <button
            type="button"
            onClick={() => void save()}
            disabled={saving || creating || !api}
            title={creating ? "作成が終わるまで保存できません" : undefined}
          >
            {saving ? "保存中…" : "保存"}
          </button>
        </div>
      </header>

      <div className="board-body">
        <div className="canvas">
          <Excalidraw
            excalidrawAPI={setApi}
            initialData={initialData as never}
            onChange={handleChange as never}
            langCode="ja-JP"
          />
        </div>

        <AnnotationPanel
          annotations={annotations}
          markableFrameIds={markable}
          unmarkableFrameIds={unmarkable}
          onMark={handleMark}
          onUnmark={handleUnmark}
          onChangeGranularity={(id, g) => handleMark(id, g)}
          stale={dirty}
          interpretations={interpretations}
          onInterpret={(id) => void interpret(id)}
          creations={creations}
          onCreate={(id, interpretation) => void create(id, interpretation)}
        />
      </div>
    </div>
  );
}
