import { Footer } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { BoardDetail, BoardWithFiles, Capabilities } from "../api/types";
import { unavailableReason } from "../app/capability";
import { isAnnotation } from "../excalidraw/annotation";
import { ErrorBoundary } from "../app/ErrorBoundary";
import type { Theme } from "../app/theme";
import {
  AnnotationDetail,
  type CreationProps,
  type InterpretationProps,
} from "./annotations/AnnotationDetail";
import { useAnnotationDetail } from "./annotations/useAnnotationDetail";
import {
  ANNOTATIONS_FAILED,
  useAnnotationStates,
} from "./annotations/useAnnotationStates";
import { useExclusion } from "./exclusion";
import { useInterpretations } from "./annotations/useInterpretations";
import { ShareDialog } from "./members/ShareDialog";
import { type DiagramMode } from "./diagram/DiagramTab";
import { useDiagramDraft } from "./diagram/useDiagramDraft";
import { projectLabel } from "../boards/grouping";
import { SaveConflictAlert, SceneLimitWarning } from "./BoardAlerts";
import { BoardBar } from "./BoardBar";
import { BoardCanvas } from "./BoardCanvas";
import { BoardContext } from "./BoardContext";
import { BoardMenu } from "./BoardMenu";
import { BoardStatus } from "./BoardStatus";
import { DeleteConfirm } from "./DeleteConfirm";
import { AnnotationsTabContent, DiagramTabContent } from "./PanelTabContents";
import { PremiseDialog } from "./premise/PremiseDialog";
import { usePremise } from "./premise/usePremise";
import { SidePanel, type TabSpec } from "./SidePanel";
import { useCreation } from "./annotations/useCreation";
import { useRunHistories } from "./annotations/useRunHistories";
import { railBadgesOf } from "./panelState";
import { projectLink } from "./target/projectLink";
import { canEditBoard } from "./members/roles";
import { useBoardDeletion, useRename, useTargetRefresh } from "./useBoardManagement";
import { useBoardNotify } from "./useBoardNotify";
import { useBoardTransfer } from "./useBoardTransfer";
import { useCanvasCommands } from "./useCanvasCommands";
import {
  measureVisibleCanvas,
  useArrowKeyScroll,
  useDragEdgeScroll,
} from "./useCanvasScroll";
import { useConfirmLeave, useDirtyScene } from "./useDirtyScene";
import { useCanvasAnnotations } from "./useCanvasAnnotations";
import { useEkidoki } from "./useEkidoki";
import { SCENE_UNREADABLE, useInitialScene } from "./useInitialScene";
import { useMenuTriggerName } from "./useMenuTriggerName";
import { usePanelLayout } from "./usePanelLayout";
import { useProjectAccess } from "./useProjectAccess";
import { useSaveShortcut } from "./useSaveShortcut";
import { SAVE_FAILED, useSceneSave } from "./useSceneSave";
import { useSceneSize } from "./useSceneSize";

type Props = {
  /**
   * 開いたボード。**貼った画像を持つのは開いたときだけ**（ADR 0074）。キャンバスに
   * 渡すのはマウントの 1 回きりで、以後はキャンバスが画像を持っている。
   */
  board: BoardWithFiles;
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
   * 版（`updatedAt`）は動かない（ADR 0092）。保存の基準は据え置く。
   */
  onTargetRefreshed: (board: BoardDetail) => void;
  /**
   * 名前を変えたので、手元のボードを差し替えてもらう。
   *
   * 版（`updatedAt`）は動かないので、保存の基準は据え置く（ADR 0020 / 0092）。
   */
  onRenamed: (board: BoardDetail) => void;
  /**
   * ボードの背景を書き換えた、または先に書き換えられていたので取り直した
   * （ADR 0093）。手元のボードを差し替えてもらう。
   *
   * 版（`updatedAt`）は動かないので、保存の基準は据え置く（ADR 0092）。
   */
  onPremiseChanged: (board: BoardDetail) => void;
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
  onPremiseChanged,
  onDeleted,
  onDirtyChange,
  theme,
  onThemeChange,
}: Props) {
  const canEdit = canEditBoard(board.role);

  // 画面全体に出す失敗は通知へ（`useBoardNotify`、ADR 0058）。**束のまま
  // 持たない。** 中の関数を依存に置くフックが多い。
  const { onError, dismissHere, dismissKey } = useBoardNotify();

  const [api, setApi] = useState<ExcalidrawImperativeAPI | null>(null);
  // 保存済みの注釈の状態（`useAnnotationStates`）。開いたら 1 度引き、保存と作成の
  // あとに取り直す。`detached` は `annotations` と混ぜない（#111）。
  const { annotations, detached, refreshAnnotations } = useAnnotationStates({
    boardId: board.id,
    onError,
    dismiss: dismissHere,
  });
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
    cancel: cancelInterpretation,
    recordCreated,
    discardAll: discardInterpretations,
  } = useInterpretations({
    api,
    boardId: board.id,
    annotations,
    // 未設定のうちは引かない。capabilities が届くまで（null）も待つ。
    enabled: capabilities?.interpretation === true,
  });
  // 右のパネルの置き方と開閉（`usePanelLayout`）。**束のまま持たない。** 中の関数を
  // 依存に置く `useCallback` が、置き方が変わるたびに作り直される。
  const {
    layout,
    measureLayout,
    phone,
    panelTab,
    setPanelTab,
    panelCollapsed,
    changePanelCollapsed,
    phonePanelOpen,
    setPhonePanelOpen,
    panelOpenRequest,
    openPhonePanel,
    closePhonePanel,
    showTab,
  } = usePanelLayout();
  // 共有のダイアログ（`ShareDialog`、#248）。開くのはキャンバスのメニュー。
  // **関数を安定させる。** メニューは `useMemo` で持つ（ADR 0065）。
  const [shareOpen, setShareOpen] = useState(false);
  const openShare = useCallback(() => setShareOpen(true), []);
  const closeShare = useCallback(() => setShareOpen(false), []);
  // ボードの背景（`usePremise`、ADR 0093）。**書きかけはダイアログの外のここで
  // 持つ。** 閉じても消えない。開くのはキャンバスのメニュー。
  const premise = usePremise({ board, onChanged: onPremiseChanged });
  const openPremise = premise.openEditor;
  // 図のドラフトのタブで、LLM に作らせるか mermaid を貼るか（`DiagramTab`）。
  const [diagramMode, setDiagramMode] = useState<DiagramMode>("generate");
  // 注釈の詳細の開閉（`useAnnotationDetail`）。保存で消えた注釈の詳細は閉じる。
  const { detailId, detailRequest, openDetail, closeDetail } =
    useAnnotationDetail(annotations);
  // 作成先の Project に書けるかどうか（`useProjectAccess`）。表示用で、判定には
  // 使わない（ADR 0017）。
  const projectAccess = useProjectAccess(board.id);
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

  // キャンバスに渡す最初のシーンと、開いた時点でサーバーが持っていた画像
  // （`useInitialScene`）。読めなかったら空で開いて通知する。
  const { initialData, openedFileIds } = useInitialScene(board, onError);

  // キャンバスの写しと、要素を書き換える口（`useCanvasAnnotations`）。Excalidraw の
  // `onChange`（`handleChange`）もここにある。**束のまま持たない。** `handleChange` が
  // 作り直されると、Excalidraw の `onChange` が差し替わり続けて画面ごと落ちる。
  const {
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
  } = useCanvasAnnotations({
    api,
    theme,
    onThemeChange,
    applySignature,
    scheduleMeasure,
  });

  // 図のドラフト（LLM に作らせる・mermaid を貼る・置く）。`useDiagramDraft`。
  const {
    chat,
    prompt: diagramPrompt,
    setPrompt: setDiagramPrompt,
    generate: generateDiagram,
    changeKind: handleChangeKind,
    placeDraft,
    pasteText,
    setPasteText,
    pasteMermaid,
  } = useDiagramDraft({
    api,
    boardId: board.id,
    currentElements,
    updateElements,
    // 置いた図を見せる。スマホでは全面のパネルがキャンバスを隠している（#199）。
    onPlaced: closePhonePanel,
    // 未設定のうちは引かない。capabilities が届くまで（null）も待つ。viewer には
    // 生成の口を出さない（下の `canEdit`）ので、引いても見せる先が無い。
    enabled: capabilities?.diagramDraft === true && canEdit,
  });

  // 表を置く・フレームへ寄せる（`useCanvasCommands`）。
  const { addTable, focusFrame } = useCanvasCommands({
    api,
    currentElements,
    updateElements,
    closePhonePanel,
  });

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
    fileIds: openedFileIds,
    sceneOverLimit: board.sceneOverLimit,
    exclusive,
    currentBackground,
    markSaved,
    onSaved: discardAfterSave,
    onError,
    dismiss: dismissHere,
  });

  // 「絵解き」の流れ（`useEkidoki`）。保存を挟んでから開く・読む（ADR 0077）。
  const showAnnotationList = useCallback(() => showTab("annotations"), [showTab]);
  const { startEkidoki, interpretAfterSave } = useEkidoki({
    isDirty,
    save,
    refreshAnnotations,
    canvasMetas,
    interpret,
    openDetail,
    showAnnotationList,
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
   * 保存を押せるか。**ボタン（「保存」と「絵解き」）の `disabled` とショートカットで
   * 同じ式を使う。**
   *
   * 2 つに分けて書くと、片方だけに条件を足したときに、押せないはずの経路が
   * キーボードからだけ通る。
   *
   * 走っている操作があれば押させない。表の `saving` 行はどの操作に対しても
   * 止める（対角も `useExclusion` が弾く）ので、`running` を見れば足りる
   * （`exclusion.ts`）。
   */
  const canSave = canEdit && api !== null && exclusive.running === null;

  // Ctrl / Cmd + S で保存する（`useSaveShortcut`、issue #145）。
  useSaveShortcut(canSave, save);

  /*
    キャンバスの移動を楽にする（#250）。何も選んでいないときの矢印キーと、
    ドラッグ中に端へ寄せたときのスクロール。**測る枠は重ねたパネルを除いた
    もの**（`measureVisibleCanvas`）。パネルの下に隠れたところを端にすると、
    パネルに重なるまで運ばないとスクロールしない。
  */
  const canvasFrame = useRef<HTMLDivElement>(null);
  const visibleCanvas = useCallback(
    () => (canvasFrame.current ? measureVisibleCanvas(canvasFrame.current) : null),
    [],
  );
  useArrowKeyScroll(api, visibleCanvas);
  useDragEdgeScroll(api, visibleCanvas);
  // etoki の操作を収めたメニューの入口に、読み上げの名前を付ける（#299）。
  useMenuTriggerName(canvasFrame, api);

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

  // 解釈の口。詳細の帯に渡す（#201）。
  const interpretation: InterpretationProps = {
    states: interpretations,
    // 押したら詳細を開く。結果はそこに出るので、開かないと押したあとに何が
    // 起きたかが見えない。
    onInterpret: (id) => {
      openDetail(id);
      void interpretAfterSave(id);
    },
    onSelect: showInterpretation,
    // 取り消せなかったことは `cancel` 自身が帯に出す（`useInterpretations` の
    // `cancelFailed`）ので、待たずに手放してよい。
    onCancel: (id) => void cancelInterpretation(id),
    unavailable: interpretationUnavailable,
    premise: board.premise,
  };
  // 作る先の見出し。押す直前の帯の文（`creationNotice`）に出す。作成先が未選択の
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

  // 右上に出す。ボード名・未保存・「保存」・「絵解き」（`BoardStatus`）。**描くたびに
  // 作り直さない**（ADR 0065）。依存にはプリミティブと、同一性の保たれた関数だけを置く。
  const topRightUI = useMemo(
    () => (
      <BoardStatus
        name={board.name}
        nameDraft={nameDraft}
        onNameDraftChange={setNameDraft}
        renaming={renaming}
        onRename={() => void rename()}
        dirty={dirty}
        canEdit={canEdit}
        onSave={() => void save()}
        onEkidoki={() => void startEkidoki()}
        canSave={canSave}
        saveBlocked={saveBlocked}
        saving={saving}
      />
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
      startEkidoki,
      canSave,
      saveBlocked,
      saving,
    ],
  );

  // 下に出す。ロール・作成先・大きさ（`BoardContext`）。作り直さない理由は上と同じ。
  const footerUI = useMemo(
    () => (
      <BoardContext
        role={board.role}
        targetLabel={targetLabel}
        repository={`${board.repositoryOwner}/${board.repositoryName}`}
        linkHref={linkHref}
        linkExact={linkExact}
        projectAccess={projectAccess}
        targetLocked={board.targetLocked}
        sceneBytes={sceneBytes}
      />
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

  // たまに押す操作はキャンバスのメニューにしまう（`BoardMenu`）。作り直さない
  // 理由は上と同じ。
  const boardMenu = useMemo(
    () => (
      <BoardMenu
        onClose={onClose}
        canEdit={canEdit}
        role={board.role}
        targetLocked={board.targetLocked}
        onRename={() => setNameDraft(board.name)}
        onPremise={openPremise}
        onShare={openShare}
        creationUnavailable={creationUnavailable}
        onRefreshTarget={() => void refreshTargetDisplay()}
        refreshingTarget={refreshingTarget}
        onChangeTarget={onChangeTarget}
        targetChangeBlocked={targetChangeBlocked}
        canvasReady={api !== null}
        onExport={exportScene}
        onImport={() => fileInput.current?.click()}
        running={running}
        importBlocked={importBlocked}
        importing={importing}
        onAddTable={addTable}
        deletion={deletion}
        onAskDelete={() => void askDelete()}
      />
    ),
    [
      onClose,
      canEdit,
      board.name,
      board.role,
      setNameDraft,
      openPremise,
      openShare,
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
   *
   * **スマホの置き方では何も返さない**（#199）。右上の島は Excalidraw の
   * ツールバーの行ではなく、キャンバスの上の etoki の帯に置く。**判定は etoki の
   * 置き方で行い、引数の `isMobile` は見ない。** 島をどこに描くかを 1 つの判定で
   * 決めておけば、2 つの判定がずれた幅でも島は必ずどちらか 1 か所に出る。
   * Excalidraw の判定で返さないと、ずれた幅で島がどこにも無くなる。
   */
  const renderTopRightUI = useCallback(
    () => (phone ? null : topRightUI),
    [phone, topRightUI],
  );
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

  /*
   * 「絵解いた」（注釈）と「etoki AI」（図のドラフト）は右の 1 か所にタブで並べる
   * （ADR 0065）。共有はボードそのものの管理なので、ここではなくメニューから
   * ダイアログで開く（#248、ADR 0078）。スマホの上の帯（`BoardBar`）も、同じ
   * タブから開くボタンを作る。中身と境界の包み方は `PanelTabContents`。
   */
  const panelTabs: TabSpec[] = [
    {
      id: "annotations",
      // 囲んで絵解きしたもの（#248）。中で使う語（注釈）は据え置く。
      label: "絵解いた",
      // 畳んでいるあいだに知りたいのは、手を打つ必要があるものだけ。
      // 数えるのは注釈の一覧と同じく保存済みシーンが基準。
      railBadges: railBadgesOf(annotations),
      content: (
        <AnnotationsTabContent
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
            label: "etoki AI",
            content: (
              <DiagramTabContent
                mode={diagramMode}
                onModeChange={setDiagramMode}
                chat={{
                  chat,
                  prompt: diagramPrompt,
                  onPromptChange: setDiagramPrompt,
                  onChangeKind: handleChangeKind,
                  onSend: generateDiagram,
                  onPlace: () => void placeDraft(),
                  unavailable: diagramUnavailable,
                }}
                paste={{
                  text: pasteText,
                  onChangeText: setPasteText,
                  onPlace: pasteMermaid,
                }}
              />
            ),
          },
        ]
      : []),
  ];

  return (
    <div className="board" data-layout={layout}>
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

      {deletion !== null && deletion.status !== "loading" && (
        <DeleteConfirm
          name={board.name}
          deletion={deletion}
          onConfirm={() => void deleteBoard()}
          onCancel={cancelDelete}
        />
      )}

      {/* 保存の衝突（ADR 0020）と、上限を超えたシーン（ADR 0038）の帯。 */}
      {conflicted && <SaveConflictAlert />}
      {overLimit && <SceneLimitWarning sceneBytes={sceneBytes} />}

      {/*
        注意の帯を除いた領域。**置き方はこの枠の大きさで決める**（`useBoardLayout`）。
        スマホの上の帯もこの中に置く。
      */}
      <div className="board-main" ref={measureLayout}>
        {/*
          スマホの置き方では、右上と下の帯の中身をキャンバスの上の etoki の帯に
          移す（#199）。Excalidraw のモバイル用 UI では `Footer` が描かれず、
          状態を常に見せる約束（ADR 0064 / 0065）が崩れるため。1 段目はボード名・
          未保存・「保存」・「絵解き」で、**全面に開いたパネルと詳細もここは覆わない**
          （ADR 0021）。
        */}
        {phone && <div className="board-bar board-bar-status">{topRightUI}</div>}
        <div className="board-stage">
          {phone && (
            <BoardBar
              tabs={panelTabs}
              active={panelTab}
              panelOpen={phonePanelOpen}
              onOpen={openPhonePanel}
              context={footerUI}
            />
          )}
          <div className="board-body">
            <div className="canvas">
              {/*
                Excalidraw と注釈の枠（`BoardCanvas`）。
                **詳細はこの外に置く。** スマホでは詳細が上の帯の 2 段目まで
                覆うので、キャンバスの矩形を基準にできない。
              */}
              <BoardCanvas
                frameRef={canvasFrame}
                onApi={setApi}
                initialData={initialData}
                onChange={handleChange}
                theme={theme}
                canEdit={canEdit}
                onError={onError}
                renderTopRightUI={renderTopRightUI}
                overlayBoxes={overlayBoxes}
              >
                {canvasChildren}
              </BoardCanvas>
              {/*
                解釈の結果と下書きの手直しは、キャンバスの上に広く開く。
                **`.excalidraw` の外に置く。** 中に入れると色の変数がぶつかり、
                axe が色を判定できない（ADR 0065）。
              */}
              <ErrorBoundary name="絵解きの結果" recovery="remount">
                <AnnotationDetail
                  openId={detailId}
                  openRequest={detailRequest}
                  onClose={closeDetail}
                  annotations={annotations}
                  frames={{
                    canvasIds: canvasFrameIds,
                    metas: canvasMetas,
                    names: canvasNames,
                    onFocus: focusFrame,
                    onChangeGranularity: handleMark,
                    onChangeKind: handleChangeAnnotationKind,
                    onChangeName: handleChangeAnnotationName,
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
              右のパネル（`panelTabs`）。**並べられる幅ではキャンバスを覆わない。**
              図のドラフトは置いた図がどこに出るかを見ながら直す道具なので、上に
              敷くと「置く」を押した結果が確かめられない。並べるとキャンバスが
              モバイル用 UI になる幅では重ね、スマホでは全面に開く（#199）。
            */}
            <SidePanel
              active={panelTab}
              onSelect={setPanelTab}
              collapsed={phone ? !phonePanelOpen : panelCollapsed}
              onCollapsedChange={
                phone
                  ? (collapsed) => setPhonePanelOpen(!collapsed)
                  : changePanelCollapsed
              }
              rail={!phone}
              openRequest={panelOpenRequest}
              tabs={panelTabs}
            />
            <ShareDialog
              open={shareOpen}
              onClose={closeShare}
              boardId={board.id}
              role={board.role}
              unavailable={sharingUnavailable}
            />
            <PremiseDialog editor={premise} canEdit={canEdit} premise={board.premise} />
          </div>
        </div>
      </div>
    </div>
  );
}
