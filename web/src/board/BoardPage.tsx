import {
  CaptureUpdateAction,
  Excalidraw,
  Footer,
  MainMenu,
} from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from "react";

import { boardsApi } from "../api/boards";
import {
  canvasMermaidPasteFailure,
  describeFailure,
  sceneUnreadableFailure,
  type Failure,
} from "../api/errorMessage";
import type {
  AnnotationStatus,
  BoardDetail,
  Capabilities,
  DetachedAnnotation,
  DiagramKind,
  Granularity,
  ProjectAccess,
} from "../api/types";
import { unavailableReason } from "../app/capability";
import {
  annotationMetas,
  frameIds,
  isAnnotation,
  markAsAnnotation,
  setAnnotationKind,
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
import { isMaybeMermaidDefinition } from "../excalidraw/excalidrawMermaid";
import { formatSceneSize } from "../excalidraw/size";
import { createTable, tableCenter } from "../excalidraw/table";
import { ErrorBoundary } from "../app/ErrorBoundary";
import { useNotify } from "../notification/NotificationProvider";
import type { NotifyOptions } from "../notification/types";
import type { Theme } from "../app/theme";
import { AnnotationOverlay } from "./annotations/AnnotationOverlay";
import {
  AnnotationDetail,
  type CreationProps,
  type InterpretationProps,
} from "./annotations/AnnotationDetail";
import {
  ANNOTATION_LIST_HEADING_ID,
  AnnotationPanel,
} from "./annotations/AnnotationPanel";
import { DiagramChatPanel } from "./diagram/DiagramChatPanel";
import { useExclusion } from "./exclusion";
import { useInterpretations } from "./annotations/useInterpretations";
import { MemberPanel } from "./members/MemberPanel";
import {
  backToListIcon,
  changeTargetIcon,
  deleteBoardIcon,
  exportIcon,
  importIcon,
  refreshTargetIcon,
  renameIcon,
  tableIcon,
} from "./menuIcons";
import { DiagramTab, type DiagramMode } from "./diagram/DiagramTab";
import { MermaidPastePanel } from "./diagram/MermaidPastePanel";
import { useDiagramDraft } from "./diagram/useDiagramDraft";
import { projectLabel } from "../boards/grouping";
import { SidePanel, type SidePanelTab } from "./SidePanel";
import { useCreation } from "./annotations/useCreation";
import { useRunHistories } from "./annotations/useRunHistories";
import { railBadgesOf, readPanelCollapsed, writePanelCollapsed } from "./panelState";
import { projectLink } from "./target/projectLink";
import { canEditBoard, isOwner, ROLE_LABELS } from "./members/roles";
import { useBoardDeletion, useRename, useTargetRefresh } from "./useBoardManagement";
import { useBoardTransfer } from "./useBoardTransfer";
import { useConfirmLeave, useDirtyScene } from "./useDirtyScene";
import { SAVE_FAILED, useSceneSave } from "./useSceneSave";
import { useSceneSize } from "./useSceneSize";

/**
 * ライブラリのメニューから閉じるもの（ADR 0045）。
 *
 * **持ち出しと取り込みの口は etoki のメニュー 1 つに寄せる。** ライブラリ側を
 * 残すと、同じ画面に意味の違う「保存」が 2 つ並び、片方だけが etoki のボード名と
 * 未保存の確認を知っている形になる。
 *
 * **`.excalidraw` を書き出しているのは `saveAsImage` ではなく `export`。** 表示は
 * 「名前を付けて保存...」で、隣の「画像のエクスポート...」と紛らわしい。
 * `saveToActiveFile` はそこで開いたファイルへの上書きなので、一緒に閉じる。
 * **画像のエクスポートは閉じない。** 答えている問いが違う（持ち出しではなく、
 * 絵を他所に貼ること）。
 *
 * **テーマの切り替えは開ける**（ADR 0055）。`theme` を渡すとライブラリは既定で
 * この項目を隠すので、明示しないと手で切り替える口が消える。
 *
 * **モジュールの定数として持つ。** 描画のたびに作り直すと、Excalidraw には
 * 毎回違うオブジェクトが渡る。
 */
const UI_OPTIONS = {
  canvasActions: {
    loadScene: false,
    export: false,
    saveToActiveFile: false,
    toggleTheme: true,
  },
} as const;

/**
 * 開いたときに effect から出る失敗の通知。
 *
 * **effect から出すものには key を付ける。** 開発時の StrictMode では effect が
 * 2 回走るので、畳まないと同じ失敗が 2 件並ぶ。注釈の状態は保存や作成のたびにも
 * 取り直すので、読めたら下げる。
 */
const ANNOTATIONS_FAILED = "annotations-failed";
const SCENE_UNREADABLE = "scene-unreadable";

type Props = {
  board: BoardDetail;
  /**
   * いま使える機能。null は「まだ確かめていない」（ADR 0030）。
   *
   * ボード単位の権限（`projectAccess`）とは別物。プロセスの設定なので、
   * ボードを開くたびに変わりはしない。**混ぜない。**
   */
  capabilities: Capabilities | null;
  /**
   * ボードを閉じて一覧へ戻る（ADR 0064）。未保存の確認は親が通す。
   *
   * **一覧はボードと同じ画面に無い。** これが無いと、開いたボードから出る
   * 手段がブラウザの「戻る」しか無くなる。口はキャンバスのメニューの先頭。
   */
  onClose: () => void;
  /** 作成先を選び直す。固定済みなら呼ばれない。 */
  onChangeTarget: () => void;
  /**
   * 作成先の表示名を取り直したので、手元のボードを差し替えてもらう。
   *
   * 版（`updatedAt`）も進むので、持ち主である App が持ち替える必要がある。
   */
  onTargetRefreshed: (board: BoardDetail) => void;
  /**
   * 名前を変えたので、手元のボードを差し替えてもらう。
   *
   * 版（`updatedAt`）は動かないので、保存の基準は据え置きでよい（ADR 0020）。
   */
  onRenamed: (board: BoardDetail) => void;
  /**
   * ボードを削除したので、手元から外してもらう。
   *
   * **確認はこの中で済ませてある**（ADR 0042）。親は未保存の確認を重ねない。
   * 消えたのはボードそのもので、未保存の編集を捨てるかどうかを訊いても
   * 戻せる先が無い。
   *
   * **消えた ID を渡す。** 親は一覧からその 1 件を外すのに使う。開いている
   * ボードから読み直させると、遅れて呼ばれたときに別のボードを外しうる。
   */
  onDeleted: (id: string) => void;
  /**
   * 未保存かどうかを親に伝える。
   *
   * キャンバスから離れる導線（ボードの切り替え、作成先の選択）は親が持って
   * いるので、止めるかどうかを判断する材料をそこへ渡す必要がある。
   */
  onDirtyChange: (dirty: boolean) => void;
  /** 画面の配色。持ち主は App（ADR 0055）。 */
  theme: Theme;
  /**
   * キャンバスのメニューでテーマが切り替えられた。
   *
   * **ここで持ち直さない。** etoki のパネルとキャンバスで値が 2 つになり、
   * 片方だけが暗い画面が起きる。
   */
  onThemeChange: (theme: Theme) => void;
};

export function BoardPage({
  board,
  capabilities,
  onClose,
  onChangeTarget,
  onTargetRefreshed,
  onRenamed,
  onDeleted,
  onDirtyChange,
  theme,
  onThemeChange,
}: Props) {
  const canEdit = canEditBoard(board.role);

  // 画面全体に出す失敗は通知へ（ADR 0058）。**保存の衝突・解釈や作成の失敗は
  // ここを通さない。** 消えてよい失敗ではなく、残して読ませる状態だから。
  const { notify, dismissKey } = useNotify();
  // **画面から外れたあとに返った失敗は通知しない。** 通知はキャンバスより上に
  // 生きているので、ボードを離れる前に投げた保存が離れたあとで失敗すると、
  // 別のボードの画面に「保存できませんでした」が出る。その「再試行」が呼ぶのは
  // 離れたボードの save で、捨てると決めたシーンを前のボードへ保存しにいく。
  // 離れる時点で出ている通知は下げてある（SAVE_FAILED の effect）ので、ここで
  // 塞ぐのは離れる時点でまだ応答待ちだったぶん。
  //
  // 印は effect で立てる。StrictMode は effect を 2 回走らせ、そのあいだに
  // cleanup を挟むので、初期値で true にすると 2 回目以降が false のまま残る。
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  // **下げる側も同じ印を見る。** 通知はボードより上（`NotificationProvider`）に
  // 生きていて key で消すので、**離れたあとに届いた成功で下げると、いま開いて
  // いるボードに出ている同じ key の通知が消える。** 出す側（`onError`）だけを
  // 塞いでも、消える側からボードを跨いでしまう。
  //
  // **離れる時点で下げるのは別の話**なので、そちらは `dismissKey` を直に呼ぶ
  // （下の `board.id` の cleanup）。あれは「離れたから下げる」であって、
  // 「離れたのに下げる」ではない。
  const dismissHere = useCallback(
    (key: string) => {
      if (!mounted.current) return;
      dismissKey(key);
    },
    [dismissKey],
  );

  const onError = useCallback(
    (failure: Failure, options: Pick<NotifyOptions, "action" | "key"> = {}) => {
      if (!mounted.current) return;
      notify({
        kind: "error",
        message: failure.message,
        detail: failure.detail,
        ...options,
      });
    },
    [notify],
  );

  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
  const [annotations, setAnnotations] = useState<AnnotationStatus[]>([]);
  // シーンから消えたのに GitHub 側にものが残っている注釈（#111）。
  //
  // **`annotations` と混ぜない。** 3 状態も名前も無いので、注釈のカードと
  // 同じ形では出せない。同じ問い合わせで返るので、引く回数は増えない。
  const [detached, setDetached] = useState<DetachedAnnotation[]>([]);
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
  // 注釈にした frame に重ねる枠。キャンバスの見え方が変わるたびに引き直す。
  const [overlayBoxes, setOverlayBoxes] = useState<AnnotationBox[]>([]);
  // 未保存かどうかを決めるのはここだけ（`useDirtyScene`）。
  const { dirty, isDirty, applySignature, markSaved } = useDirtyScene(onDirtyChange);
  /**
   * 保存・取り込み・作成の排他（#146）。**表も判定も `exclusion.ts` にある。**
   *
   * 以前は `exclusiveOperation`（ref）・`saving` / `importing`（state）・
   * `creations` からの導出（`creating`）に分かれていて、どの操作とどの操作が
   * 排他かを知るにはこのファイルを通して読むことになっていた。
   */
  const exclusive = useExclusion();
  // 表示のための別名。**ここで条件を組み直さない。** 何が走っているかを
  // 知っているのは `exclusive` だけで、こちらはその言い換え。
  const saving = exclusive.running === "saving";
  const importing = exclusive.running === "importing";
  const creating = exclusive.running === "creating";
  // いまのシーンの大きさ（`useSceneSize`）。**束のまま持たない。**
  // `handleChange` は `scheduleMeasure` を依存に置くので、束で受けると
  // バイト数が変わるたびに `onChange` ごと差し替わる。
  const { bytes: sceneBytes, schedule: scheduleMeasure } = useSceneSize(api);
  // 引いた解釈（`useInterpretations`）。束のまま持たない理由は上の `useSceneSize` と
  // 同じで、`useCreation` と `discardAfterSave` が中の関数を依存に置く。
  const {
    states: interpretations,
    interpret,
    select: showInterpretation,
    recordCreated,
    discardAll: discardInterpretations,
  } = useInterpretations({ api, boardId: board.id, annotations });
  // 右のパネルでどのタブを開いているか（`SidePanel`）。既定は注釈。
  const [panelTab, setPanelTab] = useState<SidePanelTab>("annotations");
  // 右のパネルを畳んでいるか（#202）。端末ごとに覚えた値で始める。
  const [panelCollapsed, setPanelCollapsed] = useState(() => readPanelCollapsed());
  const changePanelCollapsed = useCallback((collapsed: boolean) => {
    setPanelCollapsed(collapsed);
    writePanelCollapsed(collapsed);
  }, []);
  // 図のドラフトのタブで、LLM に作らせるか mermaid を貼るか（`DiagramTab`）。
  const [diagramMode, setDiagramMode] = useState<DiagramMode>("generate");
  // キャンバスの上に開いている注釈の詳細（`AnnotationDetail`）。null なら閉じている。
  const [detailId, setDetailId] = useState<string | null>(null);
  // 詳細を開く操作の回数。開いたままの注釈を開き直しても焦点を移すために、
  // 注釈 ID とは別に持つ（`AnnotationDetail`）。
  const [detailRequest, setDetailRequest] = useState(0);
  const openDetail = useCallback((id: string) => {
    setDetailId(id);
    setDetailRequest((n) => n + 1);
  }, []);
  // 開いていた詳細を、注釈が消えたので閉じた回数（下の effect が焦点を移す）。
  // **ID ではなく回数で持つ。** 同じ注釈が戻ってまた消えたとき、ID だと値が
  // 変わらず effect が走らない。
  const [detailVanished, setDetailVanished] = useState(0);
  // 作成先の Project に書けるかどうか。確かめるまでは unknown。
  //
  // ボードの取得とは別に訊く。GitHub が未設定・不通でもボードは開ける必要が
  // あるため（ADR 0017）。
  const [projectAccess, setProjectAccess] = useState<ProjectAccess>("unknown");
  // ボードそのものの管理（`useBoardManagement`）。改名・削除・作成先の名前の
  // 取り直しは互いに関わらないので、フックも分けてある。
  const {
    draft: nameDraft,
    setDraft: setNameDraft,
    renaming,
    rename,
  } = useRename({ board, onRenamed, onError });
  const {
    state: deletion,
    ask: askDelete,
    confirm: deleteBoard,
    cancel: cancelDelete,
  } = useBoardDeletion({ boardId: board.id, onDeleted, onError });
  const { refreshing: refreshingTarget, refresh: refreshTargetDisplay } =
    useTargetRefresh({
      board,
      onTargetRefreshed,
      onError,
    });
  // 注釈ごとの実行履歴（`useRunHistories`）。**束のまま持たない。** `useCreation` は
  // `discardRuns` を依存に置くので、束で受けると履歴を引くたびに作り直される。
  const {
    states: runHistories,
    load: loadRuns,
    discard: discardRuns,
  } = useRunHistories(board.id);

  /**
   * 確認が出たら、そこへフォーカスを移す。
   *
   * **移さないとキーボードの居場所が消える。** 押した「ボードを削除」は確認が
   * 開くと disabled になり、focus を body へ落とす。取り消せない操作の直前で
   * 行き先を失わせない。
   */
  const focusDeleteConfirm = useCallback((node: HTMLElement | null) => {
    node?.focus();
  }, []);

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

  const initialScene = useMemo(() => {
    try {
      const scene = JSON.parse(board.scene) as { elements?: unknown; appState?: unknown };
      // **開いたら中身が見えている位置から始める。** シーンの原点はキャンバスの
      // 左上に来るので、原点あたりに描かれたものはツールバーの下に隠れる。
      // ひな形から作ったボードは必ずそこから始まる（`excalidraw/template.ts`）
      // ので、見出しが隠れた状態が最初の 1 画面になる。
      //
      // **要素は動かさない。** 動かすと座標の変更として未保存になり、開いた
      // だけで保存を促すことになる。動かすのは見ている位置のほう。
      return { data: { ...scene, scrollToContent: true }, unreadable: false };
    } catch {
      // 保存時に検証しているのでここには来ないはずだが、来たら空で開く。
      return { data: { elements: [], appState: {} }, unreadable: true };
    }
  }, [board.scene]);
  const initialData = initialScene.data;

  // 読めなかったことの通知は描画の外で出す。useMemo の中で出すと、描画中に
  // 別のコンポーネント（通知）の状態を書き換えることになる。
  useEffect(() => {
    if (initialScene.unreadable)
      onError(sceneUnreadableFailure(), { key: SCENE_UNREADABLE });
  }, [initialScene, onError]);

  // 状態の取得は初期表示・保存・作成から重なって走る。番号を振って最後に
  // 投げたものだけ反映する。古い応答で上書きすると、作成済みの注釈が未作成に
  // 巻き戻って見える。
  const annotationsRequest = useRef(0);

  const refreshAnnotations = useCallback(async () => {
    const request = ++annotationsRequest.current;
    try {
      const next = await boardsApi.annotations(board.id);
      if (request !== annotationsRequest.current) return;
      setAnnotations(next.annotations);
      setDetached(next.detached);
      dismissHere(ANNOTATIONS_FAILED);
    } catch (e) {
      if (request !== annotationsRequest.current) return;
      onError(describeFailure("注釈の状態を取得できませんでした", e), {
        key: ANNOTATIONS_FAILED,
      });
    }
  }, [board.id, dismissHere, onError]);

  // 開いたら注釈の状態を引く。**`set-state-in-effect` の対象外。** state を置くのは
  // 応答が届いてから（`await` の後）で、effect の本体からは置かない。BoardPage が
  // 大きかったうちは解析が諦めていて出ていなかった（`web/CLAUDE.md` の「関心を
  // フックに切り出すとき」）。
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refreshAnnotations();
  }, [refreshAnnotations]);

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

  /**
   * mermaid らしい文字列の貼り付けを、Excalidraw が描く前に止める（ADR 0067）。
   *
   * Excalidraw はこれに当たる文字列を etoki の守り（ADR 0040 / 0061）を通さずに
   * mermaid で描く。**当たらないものは素通しする。** `false` を返すと貼り付け
   * そのものが止まるので、広げると普通の文字も貼れなくなる。
   */
  const handlePaste = useCallback(
    (data: { text?: string }) => {
      if (data.text === undefined || !isMaybeMermaidDefinition(data.text)) return true;
      onError(canvasMermaidPasteFailure());
      return false;
    },
    [onError],
  );

  /** 選択状態の変化を拾い、注釈にできる frame を割り出す。 */
  const handleChange = useCallback(
    (
      elements: readonly unknown[],
      appState: {
        selectedElementIds: Record<string, boolean>;
        scrollX: number;
        scrollY: number;
        zoom: { value: number };
        viewBackgroundColor: string;
        theme: Theme;
      },
    ) => {
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

  // 図のドラフト（LLM に作らせる・mermaid を貼る・置く）。`useDiagramDraft`。
  const {
    chat,
    generate: generateDiagram,
    changeKind: handleChangeKind,
    placeDraft,
    pasteText,
    setPasteText,
    pasteMermaid,
  } = useDiagramDraft({ api, boardId: board.id, currentElements, updateElements });

  /**
   * 表を 1 つ置く（ADR 0069）。
   *
   * **ダイアログを挟まない。** 描いている最中の操作なので、手数の少なさが
   * そのまま値打ちになる。大きさは固定で、足りなければ複製やセルの追加で
   * 広げる。置くだけで保存はしない。確定させるのは人間の保存操作だけ。
   *
   * 置いた表は group ごと選んでおく。そのまま動かしたり、消したりできる。
   */
  const addTable = useCallback(() => {
    if (!api) return;

    const { scrollX, scrollY, zoom, width, height } = api.getAppState();
    const table = createTable(
      tableCenter({ scrollX, scrollY, zoom: zoom.value, width, height }),
    );
    updateElements([...currentElements(), ...table]);

    const groupId = table[0]?.groupIds?.[0];
    if (groupId !== undefined) {
      // 選択を渡す `updateScene` はツールを切り替えない。矩形ツールのまま置くと、
      // 次のポインター操作で選択が外れて新しい矩形を描いてしまい、置いた直後に
      // 表を動かせない。
      api.setActiveTool({ type: "selection" });
      api.updateScene({
        appState: {
          selectedElementIds: Object.fromEntries(table.map((el) => [el.id, true])),
          selectedGroupIds: { [groupId]: true },
        },
      } as never);
    }
  }, [api, currentElements, updateElements]);

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
   * `sceneSignature` が appState から見るのは背景色だけで、選択とスクロールは
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

  const { fileInput, exportScene, importScene } = useBoardTransfer({
    api,
    boardName: board.name,
    exclusive,
    isDirty,
    updateElements,
    onError,
  });

  // 解釈結果から draft issue を作る（`useCreation`）。
  const {
    states: creations,
    create,
    discardAll: discardCreations,
  } = useCreation({
    boardId: board.id,
    exclusive,
    recordCreated,
    discardRuns,
    refreshAnnotations,
  });

  /**
   * 保存が済んだら捨てるもの。**一覧はここ 1 箇所にある**（#146）。
   *
   * - **引いた解釈。** 保存済みシーンに対する結果なので、対象が変わった。
   * - **作成の結果。** どの解釈に対して作ったのかが読めなくなる。
   * - **それぞれの世代。** 実行中のものが後から返って復活すると、いまの内容を
   *   解釈したものだと誤読される。
   *
   * **図のドラフトの会話は捨てない。** 生成は保存済みシーンを読まないので、
   * 保存しても前提が変わらない（解釈との非対称、ADR 0041）。
   *
   * 最後に注釈の状態を取り直す。**これは etoki 自身の状態の読み直しであって
   * GitHub への同期ではない**（`.claude/rules/async-ui.md`）。
   */
  const discardAfterSave = useCallback(async () => {
    discardInterpretations();
    discardCreations();
    await refreshAnnotations();
  }, [discardCreations, discardInterpretations, refreshAnnotations]);

  const { conflicted, overLimit, save } = useSceneSave({
    api,
    boardId: board.id,
    updatedAt: board.updatedAt,
    sceneOverLimit: board.sceneOverLimit,
    exclusive,
    currentBackground,
    markSaved,
    onSaved: discardAfterSave,
    onError,
    dismiss: dismissHere,
  });

  // **ボードを変えたら、そのボードについての通知は全部下げる。** 通知は
  // キャンバスより上に生きているので、残すと別のボードの画面に並ぶ。
  //
  // - `SAVE_FAILED` — 「再試行」が呼ぶのは押した時点の save、つまり**いま開いて
  //   いるボードの保存**なので、読んでいる文と起きることが食い違う。
  // - `SCENE_UNREADABLE` — 下げる経路がここしか無い。残すと、読めたボードが
  //   空で開いたように読める。
  // - `ANNOTATIONS_FAILED` — 次のボードの取得が成功するまで前のボードの失敗が
  //   残る。
  //
  // 次のボードが出す通知は消さない。前のボードの cleanup は、次のボードの effect
  // より先に走る。
  useEffect(
    () => () => {
      for (const key of [SAVE_FAILED, SCENE_UNREADABLE, ANNOTATIONS_FAILED])
        dismissKey(key);
    },
    [board.id, dismissKey],
  );

  // 未保存のあいだと、作成の実行中は離脱を確認する（ADR 0021 / 0051）。
  // ブレストは etoki の最初のフェーズなので、ここで失うと後段（注釈・解釈・
  // 作成）が全部やり直しになる。**2 つを渡すのはここ**で、理由は
  // `useConfirmLeave` にある。
  useConfirmLeave(dirty || creating);

  // 押せない理由は表から引く（`exclusion.ts`）。**ここで条件を組み直さない。**
  // 組み直すと、止める条件と画面に出る文が別々に古くなる。
  const importBlocked = exclusive.reasonFor("importing");
  const saveBlocked = exclusive.reasonFor("saving");

  /**
   * 保存を押せるか。**ボタンの `disabled` とショートカットで同じ式を使う。**
   *
   * 2 つに分けて書くと、片方だけに条件を足したときに、押せないはずの経路が
   * キーボードからだけ通る。
   *
   * 走っている操作があれば押させない。表の `saving` 行はどの操作に対しても
   * 止める（対角も `useExclusion` が弾く）ので、`running` を見れば足りる
   * （`exclusion.ts`）。
   */
  const canSave = canEdit && api !== null && exclusive.running === null;

  /**
   * Ctrl / Cmd + S で保存する（issue #145）。
   *
   * 保存は明示操作だけ（ADR 0021）なので、押すまでの手数の少なさがそのまま
   * 値打ちになる。**誰も拾わないと既定の動作（ブラウザの「ページを保存」）に
   * 落ちる。** Excalidraw 側の Ctrl+S は `saveToActiveFile` だが、etoki は
   * それを `UIOptions` から外してある（ADR 0045）ので、押しても何も起きず
   * `preventDefault` もされない。
   *
   * **押せないときも既定の動作は止める。** 「保存できなかった」の代わりに
   * ブラウザの保存ダイアログが出るのは、押せない理由を見せるどころではない。
   *
   * **Shift は拾わない。** Ctrl/Cmd+Shift+S は Excalidraw 側の書き出しで、
   * 別の操作（`web/e2e/scene.spec.ts` が走らないことを見ている）。
   *
   * **入力欄にフォーカスがあっても同じ扱いにする。** この画面で Ctrl+S が
   * 指しうる保存はシーンの保存 1 つだけで、ボード名の変更には専用のボタンが
   * ある。フォーカス位置で意味が変わるほうが、習慣で押す人には読めない。
   */
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      // **`e.key` だけで見ない。** 非ラテン配列では物理の S を押しても `e.key` が
      // "s" にならず、保存されないままブラウザの既定（ページを保存）が開く。
      // 物理キーの位置（`e.code`）も見る。Ctrl+S の習慣は位置で覚えているため。
      if (e.key !== "s" && e.key !== "S" && e.code !== "KeyS") return;
      if (!(e.ctrlKey || e.metaKey) || e.shiftKey || e.altKey) return;

      e.preventDefault();
      if (!canSave) return;
      void save();
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [canSave, save]);

  // 設定していない機能は、押す前に理由を出す（ADR 0030）。null は使える、
  // または「まだ確かめていない」。
  //
  // **引くのはここ 1 箇所で、下へは文言として渡す。** 各コンポーネントで
  // 引き直すと、同じ判定が枝の数だけ増える。
  const interpretationUnavailable = unavailableReason(capabilities, "interpretation");
  // **解釈とは別に引く。** 同じ LLM の設定で決まるが、答えている問いが違う
  // （ADR 0041）。片方から推し量ると、使えると案内したほうが 503 を返す。
  const diagramUnavailable = unavailableReason(capabilities, "diagramDraft");
  const creationUnavailable = unavailableReason(capabilities, "creation");
  const sharingUnavailable = unavailableReason(capabilities, "sharing");

  const annotationIdsOnCanvas = new Set(
    currentElements()
      .filter((el) => isAnnotation(el))
      .map((el) => el.id),
  );
  const markable = selectedFrames.filter((f) => !annotationIdsOnCanvas.has(f.id));
  const unmarkable = selectedFrames.filter((f) => annotationIdsOnCanvas.has(f.id));

  // 作った draft issue を確かめにいく先。未選択のボードでは null（ADR 0025）。
  //
  // **中身をプリミティブに落として持つ。** `projectLink` は呼ぶたびに新しい
  // オブジェクトを返すので、そのまま下の `useMemo` の依存に入れると毎回作り直しに
  // なり、キャンバスとの描き直しの輪が閉じなくなる（`footerUI` を参照）。
  const link = projectLink(board);
  const linkHref = link?.href ?? null;
  const linkExact = link?.exact ?? false;
  const running = exclusive.running;

  // **開いていた詳細の注釈が保存で消えたら、詳細を閉じる。** 描いたまま残すと、
  // 同じ注釈が戻ったとき（元に戻して保存したとき）に、閉じたはずの詳細が開き直る。
  // 描いている最中に置く。前の値と違うときだけ置く形なので React が許している。
  //
  // **一覧が壊れていても投げない。** ここはパネルの境界の外なので、投げると画面
  // ごと落ちる（ADR 0027、`railBadgesOf` と同じ）。
  if (
    detailId !== null &&
    !(annotations as readonly (AnnotationStatus | null)[]).some((a) => a?.id === detailId)
  ) {
    setDetailVanished((n) => n + 1);
    setDetailId(null);
  }
  // 閉じた詳細の中に焦点があったなら、一覧の見出しへ移す。**焦点が行き場を
  // 失ったときだけ。** キャンバスで描いている最中に保存して消えたなら、焦点は
  // キャンバスにあるので動かさない。
  useEffect(() => {
    if (detailVanished === 0) return;
    if (document.activeElement === null || document.activeElement === document.body) {
      document.getElementById(ANNOTATION_LIST_HEADING_ID)?.focus();
    }
  }, [detailVanished]);

  // 解釈の口。詳細の帯に渡す（#201）。
  const interpretation: InterpretationProps = {
    states: interpretations,
    // 押したら詳細を開く。結果はそこに出るので、開かないと押したあとに何が
    // 起きたかが見えない。
    onInterpret: (id) => {
      openDetail(id);
      void interpret(id);
    },
    onSelect: showInterpretation,
    unavailable: interpretationUnavailable,
  };
  // 作る先の見出し。帯の「選んだ N 件を … に作ります」に出す。作成先が未選択の
  // ボードでは作成まで進めないので null。
  const targetLabel =
    board.projectId === ""
      ? null
      : `${board.repositoryOwner}/${board.repositoryName} › ${projectLabel(board)}`;

  // 作成の口。作るのは詳細の中だけ。
  const creation: CreationProps = {
    states: creations,
    saving,
    blocked: exclusive.reasonFor("creating"),
    onCreate: (id, interpretationId, result) => void create(id, interpretationId, result),
    projectAccess,
    unavailable: creationUnavailable,
  };

  // 作成先を変更できない理由。押せるなら null（ADR 0039）。
  //
  // **未保存が先。** `dirty` を下ろすのは応答が返ってから（`save`）なので、
  // 保存中もこちらが出続ける。保存中の文が出るのは、変更が無いまま保存を
  // 押したとき。そこも押せないことに変わりはないので、理由を空けない。
  // **未保存は排他ではない。** 表が答えるのは「何が走っているか」だけなので、
  // 保存していないという前提はここで先に見る（`blockingReasons` に進行中の
  // 状態を混ぜないのと同じ切り分け）。
  const targetChangeBlocked = dirty
    ? "保存してから作成先を変更できます"
    : exclusive.reasonFor("changeTarget");

  /*
   * 右上に出す。ボード名と、未保存かどうかと、保存（ADR 0065）。
   *
   * **保存だけは拡張点のメニューに入れない。** いちばん押すものであり、作成中や
   * 取り込み中に止まる理由（一時的な理由）がいちばん出る場所でもある。待たされて
   * いる本人が見ている場所で理由が読めないと意味が無い（ADR 0066）。
   *
   * **ボード名を隣に置く。** 何を保存するのかが、ボタンの隣で読める。
   */
  const topRightUI = useMemo(
    () => (
      <div className="board-status etoki-ui">
        {nameDraft === null ? (
          <h1>{board.name}</h1>
        ) : (
          <form
            className="rename-form"
            onSubmit={(e) => {
              e.preventDefault();
              void rename();
            }}
          >
            {/*
            ラベルは一覧の画面の「ボード名」（新規作成の入力）と分ける。
            入れるものが違う（作るボードの名前か、開いているボードの名前か）。
          */}
            <input
              aria-label="ボードの名前"
              value={nameDraft}
              disabled={renaming}
              onChange={(e) => setNameDraft(e.target.value)}
              /*
              jsx-a11y が禁じているのは「開いた瞬間に勝手に焦点が移る」
              autoFocus で、ここはそれに当たらない。メニューの「名前を変更」を
              押した結果としてこの入力が現れるので、移さないとキーボードの
              利用者の焦点は body に落ちる。**外すほうが a11y は悪くなる。**
              規則が見ているのは属性で、押した結果として現れたかどうかは
              見られない（ADR 0039）。
            */
              // eslint-disable-next-line jsx-a11y/no-autofocus
              autoFocus
            />
            {/*
            「保存」とは書かない。隣にシーンの保存ボタンが並んでいるので、
            同じ文言だと何を保存するのかが読めない。
          */}
            <button type="submit" disabled={renaming || nameDraft.trim() === ""}>
              {renaming ? "変更中…" : "名前を保存"}
            </button>
            <button
              type="button"
              className="quiet"
              disabled={renaming}
              onClick={() => setNameDraft(null)}
            >
              取消
            </button>
          </form>
        )}
        {dirty && <span className="dirty">未保存</span>}
        {canEdit && (
          <>
            {/*
            取り消せない操作と保存は相互に排他する。**押せない理由を title に
            隠さない**（ADR 0039）。ホバーでしか読めず、disabled なボタンは
            フォーカスも当たらないので、キーボードと読み上げには届かない。
          */}
            <button
              type="button"
              className="primary"
              onClick={() => void save()}
              disabled={!canSave}
              aria-describedby={
                saveBlocked !== null ? "save-shortcut save-blocked" : "save-shortcut"
              }
            >
              {saving ? "保存中…" : "保存"}
            </button>
            {/*
            ショートカットの存在を画面に出す。**`title` に隠さない**
            （ADR 0039）。**ボタンの中には置かない。** 中に置くと読み上げる名前が
            「保存 Ctrl / ⌘ + S」になり、名前で引いている E2E が全部ずれる。
            外に出して `aria-describedby` で結ぶ。

            修飾キーは両方書く。どちらが効くかは OS で決まるが、etoki は
            それを見ていないので、片方だけ出すともう片方の利用者には嘘になる。
          */}
            <kbd className="hint shortcut" id="save-shortcut">
              ⌘/Ctrl+S
            </kbd>
            {saveBlocked !== null && (
              <span className="hint" id="save-blocked">
                {saveBlocked}
              </span>
            )}
          </>
        )}
      </div>
    ),
    [
      board.name,
      nameDraft,
      setNameDraft,
      renaming,
      rename,
      dirty,
      canEdit,
      save,
      canSave,
      saveBlocked,
      saving,
    ],
  );

  /*
   * 下に出す。**常に見えている状態だけ**（ADR 0064 / 0065）。押すものは置かない。
   */
  const footerUI = useMemo(
    () => (
      <div className="board-context etoki-ui">
        {/*
        自分が何をできるのかは、操作して断られる前に見えている必要がある。
        共有すると「開けるが書けない」が普通に起きる（ADR 0017）。
      */}
        <span className="badge badge-role">{ROLE_LABELS[board.role]}</span>
        {/*
        どこに作られるのかは、作る直前ではなく常に見えている必要がある。
        作った draft issue は取り消せない（ADR 0009）。

        飛び先が組めるならリンクにする。取り消せない操作の結果を確かめる
        導線がここから始まる（ADR 0025）。組めないのは作成先が未選択の
        ボードだけなので、そのときはこれまでどおり文字のまま出す。

        **Project まで書く**（#217）。1 つのリポジトリに Project は複数ありうる
        ので、リポジトリ名だけでは作る先が決まらない。組み立ては詳細の帯の
        「作る先」（`targetLabel`）と同じものを使う。
      */}
        {linkHref !== null ? (
          <a
            className="badge badge-target"
            href={linkHref}
            target="_blank"
            rel="noreferrer"
            title={
              linkExact
                ? "作成先の Project を GitHub で開く"
                : "リポジトリの Projects を GitHub で開く"
            }
          >
            {targetLabel ?? `${board.repositoryOwner}/${board.repositoryName}`}
          </a>
        ) : (
          <span className="badge badge-target">
            {targetLabel ?? `${board.repositoryOwner}/${board.repositoryName}`}
          </span>
        )}
        {/*
          作成先の Project に書けるか（ADR 0017）。**書けないと分かったときだけ
          出す**（#217）。`unknown`（まだ確かめていない・確かめられなかった）を
          「書けません」に見せない。なぜ書けないかの本文は詳細の帯にある。
        */}
        {projectAccess === "denied" && (
          <span className="badge badge-denied">書けません</span>
        )}
        {/*
        作成先が固定済みかは**状態**なので、ここにも出す（#62 が読めなければ
        ならないものとして挙げている）。メニューの中の文は「作成先を変更」が
        無い理由で、閉じているあいだは読めない。役目が違うので両方に置く。
      */}
        {board.targetLocked && <span className="badge badge-locked">作成先は確定</span>}
        {/*
        いまの大きさを出す。**上限との比は出さない。** 比を出すには上限を
        フロントが知る必要があり、それは判定を 2 箇所に持つのと同じこと
        になる（ADR 0018 / 0038）。「大きいときだけ」出さないのも同じ
        理由で、上限を知らない以上どこからが大きいのかを決められない。
      */}
        {sceneBytes !== null && (
          <span className="badge badge-size" title="保存に送るシーンの大きさ">
            {formatSceneSize(sceneBytes)}
          </span>
        )}
      </div>
    ),
    [
      board.role,
      board.repositoryOwner,
      board.repositoryName,
      board.targetLocked,
      targetLabel,
      projectAccess,
      linkHref,
      linkExact,
      sceneBytes,
    ],
  );

  /*
   * たまに押す操作はキャンバスのメニューにしまう（ADR 0065）。
   *
   * **押せない理由は、しまった先で本文として出す**（ADR 0066）。メニューは
   * 開けばフォーカスが入るので、キーボードと読み上げには届く。形はヘッダーに
   * あったころと同じで、権限や設定で押せない操作はボタンごと出さずに理由の文を
   * 出し、一時的に押せない操作は押せないボタンと理由を `aria-describedby` で結ぶ。
   *
   * **独自のメニューを渡すと既定の中身は丸ごと置き換わる。** 残すものは
   * `MainMenu.DefaultItems` で並べ直してある。**etoki の項目には
   * `etoki-menu-item` を付ける。** axe はライブラリの DOM を外して掛けており、
   * この印で etoki の項目だけを検査に戻している（`web/e2e/helpers/a11y.ts`）。
   * テーマの切り替えはここに残す
   * （ADR 0055 / 0065 の「口は 1 つ」）。**etoki の項目にもアイコンを付ける**
   * （`menuIcons.tsx`、#204）。既定の項目にだけあると、字下げが揃わず 2 種類の
   * 部品が混ざって見える。Excalidraw 自身へのリンク
   * （`Socials`）は etoki の利用者に向けたものではないので置かない。
   */
  const boardMenu = useMemo(
    () => (
      <MainMenu>
        <MainMenu.Item
          className="etoki-menu-item"
          icon={backToListIcon}
          onSelect={onClose}
        >
          ボード一覧へ戻る
        </MainMenu.Item>
        <MainMenu.Separator />
        {/*
        名前はブレストの中身に属する表示物なので、editor にも直させる
        （作成先の変更は owner だけ、ADR 0017）。押せる人にだけ出す。
      */}
        {canEdit && (
          <MainMenu.Item
            className="etoki-menu-item"
            icon={renameIcon}
            onSelect={() => setNameDraft(board.name)}
          >
            名前を変更
          </MainMenu.Item>
        )}
        <MainMenu.Separator />
        {!isOwner(board.role) ? (
          // 作成先を変えられるのは owner だけ（ADR 0017）。押せるのに 403 で
          // 断るより、押せないことを見せるほうが状態として正しい。
          <MenuNote>作成先を変えられるのはオーナーだけです</MenuNote>
        ) : board.targetLocked ? (
          // 固定済みなら変更手段を出さない。押せるのに 409 で断るより、
          // 押せないことを見せるほうが状態として正しい。
          //
          // **名前の取り直しだけは出す。** 固定するのは作成先そのもので
          // あって、表示用のスナップショットではない（ADR 0037）。
          <>
            <MenuNote>作成先は確定（draft issue を作成済み）</MenuNote>
            {/*
            GitHub が組み立てられていない構成では、押しても Project の
            一覧を引けない。ボタンを黙って消さず、代わりに理由を出す
            （ADR 0030）。
          */}
            {creationUnavailable !== null ? (
              <MenuNote>{creationUnavailable}</MenuNote>
            ) : (
              <MainMenu.Item
                className="etoki-menu-item"
                icon={refreshTargetIcon}
                onSelect={() => void refreshTargetDisplay()}
                disabled={refreshingTarget}
              >
                {refreshingTarget ? "取り直し中…" : "作成先の名前を取り直す"}
              </MainMenu.Item>
            )}
          </>
        ) : (
          <>
            <MainMenu.Item
              className="etoki-menu-item"
              icon={changeTargetIcon}
              onSelect={onChangeTarget}
              // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる。
              // 黙って捨てずに、保存してからにしてもらう。
              disabled={targetChangeBlocked !== null}
              aria-describedby={
                targetChangeBlocked !== null ? "target-change-blocked" : undefined
              }
            >
              作成先を変更
            </MainMenu.Item>
            {targetChangeBlocked !== null && (
              <MenuNote id="target-change-blocked">{targetChangeBlocked}</MenuNote>
            )}
          </>
        )}
        <MainMenu.Separator />
        {/*
        持ち出しと取り込みの口はここ 1 つ（ADR 0045）。ライブラリの既定の
        項目（開く・保存）はこのメニューに並べていない。

        書き出しは viewer にも出す。見えているものを出すだけなので、
        共有した相手に新しく見せるものが無い（ADR 0017）。
      */}
        <MainMenu.Item
          className="etoki-menu-item"
          icon={exportIcon}
          onSelect={exportScene}
          disabled={!api}
        >
          書き出し
        </MainMenu.Item>
        {canEdit && (
          <>
            <MainMenu.Item
              className="etoki-menu-item"
              icon={importIcon}
              onSelect={() => fileInput.current?.click()}
              // **作成中は取り込ませない。** キャンバスを置き換えるので、
              // 保存を止めているのと同じ理由で止める（作られた内容と記録
              // されるハッシュが食い違いうる）。
              disabled={!api || running !== null}
              aria-describedby={importBlocked !== null ? "import-blocked" : undefined}
            >
              {importing ? "取り込み中…" : "取り込み"}
            </MainMenu.Item>
            {importBlocked !== null && (
              <MenuNote id="import-blocked">{importBlocked}</MenuNote>
            )}
            <MainMenu.Item
              className="etoki-menu-item"
              icon={tableIcon}
              onSelect={addTable}
              disabled={!api}
            >
              表
            </MainMenu.Item>
          </>
        )}
        <MainMenu.Separator />
        <MainMenu.DefaultItems.SaveAsImage />
        <MainMenu.DefaultItems.SearchMenu />
        <MainMenu.DefaultItems.Help />
        {canEdit && <MainMenu.DefaultItems.ClearCanvas />}
        <MainMenu.Separator />
        <MainMenu.DefaultItems.ToggleTheme />
        {canEdit && <MainMenu.DefaultItems.ChangeCanvasBackground />}
        <MainMenu.Separator />
        {/*
        ボードごと畳むのは owner だけ（ADR 0042）。**押した時点では消さない。**
        何が残るのかを引いてから確認を出す。

        **消すなら理由を出す**（ADR 0017 / 0030）。権限で押せない操作は、
        ボタンを黙って消さずに押せない理由のほうを見せる。

        **最後に区切って置く。** 取り消せない操作を、日常の操作と同じ並びの
        途中に置かない。
      */}
        {isOwner(board.role) ? (
          <MainMenu.Item
            className="etoki-menu-item danger"
            icon={deleteBoardIcon}
            onSelect={() => void askDelete()}
            disabled={deletion !== null}
          >
            {deletion?.status === "loading" ? "確認中…" : "ボードを削除"}
          </MainMenu.Item>
        ) : (
          <MenuNote>ボードを削除できるのはオーナーだけです</MenuNote>
        )}
      </MainMenu>
    ),
    [
      onClose,
      canEdit,
      board.name,
      board.role,
      setNameDraft,
      board.targetLocked,
      api,
      creationUnavailable,
      refreshTargetDisplay,
      refreshingTarget,
      onChangeTarget,
      targetChangeBlocked,
      exportScene,
      fileInput,
      running,
      importBlocked,
      importing,
      addTable,
      askDelete,
      deletion,
    ],
  );

  /*
   * **キャンバスに渡すものは、中身が変わったときだけ作り直す。**
   *
   * Excalidraw は props が変わると描き直し、そのたびに `onChange` を呼ぶ。
   * `handleChange` は毎回新しい配列で state を置くので、ここを描くたびに新しく
   * 作ると「描き直す → onChange → state が変わる → 描き直す」の輪が閉じず、
   * 更新の上限で落ちる。子要素（メニュー・下の帯）とこの関数を安定させておけば、
   * state が変わってもキャンバスは描き直さない。
   */
  const renderTopRightUI = useCallback(() => topRightUI, [topRightUI]);
  // **子要素は配列ごと 1 つにまとめて持つ。** `{boardMenu}<Footer>…</Footer>` を
  // その場で並べると、中身が同じでも `children` は描くたびに新しい配列になり、
  // Excalidraw の `memo` が効かない。
  const canvasChildren = useMemo(
    () => (
      <>
        {boardMenu}
        <Footer>{footerUI}</Footer>
      </>
    ),
    [boardMenu, footerUI],
  );

  return (
    <div className="board">
      {canEdit && (
        // **入力そのものは出さない。** 押す口はメニューの「取り込み」1 つで、
        // ここはファイルを選ばせるためだけに置いてある。**メニューの中に
        // 置かない。** メニューは押すと閉じて中身ごと外れるので、選んだ
        // ファイルを受け取る前に入力が消える。
        <input
          ref={fileInput}
          type="file"
          aria-label="取り込む .excalidraw ファイル"
          accept=".excalidraw,application/json"
          hidden
          onChange={(e) => {
            const file = e.target.files?.[0];
            // 選び直しで同じファイルをもう一度選べるようにする。値を
            // 残すと、2 回目の選択で change が発火しない。
            e.target.value = "";
            if (file) void importScene(file);
          }}
        />
      )}

      {/*
        **何が残るのかを見せてから確認させる**（ADR 0042、中核思想 3）。
        etoki は GitHub 側の draft issue を消さない（消せない）ので、消えるのは
        出どころの記録のほうだと分けて言う。ブラウザの confirm を使わないのは、
        件数を出す場所が無いため。
      */}
      {deletion !== null && deletion.status !== "loading" && (
        <section
          className="delete-confirm"
          role="alertdialog"
          aria-labelledby="delete-confirm-title"
          // 見出しではなく枠を受け皿にする。読み上げは aria-labelledby で
          // 見出しを読み、次のタブ移動が中のボタンに入る。
          tabIndex={-1}
          ref={focusDeleteConfirm}
        >
          <h2 id="delete-confirm-title">「{board.name}」を削除しますか</h2>
          <p>
            {"シーンもメンバーも実行の記録も消えます。"}
            <strong>取り消せません。</strong>
          </p>
          {deletion.losing.recordedItemCount > 0 ? (
            <p>
              {`このボードから作成した draft issue が ${deletion.losing.recordedItemCount} 件記録されています。`}
              {"GitHub 側の draft issue は削除されません（etoki からは消せません）。"}
              {"削除すると、その draft issue がどこから作られたのかを辿れなくなります。"}
            </p>
          ) : (
            <p>このボードから作成した draft issue の記録はありません。</p>
          )}
          <div className="delete-confirm-actions">
            <button
              type="button"
              className="danger"
              onClick={() => void deleteBoard()}
              disabled={deletion.status === "deleting"}
            >
              {deletion.status === "deleting" ? "削除中…" : "削除する"}
            </button>
            <button
              type="button"
              className="quiet"
              onClick={cancelDelete}
              disabled={deletion.status === "deleting"}
            >
              やめる
            </button>
          </div>
        </section>
      )}

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

      {/*
        保存済みシーンが保存できる上限を超えていて、このままでは保存し直せない
        状態（issue #103、ADR 0038）。**上限の数値は出さない。** サーバーの
        判定結果を見せるだけで、フロントは上限を複製しない。開いた時点で
        分かるよう、キャンバスを描く前から出す（中核思想 3）。
      */}
      {overLimit && (
        <p className="scene-limit-warning" role="alert">
          {
            "このボードは保存できる上限を超えています。保存し直すには貼った画像を減らしてください。"
          }
          {sceneBytes !== null && `（いまの大きさ: ${formatSceneSize(sceneBytes)}）`}
        </p>
      )}

      <div className="board-body">
        <div className="canvas">
          <Excalidraw
            excalidrawAPI={setApi}
            initialData={initialData as never}
            onChange={handleChange as never}
            langCode="ja-JP"
            theme={theme}
            // 持ち出しと取り込みの口は etoki のメニューに寄せてある（ADR 0045）。
            UIOptions={UI_OPTIONS}
            // Excalidraw の AI の口を閉じる（ADR 0067）。コマンドパレットの
            // 「Mermaid to Excalidraw」はこれで出し分けられている。0.18.1 は
            // パレットを置かないので今は効いていないが、置かれたときに守りを
            // 通らない口が黙って開かないようにする。「その他」メニューの同じ
            // 項目はこれでは消えないので、`board.css` で隠している。
            aiEnabled={false}
            onPaste={handlePaste}
            // viewer には描かせない。描けるのに保存できないと、描いた内容を
            // 黙って捨てることになる（ADR 0017）。
            viewModeEnabled={!canEdit}
            renderTopRightUI={renderTopRightUI}
          >
            {canvasChildren}
          </Excalidraw>
          {/*
            注釈の frame を見分けられるようにする。Excalidraw の外に重ねる
            だけで、要素には触らない（AnnotationOverlay の doc）。

            **枠も境界で包む。** 包まずに落ちると外側の 1 枚が受けることになり、
            キャンバスごと外れて未保存のブレストが消える（ADR 0027）。
          */}
          <ErrorBoundary name="注釈の枠" recovery="remount">
            <AnnotationOverlay boxes={overlayBoxes} />
          </ErrorBoundary>
          {/*
            解釈の結果と下書きの手直しは、キャンバスの上に広く開く。
            **`.excalidraw` の外に置く。** 中に入れると色の変数がぶつかり、
            axe が色を判定できない（ADR 0065）。
          */}
          <ErrorBoundary name="解釈の結果" recovery="remount">
            <AnnotationDetail
              openId={detailId}
              openRequest={detailRequest}
              onClose={() => setDetailId(null)}
              annotations={annotations}
              frames={{
                canvasIds: canvasFrameIds,
                metas: canvasMetas,
                onFocus: focusFrame,
                onChangeGranularity: handleMark,
                onChangeKind: handleChangeAnnotationKind,
              }}
              interpretation={interpretation}
              creation={creation}
              runs={{ states: runHistories, onLoad: (id) => void loadRuns(id) }}
              stale={dirty}
              canEdit={canEdit}
              projectLink={link}
              targetLabel={targetLabel}
            />
          </ErrorBoundary>
        </div>

        {/*
          注釈・図のドラフト・メンバーは右の 1 か所にタブで並べる（ADR 0065）。
          **キャンバスを覆わない。** 図のドラフトは置いた図がどこに出るかを
          見ながら直す道具なので、上に敷くと「置く」を押した結果が確かめられない。

          **パネルは 1 枚ずつ境界で包む**（ADR 0027）。落ちたのが 1 枚でも外側で
          受けると、キャンバスごと外れて未保存のブレストが消える。
        */}
        <SidePanel
          active={panelTab}
          onSelect={setPanelTab}
          collapsed={panelCollapsed}
          onCollapsedChange={changePanelCollapsed}
          tabs={[
            {
              id: "annotations",
              label: "注釈",
              // 畳んでいるあいだに知りたいのは、手を打つ必要があるものだけ。
              // 数えるのは注釈の一覧と同じく保存済みシーンが基準。
              railBadges: railBadgesOf(annotations),
              content: (
                <ErrorBoundary name="注釈パネル" recovery="remount">
                  <AnnotationPanel
                    annotations={annotations}
                    detached={detached}
                    frames={{
                      markable,
                      unmarkable,
                      canvasIds: canvasFrameIds,
                      selectedIds: selectedFrames.map((f) => f.id),
                      onMark: handleMark,
                      onUnmark: handleUnmark,
                    }}
                    runs={{ states: runHistories, onLoad: (id) => void loadRuns(id) }}
                    stale={dirty}
                    canEdit={canEdit}
                    projectLink={link}
                    onOpenDetail={openDetail}
                  />
                </ErrorBoundary>
              ),
            },
            // 図のドラフト。**viewer には出さない**（ADR 0017）。生成は LLM を
            // 叩く外部呼び出しで課金も伴うので、解釈と同じ扱いにする。LLM が
            // 未設定でもタブは出す。**黙って消さず、開いた先で理由を見せる**
            // （ADR 0030、中核思想 3）。mermaid を貼る口（ADR 0062）も同じタブに
            // 置く。LLM を通さないので未設定でも使えるが、描かせないのと同じ
            // 理由で viewer には出さない。
            ...(canEdit
              ? [
                  {
                    id: "diagram" as const,
                    label: "図のドラフト",
                    content: (
                      <DiagramTab
                        mode={diagramMode}
                        onModeChange={setDiagramMode}
                        generate={
                          <ErrorBoundary name="図のドラフト" recovery="remount">
                            <DiagramChatPanel
                              chat={chat}
                              onChangeKind={handleChangeKind}
                              onSend={generateDiagram}
                              onPlace={() => void placeDraft()}
                              unavailable={diagramUnavailable}
                            />
                          </ErrorBoundary>
                        }
                        paste={
                          <ErrorBoundary name="mermaid の貼り付け" recovery="remount">
                            <MermaidPastePanel
                              text={pasteText}
                              onChangeText={setPasteText}
                              onPlace={pasteMermaid}
                            />
                          </ErrorBoundary>
                        }
                      />
                    ),
                  },
                ]
              : []),
            {
              id: "members",
              label: "メンバー",
              // 共有が組み立てられていない構成では、押しても 503 しか返らない。
              // タブは黙って消さず、開いた先で理由を出す（中核思想 3）。
              content:
                sharingUnavailable !== null ? (
                  <p className="hint side-panel-note">{sharingUnavailable}</p>
                ) : (
                  <ErrorBoundary name="メンバーパネル" recovery="remount">
                    <MemberPanel boardId={board.id} role={board.role} />
                  </ErrorBoundary>
                ),
            },
          ]}
        />
      </div>
    </div>
  );
}

/**
 * メニューの中に出す、押せない理由の文（ADR 0066）。
 *
 * **ボタンとして描かない。** 押しても何も起きないものを項目の形で置くと、
 * 押せるように見える。`ItemCustom` は項目と同じ並びに置けて、押せない。
 */
function MenuNote({ id, children }: { id?: string; children: ReactNode }) {
  return (
    <MainMenu.ItemCustom className="menu-note">
      <span className="hint" id={id}>
        {children}
      </span>
    </MainMenu.ItemCustom>
  );
}
