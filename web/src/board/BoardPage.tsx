import { Excalidraw, serializeAsJSON } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { ApiError, boardsApi } from "../api/boards";
import type {
  AnnotationStatus,
  BoardDetail,
  Granularity,
  Interpretation,
  ProjectAccess,
} from "../api/types";
import {
  frameIds,
  isAnnotation,
  markAsAnnotation,
  selectableFrames,
  unmarkAnnotation,
  type SceneElement,
  type SelectableFrame,
} from "../excalidraw/annotation";
import { sceneSignature } from "../excalidraw/dirty";
import { exportAnnotationImage } from "../excalidraw/image";
import {
  AnnotationPanel,
  type CreationState,
  type InterpretationState,
} from "./AnnotationPanel";
import { createGenerations } from "./generation";
import { MemberPanel } from "./MemberPanel";
import { projectLink } from "./projectLink";
import { ROLE_LABELS } from "./roles";

type Props = {
  board: BoardDetail;
  onError: (message: string) => void;
  /** 作成先を選び直す。固定済みなら呼ばれない。 */
  onChangeTarget: () => void;
  /**
   * 未保存かどうかを親に伝える。
   *
   * キャンバスから離れる導線（ボードの切り替え、作成先の選択）は親が持って
   * いるので、止めるかどうかを判断する材料をそこへ渡す必要がある。
   */
  onDirtyChange: (dirty: boolean) => void;
};

export function BoardPage({ board, onError, onChangeTarget, onDirtyChange }: Props) {
  // viewer は読むだけ。解釈も許さない（ADR 0017）。
  const canEdit = board.role !== "viewer";

  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
  const [annotations, setAnnotations] = useState<AnnotationStatus[]>([]);
  const [selectedFrames, setSelectedFrames] = useState<SelectableFrame[]>([]);
  // キャンバスにいま在る frame。状態欄のカードが飛べるかどうかの判定に使う。
  // 状態は保存済みシーンが基準なので、未保存で消したフレームは一覧に残る。
  //
  // null は「Excalidraw からまだ聞いていない」。空配列と混ぜると、マウント直後の
  // 一瞬だけ全部のカードが「キャンバスにありません」になる。
  const [canvasFrameIds, setCanvasFrameIds] = useState<string[] | null>(null);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  // 他の人が先に保存していて、こちらの保存を拒まれた状態（ADR 0020）。
  // 未保存のまま残すので、dirty とは別に持つ。
  const [conflicted, setConflicted] = useState(false);
  const [interpretations, setInterpretations] = useState<
    Record<string, InterpretationState>
  >({});
  const [creations, setCreations] = useState<Record<string, CreationState>>({});
  // 実行中の解釈を無効にするための世代。useState の初期化関数で 1 度だけ作る。
  const [generations] = useState(createGenerations);
  // 作成は解釈とは別の世代で管理する。共有すると、片方の実行がもう片方の
  // 応答まで無効にしてしまう。
  const [creationGenerations] = useState(createGenerations);
  // メンバーの一覧を開いているかどうか。
  const [showingMembers, setShowingMembers] = useState(false);
  // 作成先の Project に書けるかどうか。確かめるまでは unknown。
  //
  // ボードの取得とは別に訊く。GitHub が未設定・不通でもボードは開ける必要が
  // あるため（ADR 0017）。
  const [projectAccess, setProjectAccess] = useState<ProjectAccess>("unknown");

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const access = await boardsApi.access(board.id);
        if (!cancelled) setProjectAccess(access.projectAccess);
      } catch {
        // 確かめられなかったことをエラーにしない。GitHub が落ちているだけで
        // ボードが開けなくなる理由が無い。unknown のままにする。
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [board.id]);

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

  // 保存の基準にする版。「サーバーが持っているシーンはどれか」を指す（ADR 0020）。
  // 署名が「何を描いたか」を持つのに対して、こちらは「何の上に描いたか」を持つ。
  const baseUpdatedAt = useRef(board.updatedAt);

  // 作成先の変更などでボードを取り直したら基準も差し替える。据え置くと、
  // 自分の操作でずれた版のせいで以後の保存が必ず衝突する。
  useEffect(() => {
    baseUpdatedAt.current = board.updatedAt;
    setConflicted(false);
  }, [board.updatedAt]);

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  // 外れるときは未保存を下ろす。残すと、キャンバスがもう無いのに親が止め続ける。
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  // 未保存のあいだだけ離脱を確認する。ブレストは etoki の最初のフェーズなので、
  // ここで失うと後段（注釈・解釈・作成）が全部やり直しになる。保存は明示操作で
  // ある以上（中核思想 3）押し忘れは構造的に起きるので、**自動で保存しないなら
  // 失う直前に知らせる責任が対になる。**
  useEffect(() => {
    if (!dirty) return;

    const confirmLeave = (e: BeforeUnloadEvent) => {
      // 文面はブラウザが決める。ここで渡した文字列は表示されない。
      e.preventDefault();
      // preventDefault だけを見ないブラウザが残っているので両方立てる。
      e.returnValue = "";
    };

    window.addEventListener("beforeunload", confirmLeave);
    return () => window.removeEventListener("beforeunload", confirmLeave);
  }, [dirty]);

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
      setSelectedFrames(selectableFrames(els, appState.selectedElementIds));
      setCanvasFrameIds(frameIds(els));
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

  /**
   * キャンバスをそのフレームへ寄せて選択する。
   *
   * パネルの項目とキャンバスのフレームを結ぶ唯一の手段（ADR 0022）。
   * 選択とスクロールは appState 側の話で、`sceneSignature` は elements しか
   * 見ないので、これで未保存にはならない。
   */
  const focusFrame = useCallback(
    (frameId: string) => {
      if (!api) return;
      const frame = currentElements().find((el) => el.id === frameId);
      if (!frame) return;

      api.updateScene({ appState: { selectedElementIds: { [frameId]: true } } } as never);
      api.scrollToContent(frame as never, { fitToContent: true, animate: true });
    },
    [api, currentElements],
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

      const { updatedAt } = await boardsApi.saveScene(
        board.id,
        scene,
        baseUpdatedAt.current,
      );
      // 返った版が次の基準。捨てると 2 回目の保存が必ず衝突する。
      baseUpdatedAt.current = updatedAt;
      setConflicted(false);
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
      // 409 は「保存に失敗した」ではなく「他の人が先に保存した」という状態。
      // こちらの編集は未保存のまま残す。捨てて読み直すと、消えるのは相手では
      // なくこちらの作業になる（ADR 0020）。
      if (e instanceof ApiError && e.status === 409) {
        setConflicted(true);
        return;
      }
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
        // 画像は画面から書き出す。テキストは保存済みシーンから取るので、
        // 未保存のあいだは押させない（ADR 0018）。ここに来た時点で両者は
        // 揃っている。
        const image = api ? await exportAnnotationImage(api, annotationId) : undefined;
        if (!generations.isCurrent(annotationId, generation)) return;

        const result = await boardsApi.interpret(board.id, annotationId, image);
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
    [api, board.id, generations],
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

  const annotationIdsOnCanvas = new Set(
    currentElements()
      .filter((el) => isAnnotation(el))
      .map((el) => el.id),
  );
  const markable = selectedFrames.filter((f) => !annotationIdsOnCanvas.has(f.id));
  const unmarkable = selectedFrames.filter((f) => annotationIdsOnCanvas.has(f.id));

  // 作った draft issue を確かめにいく先。未選択のボードでは null（ADR 0025）。
  const link = projectLink(board);

  return (
    <div className="board">
      <header className="board-header">
        <h1>{board.name}</h1>
        <div className="board-actions">
          {/*
            自分が何をできるのかは、操作して断られる前に見えている必要がある。
            共有すると「開けるが書けない」が普通に起きる（ADR 0017）。
          */}
          <span className="badge badge-role">{ROLE_LABELS[board.role]}</span>
          <button type="button" onClick={() => setShowingMembers((v) => !v)}>
            {showingMembers ? "メンバーを閉じる" : "メンバー"}
          </button>
          {/*
            どこに作られるのかは、作る直前ではなく常に見えている必要がある。
            作った draft issue は取り消せない（ADR 0009）。

            飛び先が組めるならリンクにする。取り消せない操作の結果を確かめる
            導線がここから始まる（ADR 0025）。組めないのは作成先が未選択の
            ボードだけなので、そのときはこれまでどおり文字のまま出す。
          */}
          {link ? (
            <a
              className="badge badge-target"
              href={link.href}
              target="_blank"
              rel="noreferrer"
              title={
                link.exact
                  ? "作成先の Project を GitHub で開く"
                  : "リポジトリの Projects を GitHub で開く"
              }
            >
              {board.repositoryOwner}/{board.repositoryName}
            </a>
          ) : (
            <span className="badge badge-target">
              {board.repositoryOwner}/{board.repositoryName}
            </span>
          )}
          {/*
            押せない理由は title に隠さず、本文として出す。title はホバーでしか
            読めず、disabled なボタンはフォーカスも当たらないので、キーボードと
            読み上げの利用者には理由が届かない。
          */}
          {board.role !== "owner" ? (
            // 作成先を変えられるのは owner だけ（ADR 0017）。押せるのに 403 で
            // 断るより、押せないことを見せるほうが状態として正しい。
            <span className="hint">作成先を変えられるのはオーナーだけです</span>
          ) : board.targetLocked ? (
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
          {canEdit && (
            <button
              type="button"
              onClick={() => void save()}
              disabled={saving || creating || !api}
              title={creating ? "作成が終わるまで保存できません" : undefined}
            >
              {saving ? "保存中…" : "保存"}
            </button>
          )}
        </div>
      </header>

      {/*
        上書きしなかったことと、いま何ができるのかを本文で出す。バッジに畳むと
        「保存できていない」ことしか伝わらず、相手の変更を消したのかどうかが
        読めない（ADR 0020）。
      */}
      {conflicted && (
        <p className="conflict" role="alert">
          {"他の人がこのボードを保存しました。上書きしないよう未保存のまま残しています。"}
          {"いまの内容を控えてから開き直してください。"}
        </p>
      )}

      {showingMembers && (
        <MemberPanel
          boardId={board.id}
          role={board.role}
          onClose={() => setShowingMembers(false)}
        />
      )}

      <div className="board-body">
        <div className="canvas">
          <Excalidraw
            excalidrawAPI={setApi}
            initialData={initialData as never}
            onChange={handleChange as never}
            langCode="ja-JP"
            // viewer には描かせない。描けるのに保存できないと、描いた内容を
            // 黙って捨てることになる（ADR 0017）。
            viewModeEnabled={!canEdit}
          />
        </div>

        <AnnotationPanel
          annotations={annotations}
          markableFrames={markable}
          unmarkableFrames={unmarkable}
          canvasFrameIds={canvasFrameIds}
          selectedFrameIds={selectedFrames.map((f) => f.id)}
          onFocusFrame={focusFrame}
          onMark={handleMark}
          onUnmark={handleUnmark}
          onChangeGranularity={(id, g) => handleMark(id, g)}
          stale={dirty}
          interpretations={interpretations}
          onInterpret={(id) => void interpret(id)}
          creations={creations}
          saving={saving}
          onCreate={(id, interpretation) => void create(id, interpretation)}
          canEdit={canEdit}
          projectAccess={projectAccess}
          projectLink={link}
        />
      </div>
    </div>
  );
}
