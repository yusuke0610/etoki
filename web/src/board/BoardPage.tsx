import { Excalidraw, serializeAsJSON } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { boardsApi } from "../api/boards";
import type {
  AnnotationStatus,
  BoardDetail,
  Granularity,
  Interpretation,
} from "../api/types";
import {
  isAnnotation,
  markAsAnnotation,
  selectableFrameIds,
  unmarkAnnotation,
  type SceneElement,
} from "../excalidraw/annotation";
import { sceneSignature } from "../excalidraw/dirty";
import {
  AnnotationPanel,
  type CreationState,
  type InterpretationState,
} from "./AnnotationPanel";
import { createGenerations } from "./generation";

type Props = {
  board: BoardDetail;
  onError: (message: string) => void;
  /** 作成先を選び直す。固定済みなら呼ばれない。 */
  onChangeTarget: () => void;
};

export function BoardPage({ board, onError, onChangeTarget }: Props) {
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

  // 状態の取得は初期表示・保存・作成から重なって走る。番号を振って最後に
  // 投げたものだけ反映する。古い応答で上書きすると、作成済みの注釈が未作成に
  // 巻き戻って見える。
  const annotationsRequest = useRef(0);

  const refreshAnnotations = useCallback(async () => {
    const request = ++annotationsRequest.current;
    try {
      const next = await boardsApi.annotations(board.id);
      if (request !== annotationsRequest.current) return;
      setAnnotations(next);
    } catch (e) {
      if (request !== annotationsRequest.current) return;
      onError(`注釈の状態を取得できませんでした: ${String(e)}`);
    }
  }, [board.id, onError]);

  useEffect(() => {
    void refreshAnnotations();
  }, [refreshAnnotations]);

  // 保存済みシーンの署名。未保存かどうかはこれと現在の署名の比較で決める。
  // null は Excalidraw から最初の onChange がまだ来ていない状態。
  const savedSignature = useRef<string | null>(null);
  const latestSignature = useRef<string | null>(null);

  /** 署名を取り込み、保存済みと違えば未保存にする。 */
  const applySignature = useCallback((signature: string) => {
    latestSignature.current = signature;
    // Excalidraw はマウント時にも onChange を発火する。その 1 回目は保存済み
    // シーンそのものなので、未保存ではなく基準として覚える。
    savedSignature.current ??= signature;
    setDirty(signature !== savedSignature.current);
  }, []);

  /** 選択状態の変化を拾い、注釈にできる frame を割り出す。 */
  const handleChange = useCallback(
    (
      elements: readonly unknown[],
      appState: { selectedElementIds: Record<string, boolean> },
    ) => {
      const els = elements as SceneElement[];
      applySignature(sceneSignature(els));
      setSelectedFrames(selectableFrameIds(els, appState.selectedElementIds));
    },
    [applySignature],
  );

  /** 現在のシーンを elements ごと差し替える。 */
  const updateElements = useCallback(
    (next: SceneElement[]) => {
      api?.updateScene({ elements: next as never });
      // onChange の発火を待たずにここでも判定する。注釈の付け外しが未保存として
      // 出るかどうかを、updateScene が onChange を呼ぶかに依存させない。
      applySignature(sceneSignature(next));
    },
    [api, applySignature],
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
      const elements = api.getSceneElements();
      const scene = serializeAsJSON(elements, api.getAppState(), api.getFiles(), "local");
      // 送った内容そのものを新しい基準にする。保存の待ち時間に編集されていたら
      // 未保存のまま残す必要があるので、setDirty(false) とは書かない。
      const sent = sceneSignature(elements as unknown as SceneElement[]);

      await boardsApi.saveScene(board.id, scene);
      savedSignature.current = sent;
      setDirty(latestSignature.current !== sent);
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

  // 保存と作成は互いに排他にする。作成中に保存させないのは、GitHub への作成が
  // 取り消せないため。実行中にシーンが変わると、作られた内容と記録されるハッシュ
  // が食い違いうる。逆に保存は creations を捨てるので、保存中に作らせると
  // GitHub には残ったまま結果だけ消え、作られていないと思って再実行した開発者が
  // draft issue を重複させる。保存側は creating で、作成側は saving を渡して止める。
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
          {/*
            どこに作られるのかは、作る直前ではなく常に見えている必要がある。
            作った draft issue は取り消せない（ADR 0009）。
          */}
          <span className="badge badge-target">
            {board.repositoryOwner}/{board.repositoryName}
          </span>
          {/*
            押せない理由は title に隠さず、本文として出す。title はホバーでしか
            読めず、disabled なボタンはフォーカスも当たらないので、キーボードと
            読み上げの利用者には理由が届かない。
          */}
          {board.targetLocked ? (
            // 固定済みなら変更手段を出さない。押せるのに 409 で断るより、
            // 押せないことを見せるほうが状態として正しい。
            <span className="hint">作成先は確定（draft issue を作成済み）</span>
          ) : (
            <>
              <button
                type="button"
                onClick={onChangeTarget}
                // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる。
                // 黙って捨てずに、保存してからにしてもらう。
                disabled={dirty || saving}
                aria-describedby={dirty ? "target-change-blocked" : undefined}
              >
                作成先を変更
              </button>
              {dirty && (
                <span className="hint" id="target-change-blocked">
                  保存してから作成先を変更できます
                </span>
              )}
            </>
          )}
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
          saving={saving}
          onCreate={(id, interpretation) => void create(id, interpretation)}
        />
      </div>
    </div>
  );
}
