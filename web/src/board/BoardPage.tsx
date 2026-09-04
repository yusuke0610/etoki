import { Excalidraw } from "@excalidraw/excalidraw";
import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { ApiError, boardsApi, githubApi } from "../api/boards";
import {
  describeFailure,
  diagramNotPlaceableFailure,
  sceneFileUnreadableFailure,
  sceneUnreadableFailure,
  targetProjectMissingFailure,
  type Failure,
} from "../api/errorMessage";
import type {
  AnnotationStatus,
  BoardDeletion,
  BoardDetail,
  Capabilities,
  DiagramKind,
  Granularity,
  Interpretation,
  ProjectAccess,
} from "../api/types";
import { unavailableReason } from "../capability";
import {
  frameIds,
  isAnnotation,
  markAsAnnotation,
  selectableFrames,
  unmarkAnnotation,
  type SceneElement,
  type SelectableFrame,
} from "../excalidraw/annotation";
import {
  annotationBoxes,
  type AnnotationBox,
  type Viewport,
} from "../excalidraw/annotationOverlay";
import { sceneSignature } from "../excalidraw/dirty";
import { exportAnnotationImage } from "../excalidraw/image";
import { formatSceneSize, sceneBytes } from "../excalidraw/size";
import { draftOrigin, mermaidToElements, moveDraft } from "../excalidraw/mermaid";
import {
  exportFileName,
  readSceneFile,
  sceneJSON,
  type ImportedScene,
} from "../excalidraw/transfer";
import { createStickyNote, stickyNotePosition } from "../excalidraw/sticky";
import { ErrorBoundary } from "../ErrorBoundary";
import { log } from "../logger";
import { AnnotationOverlay } from "./AnnotationOverlay";
import {
  AnnotationPanel,
  type CreationState,
  type RunHistoryState,
} from "./AnnotationPanel";
import { DiagramChatPanel } from "./DiagramChatPanel";
import {
  beginTurn,
  changeKind,
  completeTurn,
  conversionRetryPrompt,
  failTurn,
  historyOf,
  startChat,
  type DiagramChat,
} from "./diagramChat";
import { createGenerations } from "./generation";
import {
  addInterpretation,
  failInterpretation,
  selectInterpretation,
  startInterpretation,
  type InterpretationState,
} from "./interpretationHistory";
import { MemberPanel } from "./MemberPanel";
import { projectLink } from "./projectLink";
import { ROLE_LABELS } from "./roles";

/**
 * シーンの大きさを数え直すまでの待ち時間（ミリ秒）。
 *
 * 描くたびに数えると、画像を貼った大きいボードほど重くなる。手が止まってから
 * 数えれば、表示が少し遅れるだけで済む。
 */
const MEASURE_DELAY_MS = 500;

/**
 * 図のドラフト生成の世代キー。
 *
 * 会話は 1 つしか持たないので 1 本でよい。解釈が注釈ごとに採番するのとは
 * 違って、区別する相手がいない。
 */
const DIAGRAM_KEY = "diagram";

/**
 * ライブラリのメニューから閉じるもの（ADR 0045）。
 *
 * **持ち出しと取り込みの口は etoki のヘッダー 1 つに寄せる。** ライブラリ側を
 * 残すと、同じ画面に意味の違う「保存」が 2 つ並び、片方だけが etoki のボード名と
 * 未保存の確認を知っている形になる。
 *
 * **`.excalidraw` を書き出しているのは `saveAsImage` ではなく `export`。** 表示は
 * 「名前を付けて保存...」で、隣の「画像のエクスポート...」と紛らわしい。
 * `saveToActiveFile` はそこで開いたファイルへの上書きなので、一緒に閉じる。
 * **画像のエクスポートは閉じない。** 答えている問いが違う（持ち出しではなく、
 * 絵を他所に貼ること）。
 *
 * **モジュールの定数として持つ。** 描画のたびに作り直すと、Excalidraw には
 * 毎回違うオブジェクトが渡る。
 */
const UI_OPTIONS = {
  canvasActions: { loadScene: false, export: false, saveToActiveFile: false },
} as const;

/**
 * 削除の確認がいまどこにいるか（ADR 0042）。
 *
 * **`losing` を持たない状態と持つ状態を型で分ける。** 件数が無いまま確認を
 * 出せる形にすると、何を失うのかを見せずに押させる画面が書ける。
 */
type DeletionState =
  | { status: "loading" }
  | { status: "confirming"; losing: BoardDeletion }
  | { status: "deleting"; losing: BoardDeletion };

type Props = {
  board: BoardDetail;
  /**
   * いま使える機能。null は「まだ確かめていない」（ADR 0030）。
   *
   * ボード単位の権限（`projectAccess`）とは別物。プロセスの設定なので、
   * ボードを開くたびに変わりはしない。**混ぜない。**
   */
  capabilities: Capabilities | null;
  onError: (failure: Failure) => void;
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
};

export function BoardPage({
  board,
  capabilities,
  onError,
  onChangeTarget,
  onTargetRefreshed,
  onRenamed,
  onDeleted,
  onDirtyChange,
}: Props) {
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
  // 注釈にした frame に重ねる枠。キャンバスの見え方が変わるたびに引き直す。
  const [overlayBoxes, setOverlayBoxes] = useState<AnnotationBox[]>([]);
  const [dirty, setDirtyState] = useState(false);
  // 未保存かどうかを ref でも持つ。**待ちを挟んだ判定が古い値を見ないため**
  // （`App` の `unsaved` と同じ理由、ADR 0021）。取り込みはファイルを読む
  // await を挟んでから確認を出すので、その時点の値が要る。
  const dirtyRef = useRef(false);
  // **書くのは必ずこちらを通す。** state だけを書くと ref が置いていかれ、
  // 「未保存かどうか」の答えが 2 つになる。
  const setDirty = useCallback((next: boolean) => {
    dirtyRef.current = next;
    setDirtyState(next);
  }, []);
  const [saving, setSaving] = useState(false);
  // 保存に送るシーンのバイト数。null は「まだ数えていない」。
  //
  // **上限は持たない。** 判定はサーバーだけが持つ（ADR 0018 / 0038）ので、
  // ここが出すのは「いまどれくらいか」という状態にとどめる。
  const [sceneSize, setSceneSize] = useState<number | null>(null);
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
  // 履歴の読み込みも別の世代で持つ。作成すると履歴は 1 件増えるので、走って
  // いる読み込みは古くなる。
  const [runGenerations] = useState(createGenerations);
  // メンバーの一覧を開いているかどうか。
  const [showingMembers, setShowingMembers] = useState(false);
  // 図のドラフトのチャット。**フロントのメモリだけ**（ADR 0041）。ボードを
  // 切り替えると BoardPage ごと作り直される（App の key）ので、持ち越されない。
  const [showingChat, setShowingChat] = useState(false);
  const [chat, setChat] = useState<DiagramChat>(() => startChat("todo"));
  // 生成の世代。**保存では無効にしない。** 生成は保存済みシーンを読まないので、
  // 保存しても前提が変わらない（解釈との非対称、ADR 0041）。
  const [diagramGenerations] = useState(createGenerations);
  // 置いている最中か。**走っているあいだの二重押しは弾く**（`loadingRuns` と
  // 同じ形）。state ではなく ref で覚えるのは、判定が「押した時点の値」を見る
  // 必要があるため。state に置くと、同じ tick に届いた 2 回目がまだ false を
  // 読み、同じ図が同じ場所に重なって置かれる。
  const placing = useRef(false);
  // 作成先の Project に書けるかどうか。確かめるまでは unknown。
  //
  // ボードの取得とは別に訊く。GitHub が未設定・不通でもボードは開ける必要が
  // あるため（ADR 0017）。
  const [projectAccess, setProjectAccess] = useState<ProjectAccess>("unknown");
  // 作成先の表示名を取り直している最中かどうか。
  const [refreshingTarget, setRefreshingTarget] = useState(false);
  // 名前を編集中なら、その下書き。null なら編集していない。
  //
  // **開いているあいだだけ入力を出す。** 常に入力欄にすると、見出しとして
  // 読むところが編集欄になり、押し間違いで名前が変わる。
  const [nameDraft, setNameDraft] = useState<string | null>(null);
  const [renaming, setRenaming] = useState(false);
  // 削除の確認。null なら押されていない（ADR 0042）。
  //
  // **失われるものを引き終わるまで確認を出さない。** 件数を伏せたまま
  // 「削除しますか」と訊くと、何を失うのかを知らないまま押させることになる。
  const [deletion, setDeletion] = useState<DeletionState | null>(null);
  // 注釈 ID をキーにした実行履歴。開いていない注釈は入っていない。
  const [runHistories, setRunHistories] = useState<Record<string, RunHistoryState>>({});

  /**
   * 名前を変える。
   *
   * 画面の見出しをその場で書き換えるだけの操作なので、キャンバスは外れない。
   * 空白だけの名前はサーバーが弾くが、押させないほうが早いのでここでも止める
   * （**判定を持つのではなく、押せない理由を見せる側**）。
   */
  const rename = useCallback(async () => {
    if (nameDraft === null) return;

    const next = nameDraft.trim();
    if (next === "" || next === board.name) {
      setNameDraft(null);
      return;
    }

    setRenaming(true);
    try {
      onRenamed(await boardsApi.rename(board.id, next));
      setNameDraft(null);
    } catch (e) {
      // 編集中の下書きは残す。閉じると、入力し直しからやり直しになる。
      onError(describeFailure("名前を変更できませんでした", e));
    } finally {
      setRenaming(false);
    }
  }, [board.id, board.name, nameDraft, onError, onRenamed]);

  /**
   * 削除で失われるものを引き、確認を出す。
   *
   * **押されたときだけ引く。** 開いたときに数えると、削除するまで要らない
   * 畳み込みをボードを開くたびに引くことになる（中核思想 3、ADR 0037 の
   * 取り直しと同じ形）。
   *
   * 世代は持たない。ボードを切り替えると BoardPage ごと作り直される
   * （App が `key={current.id}` を渡している）ので、遅れて届いた応答が別の
   * ボードの確認として出ることはない。
   */
  const askDelete = useCallback(async () => {
    setDeletion({ status: "loading" });
    try {
      setDeletion({ status: "confirming", losing: await boardsApi.deletion(board.id) });
    } catch (e) {
      // 確認を出さずに閉じる。件数を知らないまま「削除しますか」と訊くと、
      // 見せてから選ばせるという約束（ADR 0042）が守れない。
      setDeletion(null);
      onError(describeFailure("削除で失われるものを確かめられませんでした", e));
    }
  }, [board.id, onError]);

  /**
   * ボードを消す。**取り消せない。**
   *
   * GitHub に作った draft issue は消えない。消えるのは etoki 側の記録の
   * ほうで、残った draft issue の出どころが辿れなくなる（ADR 0042）。
   */
  const deleteBoard = useCallback(async () => {
    if (deletion?.status !== "confirming") return;
    const losing = deletion.losing;

    setDeletion({ status: "deleting", losing });
    try {
      await boardsApi.delete(board.id);
      onDeleted(board.id);
    } catch (e) {
      // 確認は開いたまま戻す。閉じると、押し直すのに引き直しからになる
      // （改名が下書きを残すのと同じ）。
      setDeletion({ status: "confirming", losing });
      onError(describeFailure("ボードを削除できませんでした", e));
    }
  }, [board.id, deletion, onDeleted, onError]);

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

  /**
   * その注釈の実行履歴を引く。
   *
   * **押されたときだけ引く。** 開いただけで全注釈ぶん引くと、注釈の数だけ
   * 問い合わせが増える（中核思想 3、作成先の名前の取り直しと同じ形）。
   *
   * 走っているあいだの二重押しは弾く。state ではなく ref で覚えるのは、
   * 判定が「押した時点の値」を見る必要があるため。
   */
  const loadingRuns = useRef(new Set<string>());

  const loadRuns = useCallback(
    async (annotationId: string) => {
      if (loadingRuns.current.has(annotationId)) return;
      loadingRuns.current.add(annotationId);

      // 走っているあいだに作成が終わると、この応答は 1 件足りない履歴になる。
      // 世代で照合して捨てる（`.claude/rules/async-ui.md`）。
      const generation = runGenerations.start(annotationId);

      setRunHistories((prev) => ({ ...prev, [annotationId]: { status: "loading" } }));
      try {
        const runs = await boardsApi.runs(board.id, annotationId);
        if (!runGenerations.isCurrent(annotationId, generation)) return;
        setRunHistories((prev) => ({
          ...prev,
          [annotationId]: { status: "done", runs },
        }));
      } catch (e) {
        if (!runGenerations.isCurrent(annotationId, generation)) return;
        // パネル内に残す。どの注釈の履歴で失敗したかが情報の一部なので、
        // 画面全体のエラー表示には流さない（解釈の失敗と同じ扱い）。
        setRunHistories((prev) => ({
          ...prev,
          [annotationId]: {
            status: "error",
            failure: describeFailure("履歴を読み込めませんでした", e),
          },
        }));
      } finally {
        loadingRuns.current.delete(annotationId);
      }
    },
    [board.id, runGenerations],
  );

  /**
   * 作成先の表示名を GitHub から取り直す。
   *
   * **押されたときだけ引く。** 開いただけで取りにいくと、ボードを開くたびに
   * GitHub を叩くうえ、名前が変わったことに気づく機会が消える（中核思想 3、
   * ADR 0037）。作成先そのものは固定されたままで、送るのは表示用の 3 つだけ。
   */
  const refreshTargetDisplay = useCallback(async () => {
    setRefreshingTarget(true);
    try {
      const projects = await githubApi.projects(
        board.repositoryOwner,
        board.repositoryName,
      );
      const project = projects.find((p) => p.id === board.projectId);
      if (!project) {
        // GitHub 側から消えた（あるいは見えなくなった）。作成先は固定なので
        // 選び直しでは直せない。分かったことをそのまま出す。
        onError(targetProjectMissingFailure());
        return;
      }

      // 番号も名前も URL も、この画面が GitHub から受け取ったものをそのまま
      // 送る。組み立てない（ADR 0025）。
      onTargetRefreshed(
        await boardsApi.refreshTargetDisplay(board.id, {
          projectId: board.projectId,
          projectNumber: project.number,
          projectTitle: project.title,
          projectUrl: project.url,
        }),
      );
    } catch (e) {
      onError(describeFailure("作成先の名前を取り直せませんでした", e));
    } finally {
      setRefreshingTarget(false);
    }
  }, [
    board.id,
    board.projectId,
    board.repositoryName,
    board.repositoryOwner,
    onError,
    onTargetRefreshed,
  ]);

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
      onError(sceneUnreadableFailure());
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
      onError(describeFailure("注釈の状態を取得できませんでした", e));
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

  /**
   * シーンの大きさを数え直す。
   *
   * 数えるには保存と同じ直列化が要る（画像は `getFiles()` ごと乗る）ので、
   * 描くたびに数えると、いちばん数えたい大きいボードでいちばん重くなる。
   * 変化が止まってから 1 度だけ数える。
   */
  const measureScene = useCallback(() => {
    if (!api) return;
    setSceneSize(sceneBytes(sceneJSON(api)));
  }, [api]);

  const measureTimer = useRef<number | null>(null);

  const scheduleMeasure = useCallback(() => {
    if (measureTimer.current !== null) window.clearTimeout(measureTimer.current);
    measureTimer.current = window.setTimeout(() => {
      measureTimer.current = null;
      measureScene();
    }, MEASURE_DELAY_MS);
  }, [measureScene]);

  // 開いた直後にも 1 度数える。押す前に見せるための表示なので、何か描くまで
  // 空欄というのでは遅い。
  useEffect(() => {
    measureScene();
  }, [measureScene]);

  useEffect(
    () => () => {
      if (measureTimer.current !== null) window.clearTimeout(measureTimer.current);
    },
    [],
  );

  /** 署名を取り込み、保存済みと違えば未保存にする。 */
  const applySignature = useCallback(
    (signature: string) => {
      latestSignature.current = signature;
      // Excalidraw はマウント時にも onChange を発火する。その 1 回目は保存済み
      // シーンそのものなので、未保存ではなく基準として覚える。
      savedSignature.current ??= signature;
      setDirty(signature !== savedSignature.current);
    },
    [setDirty],
  );

  // いまのキャンバスの見え方。**state ではなく ref に持つ。** 重ねる枠の
  // 引き直しにしか使わないので、スクロールのたびに再描画を増やす理由が無い。
  const viewport = useRef<Viewport>({ scrollX: 0, scrollY: 0, zoom: 1 });

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
    (
      elements: readonly unknown[],
      appState: {
        selectedElementIds: Record<string, boolean>;
        scrollX: number;
        scrollY: number;
        zoom: { value: number };
        viewBackgroundColor: string;
      },
    ) => {
      const els = elements as SceneElement[];
      applySignature(sceneSignature(els, appState.viewBackgroundColor));
      scheduleMeasure();
      setSelectedFrames(selectableFrames(els, appState.selectedElementIds));
      setCanvasFrameIds(frameIds(els));

      // スクロールとズームは onChange でしか届かない。要素が変わっていなくても
      // 引き直す必要があるので、ここでまとめて拾う。
      viewport.current = {
        scrollX: appState.scrollX,
        scrollY: appState.scrollY,
        zoom: appState.zoom.value,
      };
      setOverlayBoxes(annotationBoxes(els, viewport.current));
    },
    [applySignature, scheduleMeasure],
  );

  /**
   * 現在のシーンを elements ごと差し替える。
   *
   * `appState` は要素と一緒に変えるものだけを渡す（取り込みの背景色、ADR 0045）。
   * 表示状態そのものはここで触らない。
   */
  const updateElements = useCallback(
    (next: SceneElement[], appState?: Record<string, unknown>) => {
      // 渡された背景色があればそれが新しい色。無ければ変えていないので今の色。
      // **`updateScene` の後に `getAppState()` から読み直さない。** 反映は React
      // の更新を挟むので、直後に読むとまだ前の色が返りうる。
      const background =
        (appState?.viewBackgroundColor as string | undefined) ?? currentBackground();

      api?.updateScene({ elements: next as never, appState: appState as never });
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

  /**
   * プロンプトから図のドラフトを生成する。
   *
   * **キャンバスには何も置かない。** 置くのは `placeDraft` で、そこを人が
   * 押すまでキャンバスは変わらない（#58 の原則、中核思想 3）。
   *
   * **未保存でも呼ぶ。** 保存済みシーンを読まないので、解釈のような
   * 「保存してから」の制約が要らない（ADR 0041）。
   */
  const generateDiagram = useCallback(
    async (prompt: string, internal = false): Promise<boolean> => {
      const generation = diagramGenerations.start(DIAGRAM_KEY);
      // 送るのはいまの会話。**成立した往復だけがここに積まれている**ので、
      // 失敗した指示を「返した図」つきで送ることにはならない。
      const { kind } = chat;
      const history = historyOf(chat);
      setChat((prev) => beginTurn(prev, prompt, internal));

      try {
        const draft = await boardsApi.generateDiagram(board.id, kind, prompt, history);
        // 遅れて届いた応答を今の会話に混ぜない。種類を変えると会話ごと
        // 捨てるので、そのあとに古い図が積まれると土台が食い違う。
        if (!diagramGenerations.isCurrent(DIAGRAM_KEY, generation)) return false;
        setChat((prev) => completeTurn(prev, draft));
        return true;
      } catch (e) {
        if (!diagramGenerations.isCurrent(DIAGRAM_KEY, generation)) return false;
        // **パネルの中に出す。** 会話の続きで直せる失敗なので、画面上部の
        // 通知に出すと、直す場所と理由が離れる。
        setChat((prev) => failTurn(prev, describeFailure("生成できませんでした", e)));
        return false;
      }
    },
    [board.id, chat, diagramGenerations],
  );

  /**
   * 図の種類を変える。**捨てたときだけ、走っている生成も無効にする。**
   *
   * 種類を変えると会話ごと捨てる（`changeKind`）ので、あとから古い図が
   * 積まれると、いまの種類の会話に前の記法の図が土台として載る。パネルは
   * 生成中の選択を止めているが、**止めているのが UI だけだと、そこを外した
   * ときに黙って壊れる**（`.claude/rules/async-ui.md`）。
   *
   * **捨てていないのに世代を進めない。** 同じ種類なら `changeKind` は会話を
   * そのまま返すので `pending` が残る。そこで世代だけ進めると、走っている
   * 生成の応答が捨てられて `pending` を null にする経路が消え、パネルが
   * 「生成中…」のまま戻らなくなる。**捨てたかどうかは `changeKind` の
   * 返り値で決める。** 同じ条件をここにも書くと判定が 2 箇所になる。
   */
  const handleChangeKind = useCallback(
    (kind: DiagramKind) => {
      const next = changeKind(chat, kind);
      if (next === chat) return;

      diagramGenerations.start(DIAGRAM_KEY);
      setChat(next);
    },
    [chat, diagramGenerations],
  );

  /**
   * いまのドラフトをキャンバスに置く。
   *
   * **既存の要素には一切触らない。追加するだけ**（#58 の原則）。置き場所は
   * 既存の絵の右外で、重ねない（ADR 0040）。**保存はしない。** 確定させるのは
   * 人間の保存操作だけ。
   *
   * 変換に失敗したら、会話の次の 1 往復として投げ直す。mermaid として読める
   * かではなく Excalidraw の要素として置けるかを知っているのは変換器だけ
   * なので、投げ直せるのはここしかない（ADR 0041）。
   */
  const placeDraft = useCallback(async () => {
    // 変換は非同期。**押した時点で弾かないと、2 回目が同じ `draftOrigin` を
    // 得て、同じ図が同じ場所に重なる。** 取り消しで戻すしかなくなる。
    if (!api || chat.draft === null || placing.current) return;
    placing.current = true;

    try {
      const converted = await mermaidToElements(chat.draft.mermaid);
      if (!converted.ok) {
        if (converted.reason === "syntax") {
          // 直せる失敗。会話の次の 1 往復にして投げ直す。
          // **利用者が打った指示ではない**ので、そう印を付けて積む。画面には
          // 固定文で出る（`turnLabel`）。
          void generateDiagram(conversionRetryPrompt(converted.detail), true);
          return;
        }
        // 置ける形にならない種類だった。投げ直しても同じものが返るので、
        // 種類を変えてもらう（ADR 0040）。
        setChat((prev) => failTurn(prev, diagramNotPlaceableFailure()));
        return;
      }

      const existing = currentElements();
      const placed = moveDraft(converted.elements, draftOrigin(existing));
      updateElements([...existing, ...placed]);

      // 置いた先へ寄せる。既存の絵の外に置くので、寄せないと押したのに何も
      // 起きていないように見える（ADR 0040）。
      api.scrollToContent(placed as never, { fitToContent: true, animate: true });
    } finally {
      placing.current = false;
    }
  }, [api, chat.draft, currentElements, generateDiagram, updateElements]);

  /**
   * 付箋を 1 枚置く。
   *
   * **ダイアログを挟まない。** 描いている最中の操作なので、手数の少なさが
   * そのまま値打ちになる（矩形を描いて色を選んで文字を書く、を 1 手にする）。
   * 置くだけで保存はしない。確定させるのは人間の保存操作だけ。
   *
   * 置いた付箋は選んでおく。Enter でそのまま書き始められる。
   */
  const addStickyNote = useCallback(() => {
    if (!api) return;

    const elements = currentElements();
    const { scrollX, scrollY, zoom, width, height } = api.getAppState();
    const { x, y } = stickyNotePosition(
      { scrollX, scrollY, zoom: zoom.value, width, height },
      elements,
    );

    const note = createStickyNote(x, y);
    updateElements([...elements, ...note]);

    const id = note[0]?.id;
    if (id !== undefined) {
      api.updateScene({ appState: { selectedElementIds: { [id]: true } } } as never);
    }
  }, [api, currentElements, updateElements]);

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

  /**
   * いまのキャンバスを `.excalidraw` として書き出す（ADR 0045）。
   *
   * **保存済みシーンではなくキャンバスから出す。** 保存済みから出すと、未保存の
   * 描き足しが黙って落ちる（中核思想 3）。**未保存でも押させる。** 入力は 1 つ
   * しかないので、解釈のように揃うまで待たせる理由が無い（ADR 0018 との違い）。
   *
   * viewer にも出す。見えているものを出すだけで、共有した相手にはすでに全部
   * 見えている（ADR 0017）。
   */
  const exportScene = useCallback(() => {
    if (!api) return;

    // 保存が送るのと同じバイト列。ヘッダーに出ている大きさが、そのまま
    // 書き出したファイルの大きさになる。
    const url = URL.createObjectURL(
      new Blob([sceneJSON(api)], { type: "application/json" }),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = exportFileName(board.name);
    // DOM に入っていない a のクリックを無視するブラウザがあるので、一度入れる。
    document.body.appendChild(link);
    link.click();
    link.remove();
    // 押した直後に外すと、ダウンロードが始まる前に URL が消えることがある。
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  }, [api, board.name]);

  const fileInput = useRef<HTMLInputElement | null>(null);
  // 取り込んでいる最中か。**押した時点で弾く**（`placing` と同じ形）。ファイルの
  // 読み込みは非同期なので、state で覚えると 2 回目がまだ false を読む。
  const importingFile = useRef(false);

  /**
   * `.excalidraw` ファイルをキャンバスに取り込む（ADR 0045）。
   *
   * **サーバーには何も送らない。** 載せるだけで、確定させるのは人間の保存操作
   * だけ（中核思想 3）。取り込んだシーンの検証・版の照合・大きさの上限は、
   * その保存の経路のまま効く。
   *
   * **引いた解釈は捨てない。** これはキャンバスの編集であって保存ではない。
   * 捨てるのは保存したときだけで、それまでは未保存なので解釈は押せない
   * （ADR 0018）。
   */
  const importScene = useCallback(
    async (file: File) => {
      if (!api || importingFile.current) return;
      importingFile.current = true;

      try {
        let imported: ImportedScene;
        try {
          // **読んでから訊く**（`App.open` と同じ形、ADR 0021）。訊いてから
          // 読むと、読んでいるあいだの描き足しを確認なしで捨てる。読めなかった
          // ときに捨ててよいかを訊いてしまうことも無くなる。
          imported = await readSceneFile(file, api);
        } catch (e) {
          // 読めなかった。**キャンバスには触らない。** 例外の中身は画面に
          // 出さず console に残す（`web/CLAUDE.md`）。
          log.error("シーンファイルを読み込めませんでした", e);
          onError(sceneFileUnreadableFailure());
          return;
        }

        // **ここから先に待ちは無い。** 挟むと、待っているあいだの描き足しを
        // 確認なしで捨てる。
        if (
          dirtyRef.current &&
          !window.confirm(
            "取り込むと、いまキャンバスにある内容は置き換わります。未保存の変更は失われます。",
          )
        ) {
          return;
        }

        updateElements(
          imported.elements,
          imported.viewBackgroundColor === undefined
            ? undefined
            : { viewBackgroundColor: imported.viewBackgroundColor },
        );
        // 貼ってあった画像。**入れ忘れると画像の要素だけが空白で置かれる。**
        api.addFiles(imported.files as never);

        // 取り込んだ絵が画面の外にあると、押しても何も起きていないように見える
        // （ADR 0040 で図のドラフトに対して決めたのと同じ形）。空のシーンには
        // 寄せる先が無い。
        if (imported.elements.length > 0) {
          api.scrollToContent(imported.elements as never, { fitToContent: true });
        }
      } finally {
        importingFile.current = false;
      }
    },
    [api, onError, updateElements],
  );

  const save = useCallback(async () => {
    if (!api) return;

    setSaving(true);
    try {
      const elements = api.getSceneElements();
      const scene = sceneJSON(api);
      // 送った内容そのものを新しい基準にする。保存の待ち時間に編集されていたら
      // 未保存のまま残す必要があるので、setDirty(false) とは書かない。
      // **背景色も `scene` に載っている**ので、基準にも同じものを含める。
      const sent = sceneSignature(
        elements as unknown as SceneElement[],
        currentBackground(),
      );

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
      if (e instanceof ApiError && e.code === "scene_conflict") {
        setConflicted(true);
        return;
      }
      onError(describeFailure("保存できませんでした", e));
    } finally {
      setSaving(false);
    }
  }, [
    api,
    board.id,
    creationGenerations,
    currentBackground,
    generations,
    onError,
    refreshAnnotations,
    setDirty,
  ]);

  /**
   * 注釈を解釈させる。
   *
   * エラーはパネル内に残す。どの注釈で何が起きたか分からなくなるので、
   * 画面全体のエラー表示には流さない。
   */
  const interpret = useCallback(
    async (annotationId: string) => {
      // 応答を受け取ったとき、これがまだ最新の要求かを判断できるようにする。
      // 履歴に積むかどうかもこれで決める。**捨てるべき応答を捨てる責任は
      // 世代側にあり、履歴は返ってきたものを積むだけ。**
      const generation = generations.start(annotationId);
      // 実行したときの粒度を控える。並べて見比べるとき、同じ指定で引き直した
      // のか指定を変えたのかが読めないと選ぶ理由が無い。判定に使う粒度は
      // これまでどおり保存済みシーン側（AnnotationStatus）のもの。
      const granularity =
        annotations.find((a) => a.id === annotationId)?.granularity ?? "";

      setInterpretations((prev) => ({
        ...prev,
        [annotationId]: startInterpretation(prev[annotationId]),
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
          [annotationId]: addInterpretation(prev[annotationId], {
            id: generation,
            at: new Date().toISOString(),
            granularity,
            result,
          }),
        }));
      } catch (e) {
        if (!generations.isCurrent(annotationId, generation)) return;
        setInterpretations((prev) => ({
          ...prev,
          [annotationId]: failInterpretation(
            prev[annotationId],
            describeFailure("解釈できませんでした", e),
          ),
        }));
      }
    },
    [annotations, api, board.id, generations],
  );

  /**
   * 見る解釈を選び直す。
   *
   * 画面の中だけの操作なので、サーバーにも世代にも触らない。実行中の解釈が
   * 返ってきたら、そちらが選ばれ直す（`addInterpretation`）。引き直した直後に
   * 前の結果が出ていると、押した操作と画面が食い違うため。
   */
  const showInterpretation = useCallback((annotationId: string, runId: number) => {
    setInterpretations((prev) => {
      const state = prev[annotationId];
      if (!state) return prev;
      return { ...prev, [annotationId]: selectInterpretation(state, runId) };
    });
  }, []);

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
        // 履歴は 1 件増えたので、引いてあるものは捨てる。**黙って古いまま
        // 出さない。** 読み直すかどうかは、これまでどおり押した人が決める。
        // 走っている読み込みも無効にする。捨てた直後に古い応答が入ると、
        // 作ったばかりの run が抜けた履歴が残る。
        runGenerations.start(annotationId);
        setRunHistories((prev) => {
          if (!(annotationId in prev)) return prev;
          const next = { ...prev };
          delete next[annotationId];
          return next;
        });
        await refreshAnnotations();
      } catch (e) {
        if (!creationGenerations.isCurrent(annotationId, generation)) return;
        setCreations((prev) => ({
          ...prev,
          [annotationId]: {
            status: "error",
            failure: describeFailure("作成できませんでした", e),
          },
        }));
      }
    },
    [board.id, creationGenerations, refreshAnnotations, runGenerations],
  );

  // 保存と作成は互いに排他にする。作成中に保存させないのは、GitHub への作成が
  // 取り消せないため。実行中にシーンが変わると、作られた内容と記録されるハッシュ
  // が食い違いうる。逆に保存は creations を捨てるので、保存中に作らせると
  // GitHub には残ったまま結果だけ消え、作られていないと思って再実行した開発者が
  // draft issue を重複させる。保存側は creating で、作成側は saving を渡して止める。
  const creating = Object.values(creations).some((c) => c.status === "running");

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
  const link = projectLink(board);

  // 作成先を変更できない理由。押せるなら null（ADR 0039）。
  //
  // **未保存が先。** `dirty` を下ろすのは応答が返ってから（`save`）なので、
  // 保存中もこちらが出続ける。保存中の文が出るのは、変更が無いまま保存を
  // 押したとき。そこも押せないことに変わりはないので、理由を空けない。
  const targetChangeBlocked = dirty
    ? "保存してから作成先を変更できます"
    : saving
      ? "保存が終わるまで作成先を変更できません"
      : null;

  return (
    <div className="board">
      <header className="board-header">
        {nameDraft === null ? (
          <h1>
            {board.name}
            {/*
              名前はブレストの中身に属する表示物なので、editor にも直させる
              （作成先の変更は owner だけ、ADR 0017）。押せる人にだけ出す。
            */}
            {canEdit && (
              <button
                type="button"
                className="rename"
                onClick={() => setNameDraft(board.name)}
              >
                名前を変更
              </button>
            )}
          </h1>
        ) : (
          <form
            className="rename-form"
            onSubmit={(e) => {
              e.preventDefault();
              void rename();
            }}
          >
            {/*
              ラベルはサイドバーの「ボード名」（新規作成の入力）と分ける。
              同じ名前にすると、読み上げでも E2E でも 2 つが区別できない。
            */}
            <input
              aria-label="ボードの名前"
              value={nameDraft}
              disabled={renaming}
              onChange={(e) => setNameDraft(e.target.value)}
              /*
                jsx-a11y が禁じているのは「開いた瞬間に勝手に焦点が移る」
                autoFocus で、ここはそれに当たらない。押した「名前を変更」が
                この入力に差し替わるので、移さないとキーボードの利用者の焦点は
                body に落ちる。**外すほうが a11y は悪くなる。** 規則が見て
                いるのは属性で、押した結果として現れたかどうかは見られない
                （ADR 0039）。
              */
              // eslint-disable-next-line jsx-a11y/no-autofocus
              autoFocus
            />
            {/*
              「保存」とは書かない。ヘッダーにはシーンの保存ボタンが並んで
              いるので、同じ文言だと何を保存するのかが読めない。
            */}
            <button type="submit" disabled={renaming || nameDraft.trim() === ""}>
              {renaming ? "変更中…" : "名前を保存"}
            </button>
            <button type="button" disabled={renaming} onClick={() => setNameDraft(null)}>
              取消
            </button>
          </form>
        )}
        <div className="board-actions">
          {/*
            自分が何をできるのかは、操作して断られる前に見えている必要がある。
            共有すると「開けるが書けない」が普通に起きる（ADR 0017）。
          */}
          <span className="badge badge-role">{ROLE_LABELS[board.role]}</span>
          {/*
            共有が組み立てられていない構成では、押しても 503 しか返らない。
            ボタンを黙って消さず、代わりに理由を出す（中核思想 3）。作成先の
            変更を owner 以外に出さないのと同じ形。
          */}
          {sharingUnavailable !== null ? (
            <span className="hint">{sharingUnavailable}</span>
          ) : (
            <button type="button" onClick={() => setShowingMembers((v) => !v)}>
              {showingMembers ? "メンバーを閉じる" : "メンバー"}
            </button>
          )}
          {/*
            図のドラフト。**viewer には出さない**（ADR 0017）。生成は LLM を
            叩く外部呼び出しで課金も伴うので、解釈と同じ扱いにする。

            LLM が未設定でもボタンは出す。**黙って消さず、開いた先で理由を
            見せる**（ADR 0030、中核思想 3）。
          */}
          {canEdit && (
            <button type="button" onClick={() => setShowingChat((v) => !v)}>
              {showingChat ? "図のドラフトを閉じる" : "図のドラフト"}
            </button>
          )}
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
            //
            // **名前の取り直しだけは出す。** 固定するのは作成先そのもので
            // あって、表示用のスナップショットではない（ADR 0037）。ここが
            // 無いと、GitHub 側で改名されたボードは古い名前を出し続ける。
            <>
              <span className="hint">作成先は確定（draft issue を作成済み）</span>
              {/*
                GitHub が組み立てられていない構成では、押しても Project の
                一覧を引けない。ボタンを黙って消さず、代わりに理由を出す
                （ADR 0030）。メンバーの口と同じ形。
              */}
              {creationUnavailable !== null ? (
                <span className="hint">{creationUnavailable}</span>
              ) : (
                <button
                  type="button"
                  onClick={() => void refreshTargetDisplay()}
                  disabled={refreshingTarget}
                >
                  {refreshingTarget ? "取り直し中…" : "作成先の名前を取り直す"}
                </button>
              )}
            </>
          ) : (
            <>
              <button
                type="button"
                onClick={onChangeTarget}
                // 選択画面に移るとキャンバスごと外れ、未保存の編集は失われる。
                // 黙って捨てずに、保存してからにしてもらう。
                disabled={dirty || saving}
                aria-describedby={
                  targetChangeBlocked !== null ? "target-change-blocked" : undefined
                }
              >
                作成先を変更
              </button>
              {targetChangeBlocked !== null && (
                <span className="hint" id="target-change-blocked">
                  {targetChangeBlocked}
                </span>
              )}
            </>
          )}
          {/*
            付箋は描いている最中に使うものなので、パネルではなくヘッダーに
            置いて常に 1 手で押せるようにする。
          */}
          {canEdit && (
            <button type="button" onClick={addStickyNote} disabled={!api}>
              付箋
            </button>
          )}
          {/*
            持ち出しと取り込みの口はここ 1 つ（ADR 0045）。ライブラリのメニュー
            からは外してある（`UI_OPTIONS`）。

            書き出しは viewer にも出す。見えているものを出すだけなので、
            共有した相手に新しく見せるものが無い（ADR 0017）。
          */}
          <button type="button" onClick={exportScene} disabled={!api}>
            書き出し
          </button>
          {canEdit && (
            <>
              <button
                type="button"
                onClick={() => fileInput.current?.click()}
                // **作成中は取り込ませない。** キャンバスを置き換えるので、
                // 保存を止めているのと同じ理由で止める（作られた内容と記録
                // されるハッシュが食い違いうる）。
                disabled={!api || saving || creating}
                aria-describedby={creating ? "import-blocked" : undefined}
              >
                取り込み
              </button>
              {creating && (
                <span className="hint" id="import-blocked">
                  作成が終わるまで取り込めません
                </span>
              )}
              {/*
                入力そのものは出さない。**押す口はボタン 1 つ**で、ここは
                ファイルを選ばせるためだけに置いてある。
              */}
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
            </>
          )}
          {/*
            いまの大きさを出す。**上限との比は出さない。** 比を出すには上限を
            フロントが知る必要があり、それは判定を 2 箇所に持つのと同じこと
            になる（ADR 0018 / 0038）。「大きいときだけ」出さないのも同じ
            理由で、上限を知らない以上どこからが大きいのかを決められない。
          */}
          {sceneSize !== null && (
            <span className="badge badge-size" title="保存に送るシーンの大きさ">
              {formatSceneSize(sceneSize)}
            </span>
          )}
          {dirty && <span className="dirty">未保存</span>}
          {canEdit && (
            <>
              {/*
                取り消せない操作と保存は相互に排他する。**押せない理由を title に
                隠さない**（ADR 0039）。ホバーでしか読めず、disabled なボタンは
                フォーカスも当たらないので、キーボードと読み上げには届かない。
                「作成先を変更」と同じ形で本文に出して aria-describedby で結ぶ。
              */}
              <button
                type="button"
                onClick={() => void save()}
                disabled={saving || creating || !api}
                aria-describedby={creating ? "save-blocked" : undefined}
              >
                {saving ? "保存中…" : "保存"}
              </button>
              {creating && (
                <span className="hint" id="save-blocked">
                  作成が終わるまで保存できません
                </span>
              )}
            </>
          )}
          {/*
            ボードごと畳むのは owner だけ（ADR 0042）。押せる人にだけ出すのは
            「作成先を変更」と同じ形。**押した時点では消さない。** 何が残るのかを
            引いてから確認を出す。

            **消すなら理由を出す**（ADR 0017 / 0030）。権限で押せない操作は、
            ボタンを黙って消さずに押せない理由のほうを見せる。disabled にせず
            文だけにするのも「作成先を変更」と揃えている。ロールは開いている
            あいだ変わらないので、押せる見込みの無いボタンを置く相手がいない。
          */}
          {board.role === "owner" ? (
            <button
              type="button"
              className="danger"
              onClick={() => void askDelete()}
              disabled={deletion !== null}
            >
              {deletion?.status === "loading" ? "確認中…" : "ボードを削除"}
            </button>
          ) : (
            <span className="hint">ボードを削除できるのはオーナーだけです</span>
          )}
        </div>
      </header>

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
              onClick={() => setDeletion(null)}
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
        パネルは境界で包み、キャンバスを巻き込ませない。落ちたのがパネルでも、
        外側の 1 枚だけで受けるとツリーごと外れ、保存していないブレストが
        その場で消える（ADR 0027）。
      */}
      {showingMembers && (
        <ErrorBoundary name="メンバーパネル" recovery="remount">
          <MemberPanel
            boardId={board.id}
            role={board.role}
            onClose={() => setShowingMembers(false)}
          />
        </ErrorBoundary>
      )}

      <div className="board-body">
        {/*
          チャットはキャンバスの左に置く。**キャンバスを覆わない。** 置いた図が
          どこに出るかを見ながら直す道具なので、隠すと「置く」を押した結果が
          確かめられない。メンバーのように上に敷かないのもそのため。

          **パネルは境界で包む**（ADR 0027）。落ちたのがここでも外側の 1 枚で
          受けると、キャンバスごと外れて未保存のブレストが消える。
        */}
        {showingChat && canEdit && (
          <ErrorBoundary name="図のドラフト" recovery="remount">
            <DiagramChatPanel
              chat={chat}
              onChangeKind={handleChangeKind}
              onSend={generateDiagram}
              onPlace={() => void placeDraft()}
              onClose={() => setShowingChat(false)}
              unavailable={diagramUnavailable}
            />
          </ErrorBoundary>
        )}

        <div className="canvas">
          <Excalidraw
            excalidrawAPI={setApi}
            initialData={initialData as never}
            onChange={handleChange as never}
            langCode="ja-JP"
            // 持ち出しと取り込みの口は etoki のヘッダーに寄せてある（ADR 0045）。
            UIOptions={UI_OPTIONS}
            // viewer には描かせない。描けるのに保存できないと、描いた内容を
            // 黙って捨てることになる（ADR 0017）。
            viewModeEnabled={!canEdit}
          />
          {/*
            注釈の frame を見分けられるようにする。Excalidraw の外に重ねる
            だけで、要素には触らない（AnnotationOverlay の doc）。

            **枠も境界で包む。** 包まずに落ちると外側の 1 枚が受けることになり、
            キャンバスごと外れて未保存のブレストが消える（ADR 0027）。
          */}
          <ErrorBoundary name="注釈の枠" recovery="remount">
            <AnnotationOverlay boxes={overlayBoxes} />
          </ErrorBoundary>
        </div>

        <ErrorBoundary name="注釈パネル" recovery="remount">
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
            onSelectInterpretation={showInterpretation}
            runHistories={runHistories}
            onLoadRuns={(id) => void loadRuns(id)}
            creations={creations}
            saving={saving}
            onCreate={(id, interpretation) => void create(id, interpretation)}
            canEdit={canEdit}
            projectAccess={projectAccess}
            interpretationUnavailable={interpretationUnavailable}
            creationUnavailable={creationUnavailable}
            projectLink={link}
          />
        </ErrorBoundary>
      </div>
    </div>
  );
}
