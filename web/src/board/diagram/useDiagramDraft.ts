import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { boardsApi } from "../../api/boards";
import {
  describeFailure,
  diagramNotPlaceableFailure,
  mermaidPasteFailure,
} from "../../api/errorMessage";
import type {
  DiagramDraft,
  DiagramJob,
  DiagramKind,
  GenerateDiagramRequest,
} from "../../api/types";
import { log } from "../../app/logger";
import type { SceneElement } from "../../excalidraw/annotation";
import { draftOrigin, mermaidToElements, moveDraft } from "../../excalidraw/mermaid";
import { pasteToElements } from "../../excalidraw/mermaidPaste";
import { useReentryGuard } from "../exclusion";
import { createGenerations } from "../generation";
import { JOB_POLL_INTERVAL_MS, pollJob } from "../jobPolling";
import {
  beginTurn,
  changeKind as changeChatKind,
  completeTurn,
  conversionRetryPrompt,
  failTurn,
  historyOf,
  resumeChat,
  startChat,
  type DiagramChat,
} from "./diagramChat";
import type { PasteOutcome } from "./MermaidPastePanel";

/**
 * 図のドラフト生成の世代キー。
 *
 * 会話は 1 つしか持たないので 1 本でよい。解釈が注釈ごとに採番するのとは
 * 違って、区別する相手がいない。
 */
const DIAGRAM_KEY = "diagram";

/** 置いたドラフトと、そのときキャンバスに足した要素の ID。 */
type Placement = { draft: DiagramDraft; ids: readonly string[] };

/**
 * いまのドラフトを置いた図が、まだキャンバスに残っているか。
 *
 * **1 つでも残っていれば残っている。** 一部だけ消した図の隣に、まるごともう 1 つ
 * 置かせない。**`isDeleted` の要素は残っていないと数える。** 取り消した要素を
 * 消した印つきで持っていることがある。
 */
function placedOnCanvas(
  placement: Placement | null,
  draft: DiagramDraft | null,
  elements: readonly SceneElement[],
): boolean {
  if (placement === null || draft === null || placement.draft !== draft) return false;
  const ids = new Set(placement.ids);
  return elements.some((el) => !el.isDeleted && ids.has(el.id));
}

type Options = {
  api: ExcalidrawImperativeAPI | null;
  boardId: string;
  /** いまキャンバスにある要素。置き場所を決めるのに読む。 */
  currentElements: () => SceneElement[];
  /** キャンバスの要素を差し替える。人の操作として履歴に積む（`BoardPage`）。 */
  updateElements: (next: SceneElement[]) => void;
  /**
   * 置けたら呼ぶ。スマホの置き方ではパネルが全面でキャンバスを隠すので、
   * 閉じて置いた図を見せる（#199、`BoardPage`）。**置けなかったら呼ばない。**
   * パネルに出した理由を読ませる。
   */
  onPlaced?: () => void;
  /**
   * 生成を使えるか。使えるときだけ、開いたときに走っているジョブを引く。
   *
   * LLM が未設定のサーバーや viewer に引いても、503 / 403 が返るだけで見せるものが
   * 無い。
   */
  enabled: boolean;
  /** ジョブを見に行く間隔。テストが待たずに済むよう差し替えられる。 */
  pollIntervalMs?: number;
};

export type DiagramDrafting = {
  /** 図のドラフトのチャット。**フロントのメモリだけ**（ADR 0041）。 */
  chat: DiagramChat;
  /** 図への指示の入力欄に書かれている文字列。 */
  prompt: string;
  setPrompt: (text: string) => void;
  /** プロンプトから図のドラフトを生成する。成立したら true。 */
  generate: (prompt: string, internal?: boolean) => Promise<boolean>;
  /** 図の種類を変える。変えたら会話ごと捨てる。 */
  changeKind: (kind: DiagramKind) => void;
  /** いまのドラフトをキャンバスに置く。 */
  placeDraft: () => Promise<void>;
  /**
   * いまのドラフトを置いた図が、まだキャンバスに残っているか。残っているあいだは
   * `placeDraft` が置かない。
   */
  draftPlaced: boolean;
  /** mermaid の貼り付けパネルに貼られている文字列。 */
  pasteText: string;
  setPasteText: (text: string) => void;
  /** 貼られた mermaid を変換して置く（ADR 0062）。 */
  pasteMermaid: (text: string) => Promise<PasteOutcome>;
};

/**
 * 図のドラフト（LLM に作らせる・mermaid を貼る・置く）。#146 で `BoardPage` から
 * 切り出し。
 *
 * ボードを切り替えると BoardPage ごと作り直される（App の key）ので、会話も
 * 貼りかけの文字列も持ち越されない。
 *
 * **生成はサーバーのジョブとして走る**（ADR 0091）。ここは始めて、終わるまで
 * 見に行き、結果を会話に積む。画面を離れても生成は止まらず、開き直したときに
 * 受け取っていないぶんを会話ごと戻して続きを見に行く。
 */
export function useDiagramDraft({
  api,
  boardId,
  currentElements,
  updateElements,
  onPlaced,
  enabled,
  pollIntervalMs = JOB_POLL_INTERVAL_MS,
}: Options): DiagramDrafting {
  const [chat, setChat] = useState<DiagramChat>(() => startChat("todo"));
  // 生成の世代。**保存では無効にしない。** 生成は保存済みシーンを読まないので、
  // 保存しても前提が変わらない（解釈との非対称、ADR 0041）。
  const [generations] = useState(createGenerations);
  // 図への指示の入力欄。**パネルではなくここで持つ。** 開き直したときに、走って
  // いる生成の指示をここへ戻す（ADR 0091）。送っている最中の画面は、送った指示が
  // 入力欄に残ったまま止まっているので、戻した画面もそれと同じにする。
  const [prompt, setPrompt] = useState("");
  // mermaid の貼り付けパネルに貼られている文字列。**パネルではなくここで
  // 持つ。** 置けたときに消すのはここ（`pasteMermaid`）で、パネルは落ちたときに
  // 境界で作り直される（ADR 0027）。そちらで持つと、構文エラーを直している
  // 途中の入力が消える。
  const [pasteText, setPasteText] = useState("");
  // 置いている最中か。**走っているあいだの二重押しは弾く**（実行の履歴の読み込み
  // と同じ形）。**排他の表とは別物**で、止めるのは同じ操作の連打だけ。理由と
  // 仕組みは `exclusion.ts` の `useReentryGuard`。**図のドラフトと貼り付けで
  // 1 つにする。** どちらも同じ `draftOrigin` を読むので、分けると両方が同じ
  // 場所に置かれうる。
  const placing = useReentryGuard();
  // いまのドラフトを置いたときに足した要素。**同じドラフトは、置いた図が
  // キャンバスに残っているあいだ置き直させない**（ADR 0040 の追記）。置き場所は
  // 毎回既存の絵の右外なので、押すたびに同じ図が増え、重ならないぶん増えたことにも
  // 気づきにくい。**数え直すのは、残っているかどうかだけ。** 取り消しや削除で
  // 消えたら、往復を使わずに置き直せる。
  //
  // state は描くため、ref は押した時点の値で弾くため（`.claude/rules/async-ui.md`）。
  // state は置き終えてから描き直すまで古い。
  const [placement, setPlacement] = useState<Placement | null>(null);
  const latestPlacement = useRef<Placement | null>(null);

  // 見に行くのをやめる印。**ジョブは止めない。** 立てるのは effect で、StrictMode が
  // effect を 2 回走らせても、2 回目で立て直す。
  const alive = useRef(false);
  // 利用者が会話を動かした回数（送る・種類を変える）。開いたときにジョブを引く
  // あいだに動かしていたら、引いたジョブで会話を上書きしない。
  const touched = useRef(0);
  // いま見に行っているジョブ。種類を変えて会話ごと捨てるとき、サーバーでも止める。
  const tracking = useRef<string | null>(null);

  /** ジョブを要らないものとして捨てる。失敗しても何もしない。 */
  const discardJob = useCallback(
    (jobId: string) => {
      if (tracking.current === jobId) tracking.current = null;
      // 捨て損ねても、保持の期限（1 時間）で消える。画面に出すものが無い。
      boardsApi.discardDiagramJob(boardId, jobId).catch(() => undefined);
    },
    [boardId],
  );

  /**
   * ジョブが走り終わるまで見に行き、結果か失敗を会話に積む。成立したら true。
   *
   * **捨てるべき応答を捨てる責任は世代側にある。** 世代が古くなったとき
   * （種類を変えた、画面を離れた）は何も積まない。
   */
  const settle = useCallback(
    async (generation: number, jobId: string, sentPrompt: string): Promise<boolean> => {
      const isCurrent = () =>
        alive.current && generations.isCurrent(DIAGRAM_KEY, generation);

      let outcome;
      try {
        outcome = await pollJob(
          () => boardsApi.diagramJob(boardId, jobId),
          isCurrent,
          pollIntervalMs,
        );
      } catch (e) {
        if (!isCurrent()) return false;
        // **パネルの中に出す。** 会話の続きで直せる失敗なので、画面上部の
        // 通知に出すと、直す場所と理由が離れる。
        setChat((prev) => failTurn(prev, describeFailure("生成できませんでした", e)));
        return false;
      }
      if (!outcome || !isCurrent()) return false;

      // 受け取り終えた。残すとサーバーのメモリに溜まり、開き直したときに同じ結果が
      // もう一度出る。
      discardJob(jobId);
      if (outcome.kind === "failed") {
        setChat((prev) =>
          failTurn(prev, describeFailure("生成できませんでした", outcome.error)),
        );
        return false;
      }

      setChat((prev) => completeTurn(prev, outcome.result));
      // 送った指示は往復に積まれた。入力欄に残すと、同じ指示を 2 度送りやすい。
      // **送った指示のままのときだけ消す。** 投げ直しの指示は入力欄の文字列とは
      // 別物なので、置く前に書いていた次の指示は残る。
      setPrompt((current) => (current === sentPrompt ? "" : current));
      return true;
    },
    [boardId, discardJob, generations, pollIntervalMs],
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
  const generate = useCallback(
    async (text: string, internal = false): Promise<boolean> => {
      touched.current += 1;
      const generation = generations.start(DIAGRAM_KEY);
      // 送るのはいまの会話。**成立した往復だけがここに積まれている**ので、
      // 失敗した指示を「返した図」つきで送ることにはならない。
      const body: GenerateDiagramRequest = {
        kind: chat.kind,
        prompt: text,
        history: historyOf(chat),
        internal,
      };
      setChat((prev) => beginTurn(prev, text, internal));

      let jobId: string;
      try {
        jobId = (await boardsApi.generateDiagram(boardId, body)).id;
      } catch (e) {
        // 遅れて届いた応答を今の会話に混ぜない。種類を変えると会話ごと
        // 捨てるので、そのあとに古い失敗が出ると土台が食い違う。
        if (!generations.isCurrent(DIAGRAM_KEY, generation)) return false;
        setChat((prev) => failTurn(prev, describeFailure("生成できませんでした", e)));
        return false;
      }

      // 始めているあいだに種類を変えて、会話ごと捨てた。受け取らずにサーバーでも
      // 止める。止めないと、捨てた会話の生成が枠を握り続ける。
      if (!generations.isCurrent(DIAGRAM_KEY, generation)) {
        discardJob(jobId);
        return false;
      }

      tracking.current = jobId;
      return settle(generation, jobId, text);
    },
    [boardId, chat, discardJob, generations, settle],
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
  const changeKind = useCallback(
    (kind: DiagramKind) => {
      const next = changeChatKind(chat, kind);
      if (next === chat) return;

      touched.current += 1;
      generations.start(DIAGRAM_KEY);
      // サーバーで走っている生成も止める。捨てた会話の生成が枠を握り続けると、
      // 次の生成も解釈も 429 に弾かれる。
      if (tracking.current !== null) discardJob(tracking.current);
      setChat(next);
    },
    [chat, discardJob, generations],
  );

  // 開いたとき、走っている生成と、終わったが受け取っていない生成を、会話ごと
  // 画面に戻す（ADR 0091）。**ボードごとに 1 回だけ引く。**
  useEffect(() => {
    alive.current = true;
    if (!enabled) {
      return () => {
        alive.current = false;
      };
    }

    let cancelled = false;
    const startedTouched = touched.current;
    void (async () => {
      let jobs: DiagramJob[];
      try {
        jobs = await boardsApi.diagramJobs(boardId);
      } catch {
        // 戻すのは付け足し。引けなくても、送れば始められる。
        return;
      }
      // 引いているあいだに送ったか、種類を変えた。いまの会話を引いたジョブで
      // 上書きしない。
      if (cancelled || touched.current !== startedTouched) return;

      const latest = jobs.at(-1);
      if (latest === undefined) return;
      // 会話は 1 本なので、戻すのはいちばん新しいもの 1 つ。ほかは捨て損ねた
      // ものなので、ここで捨てる。
      for (const job of jobs.slice(0, -1)) discardJob(job.id);

      const { request } = latest;
      const generation = generations.start(DIAGRAM_KEY);
      setChat(resumeChat(request));
      // 送っている最中の画面と同じく、送った指示を入力欄に戻す。投げ直しは
      // 利用者の入力ではないので戻さない。
      if (!request.internal) setPrompt(request.prompt);
      tracking.current = latest.id;
      void settle(generation, latest.id, request.prompt);
    })();

    return () => {
      cancelled = true;
      alive.current = false;
    };
  }, [boardId, discardJob, enabled, generations, settle]);

  /**
   * 変換した要素をキャンバスに置く。図のドラフトと貼り付けで共有する。
   *
   * **既存の要素には一切触らない。追加するだけ**（#58 の原則）。置き場所は
   * 既存の絵の右外で、重ねない（ADR 0040）。**保存はしない。**
   *
   * 置いた要素を返す。置けなかったら null。
   */
  const placeElements = useCallback(
    (elements: readonly SceneElement[]): readonly SceneElement[] | null => {
      if (!api) return null;
      const existing = currentElements();
      const placed = moveDraft(elements, draftOrigin(existing));
      updateElements([...existing, ...placed]);

      // 置いた先へ寄せる。既存の絵の外に置くので、寄せないと押したのに何も
      // 起きていないように見える（ADR 0040）。
      api.scrollToContent(placed as never, { fitToContent: true, animate: true });
      onPlaced?.();
      return placed;
    },
    [api, currentElements, updateElements, onPlaced],
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
   *
   * **置いた図がキャンバスに残っているあいだは、同じドラフトを置かない**
   * （`draftPlaced`）。パネルもボタンを止めているが、止めているのが UI だけだと、
   * そこを外したときに黙って増える（`.claude/rules/async-ui.md`）。
   */
  const placeDraft = useCallback(async () => {
    const draft = chat.draft;
    if (!api || draft === null) return;
    if (placedOnCanvas(latestPlacement.current, draft, currentElements())) return;
    // 変換は非同期。**押した時点で弾かないと、2 回目が同じ `draftOrigin` を
    // 得て、同じ図が同じ場所に重なる。** 取り消しで戻すしかなくなる。
    if (!placing.enter()) return;

    try {
      const converted = await mermaidToElements(draft.mermaid);
      if (!converted.ok) {
        if (converted.reason === "syntax") {
          // 直せる失敗。会話の次の 1 往復にして投げ直す。
          // **利用者が打った指示ではない**ので、そう印を付けて積む。画面には
          // 固定文で出る（`turnLabel`）。
          void generate(conversionRetryPrompt(converted.detail), true);
          return;
        }
        // 置ける形にならない種類だった。投げ直しても同じものが返るので、
        // 種類を変えてもらう（ADR 0040）。
        setChat((prev) => failTurn(prev, diagramNotPlaceableFailure()));
        return;
      }

      const placed = placeElements(converted.elements);
      if (placed === null) return;
      // 覚えるのは押したときのドラフト。変換を待つあいだに次のドラフトが届いても、
      // そちらはまだ置いていない。
      const next = { draft, ids: placed.map((el) => el.id) };
      latestPlacement.current = next;
      setPlacement(next);
    } finally {
      placing.leave();
    }
  }, [api, chat.draft, currentElements, generate, placeElements, placing]);

  /**
   * 貼られた mermaid を変換して置く（ADR 0062）。
   *
   * 置き方は図のドラフトと同じ（`placeElements`）。違うのは**失敗したときに
   * 頼み直す相手がいない**ことで、構文エラーも種類違いも、理由を返して貼った
   * 人に直してもらう。
   *
   * **LLM を通さないので `capabilities` を見ない。** 止めると LLM を設定して
   * いない人が使えなくなる。
   */
  const pasteMermaid = useCallback(
    async (text: string): Promise<PasteOutcome> => {
      if (!api || !placing.enter()) return { placed: false, failure: null };

      try {
        const converted = await pasteToElements(text);
        if (!converted.ok) {
          // 変換器が返した理由は console にも残す。画面に畳んで出すのは
          // 構文エラーのときだけ（`mermaidPasteFailure`）。
          if (converted.reason === "syntax" || converted.reason === "unsupported") {
            log.warn("貼られた mermaid を置けませんでした", converted.detail);
          }
          return {
            placed: false,
            failure: mermaidPasteFailure(converted.reason, converted.detail),
          };
        }

        placeElements(converted.elements);
        // 置けたら入力を消す。残すと同じ図を 2 度置きやすい。**送った文字列の
        // ままのときだけ。** 変換を待つあいだにパネルを閉じて開き直すと入力を
        // 書き換えられるので、無条件に消すと新しい入力を捨てる。
        setPasteText((current) => (current === text ? "" : current));
        return { placed: true };
      } finally {
        placing.leave();
      }
    },
    [api, placeElements, placing],
  );

  // 描くたびに読み直す。BoardPage は Excalidraw の `onChange` のたびに描き直すので、
  // 取り消しや削除で消えたことにもここで追いつく（`annotationIdsOnCanvas` と同じ）。
  const draftPlaced = placedOnCanvas(placement, chat.draft, currentElements());

  return useMemo(
    () => ({
      chat,
      prompt,
      setPrompt,
      generate,
      changeKind,
      placeDraft,
      draftPlaced,
      pasteText,
      setPasteText,
      pasteMermaid,
    }),
    [
      chat,
      prompt,
      generate,
      changeKind,
      placeDraft,
      draftPlaced,
      pasteText,
      pasteMermaid,
    ],
  );
}
