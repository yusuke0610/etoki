import { CaptureUpdateAction } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useMemo, useRef, useState } from "react";

import type { DiagramKind, Granularity } from "../api/types";
import type { Theme } from "../app/theme";
import {
  annotationMetas,
  annotationNames,
  frameIds,
  markAsAnnotation,
  setAnnotationKind,
  setAnnotationName,
  selectableFrames,
  unmarkAnnotation,
  type AnnotationMeta,
  type SceneElement,
  type SelectableFrame,
} from "../excalidraw/annotation";
import {
  annotationBoxes,
  type AnnotationBox,
  type Viewport,
} from "../excalidraw/annotationOverlay";
import { sceneSignature } from "../excalidraw/dirty";

/** Excalidraw の `onChange` が渡す appState のうち、ここが読むもの。 */
type CanvasAppState = {
  selectedElementIds: Record<string, boolean>;
  scrollX: number;
  scrollY: number;
  zoom: { value: number };
  viewBackgroundColor: string;
  theme: Theme;
};

export type CanvasAnnotations = {
  /** Excalidraw の `onChange` に渡す。**同一性を保つ**（依存は呼ぶ側の安定した値だけ）。 */
  handleChange: (elements: readonly unknown[], appState: CanvasAppState) => void;
  /** 現在のシーンを elements ごと差し替える。人の操作として「元に戻す」に積む（#144）。 */
  updateElements: (next: SceneElement[], appState?: Record<string, unknown>) => void;
  currentElements: () => SceneElement[];
  /** いまキャンバスに出ている背景色（`sceneSignature` の入力）。 */
  currentBackground: () => string | undefined;
  selectedFrames: SelectableFrame[];
  canvasFrameIds: string[] | null;
  canvasMetas: Record<string, AnnotationMeta> | null;
  canvasNames: Record<string, string> | null;
  overlayBoxes: AnnotationBox[];
  handleMark: (frameId: string, granularity: Granularity) => void;
  handleChangeAnnotationKind: (frameId: string, kind: DiagramKind | undefined) => void;
  handleChangeAnnotationName: (frameId: string, name: string) => void;
  handleUnmark: (frameId: string) => void;
};

/**
 * キャンバスの写し（#352 で `BoardPage` から切り出し）。Excalidraw の `onChange` で
 * 届くものから、選んでいる frame・キャンバスに在る frame と注釈の粒度・種別・名前・
 * 重ねる枠を引き、注釈の付け外しと書き換えの口を持つ。
 *
 * **状態の基準は保存済みシーン**（`useAnnotationStates`）。ここが持つのは「いま
 * キャンバスに出ているもの」で、次の保存まで保存済みの値とずれる。
 *
 * **`handleChange` と `updateElements` の同一性を保つ**（ADR 0065、web/CLAUDE.md）。
 * 依存に置くのは呼ぶ側から受け取る安定した値だけにしてある。崩すと Excalidraw の
 * `onChange` が差し替わり続け、`Maximum update depth exceeded` で落ちる。気づけるのは
 * E2E だけ。
 */
export function useCanvasAnnotations({
  api,
  theme,
  onThemeChange,
  applySignature,
  scheduleMeasure,
}: {
  api: ExcalidrawImperativeAPI | null;
  /** 画面の配色。持ち主は App（ADR 0055）。 */
  theme: Theme;
  /** キャンバスのメニューでテーマが切り替えられた。 */
  onThemeChange: (theme: Theme) => void;
  /** 未保存の判定に署名を渡す（`useDirtyScene`）。 */
  applySignature: (signature: string) => void;
  /** シーンの大きさを数え直す（`useSceneSize`）。 */
  scheduleMeasure: () => void;
}): CanvasAnnotations {
  const [selectedFrames, setSelectedFrames] = useState<SelectableFrame[]>([]);
  // キャンバスにいま在る frame。状態欄のカードが飛べるかどうかの判定に使う。
  // 状態は保存済みシーンが基準なので、未保存で消したフレームは一覧に残る。
  //
  // null は「Excalidraw からまだ聞いていない」。空配列と混ぜると、マウント直後の
  // 一瞬だけ全部のカードが「キャンバスにありません」になる。
  const [canvasFrameIds, setCanvasFrameIds] = useState<string[] | null>(null);
  // キャンバスにいま在る注釈の粒度と種別。注釈の詳細の選択欄が出す値で、保存済み
  // の値（`annotations`）とは次の保存までずれる。null の意味は上と同じ。
  const [canvasMetas, setCanvasMetas] = useState<Record<string, AnnotationMeta> | null>(
    null,
  );
  // キャンバスにいま在る注釈の名前（#249）。注釈の詳細の名前欄が出す値で、意味は
  // 上の粒度と種別と同じ。
  const [canvasNames, setCanvasNames] = useState<Record<string, string> | null>(null);
  // 注釈にした frame に重ねる枠。キャンバスの見え方が変わるたびに引き直す。
  const [overlayBoxes, setOverlayBoxes] = useState<AnnotationBox[]>([]);

  // いまのキャンバスの見え方。**state ではなく ref に持つ。** 重ねる枠の
  // 引き直しにしか使わないので、スクロールのたびに再描画を増やす理由が無い。
  const viewport = useRef<Viewport>({ scrollX: 0, scrollY: 0, zoom: 1 });
  // キャンバスが最後に言ってきたテーマ。マウント時は props のテーマで描かれる。
  // 選び直したかどうかをこれとの差で見る（`handleChange`）。
  const canvasTheme = useRef<Theme>(theme);

  /**
   * いまキャンバスに出ている背景色。
   *
   * 未保存かどうかの判定に要る（`sceneSignature`）。`appState` を丸ごと持ち回ら
   * ないのは、署名に入れてよいのは保存が書くものだけで、スクロールや選択まで
   * 混ぜると開いただけで未保存になるため。
   */
  const currentBackground = useCallback(
    () =>
      (api?.getAppState() as { viewBackgroundColor?: string } | undefined)
        ?.viewBackgroundColor,
    [api],
  );

  /** 選択状態の変化を拾い、注釈にできる frame を割り出す。 */
  const handleChange = useCallback(
    (elements: readonly unknown[], appState: CanvasAppState) => {
      const els = elements as SceneElement[];
      // キャンバスのメニューで切り替えたテーマは、ここでしか届かない。持ち主の
      // App に返して、パネルの配色も一緒に変える（ADR 0055）。
      //
      // **キャンバスが前回と違うテーマを言ってきたときだけ返す。** props と
      // 比べると、OS の設定が変わって props を差し替えた直後に、まだ古い
      // テーマのまま届く onChange を「選び直した」と取り違え、OS に従うのを
      // やめてしまう。
      //
      // **署名には入れない。** テーマは保存が書かない（`sceneJSON` の直列化が
      // 落とす）ので、入れると切り替えただけで未保存になる。
      if (appState.theme !== canvasTheme.current) {
        canvasTheme.current = appState.theme;
        if (appState.theme !== theme) onThemeChange(appState.theme);
      }
      applySignature(sceneSignature(els, appState.viewBackgroundColor));
      scheduleMeasure();
      setSelectedFrames(selectableFrames(els, appState.selectedElementIds));
      setCanvasFrameIds(frameIds(els));
      setCanvasMetas(annotationMetas(els));
      setCanvasNames(annotationNames(els));

      // スクロールとズームは onChange でしか届かない。要素が変わっていなくても
      // 引き直す必要があるので、ここでまとめて拾う。
      viewport.current = {
        scrollX: appState.scrollX,
        scrollY: appState.scrollY,
        zoom: appState.zoom.value,
      };
      setOverlayBoxes(annotationBoxes(els, viewport.current));
    },
    [applySignature, scheduleMeasure, theme, onThemeChange],
  );

  /**
   * 現在のシーンを elements ごと差し替える。
   *
   * `appState` は要素と一緒に変えるものだけを渡す（取り込みの背景色、ADR 0045）。
   * 表示状態そのものはここで触らない。
   *
   * **ここを通る変更は、人の操作として「元に戻す」に積む**（#144）。図の
   * ドラフト・注釈の付け外しと種別・取り込みが通る。どれも開発者が押して
   * 起こした変更で、置き間違いを戻す手段が要る。取り込みは確認を経た置き換え
   * だが、戻せるほうが失うものが少ない。
   */
  const updateElements = useCallback(
    (next: SceneElement[], appState?: Record<string, unknown>) => {
      // 渡された背景色があればそれが新しい色。無ければ変えていないので今の色。
      // **`updateScene` の後に `getAppState()` から読み直さない。** 反映は React
      // の更新を挟むので、直後に読むとまだ前の色が返りうる。
      const background =
        (appState?.viewBackgroundColor as string | undefined) ?? currentBackground();

      // `captureUpdate` の既定（EVENTUALLY）はすぐには履歴に積まない。積まれない
      // まま次の操作と一緒に記録されるので、戻すとこの変更ではなく直前に描いた
      // ものが消える。
      api?.updateScene({
        elements: next as never,
        appState: appState as never,
        captureUpdate: CaptureUpdateAction.IMMEDIATELY,
      });
      // onChange の発火を待たずにここでも判定する。注釈の付け外しが未保存として
      // 出るかどうかを、updateScene が onChange を呼ぶかに依存させない。
      applySignature(sceneSignature(next, background));
      // 重ねる枠も同じ理由でここで引き直す。注釈にした瞬間に枠が出ないと、
      // 付いたかどうかをパネルでしか確かめられない。
      setOverlayBoxes(annotationBoxes(next, viewport.current));
    },
    [api, applySignature, currentBackground],
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

  /**
   * 注釈の図の種別を差し替える。
   *
   * **注釈にする操作とは分ける。** 種別は「何の図として読ませるか」で、
   * 粒度（どう分解させるか）とは選ぶ場面が違う。`handleMark` に相乗りさせると、
   * 片方だけ変えたい呼び出しがもう片方の現在値を読み直して渡すことになる。
   */
  const handleChangeAnnotationKind = useCallback(
    (frameId: string, kind: DiagramKind | undefined) => {
      updateElements(setAnnotationKind(currentElements(), frameId, kind));
    },
    [currentElements, updateElements],
  );

  /**
   * 注釈の名前を書き換える（#249）。frame の `name` に書くので、キャンバスの
   * ラベルにも同じ名前が出る。名前は `content_hash` に入らないので、作成済みでも
   * 状態は変わらない（#303、ADR 0079）。
   */
  const handleChangeAnnotationName = useCallback(
    (frameId: string, name: string) => {
      updateElements(setAnnotationName(currentElements(), frameId, name));
    },
    [currentElements, updateElements],
  );

  const handleUnmark = useCallback(
    (frameId: string) => {
      updateElements(unmarkAnnotation(currentElements(), frameId));
    },
    [currentElements, updateElements],
  );

  return useMemo(
    () => ({
      handleChange,
      updateElements,
      currentElements,
      currentBackground,
      selectedFrames,
      canvasFrameIds,
      canvasMetas,
      canvasNames,
      overlayBoxes,
      handleMark,
      handleChangeAnnotationKind,
      handleChangeAnnotationName,
      handleUnmark,
    }),
    [
      handleChange,
      updateElements,
      currentElements,
      currentBackground,
      selectedFrames,
      canvasFrameIds,
      canvasMetas,
      canvasNames,
      overlayBoxes,
      handleMark,
      handleChangeAnnotationKind,
      handleChangeAnnotationName,
      handleUnmark,
    ],
  );
}
