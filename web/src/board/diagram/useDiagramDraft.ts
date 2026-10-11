import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useMemo, useState } from "react";

import { boardsApi } from "../../api/boards";
import {
  describeFailure,
  diagramNotPlaceableFailure,
  mermaidPasteFailure,
} from "../../api/errorMessage";
import type { DiagramKind } from "../../api/types";
import { log } from "../../app/logger";
import type { SceneElement } from "../../excalidraw/annotation";
import { draftOrigin, mermaidToElements, moveDraft } from "../../excalidraw/mermaid";
import { pasteToElements } from "../../excalidraw/mermaidPaste";
import { useReentryGuard } from "../exclusion";
import { createGenerations } from "../generation";
import {
  beginTurn,
  changeKind as changeChatKind,
  completeTurn,
  conversionRetryPrompt,
  failTurn,
  historyOf,
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
};

export type DiagramDrafting = {
  /** 図のドラフトのチャット。**フロントのメモリだけ**（ADR 0041）。 */
  chat: DiagramChat;
  /** プロンプトから図のドラフトを生成する。成立したら true。 */
  generate: (prompt: string, internal?: boolean) => Promise<boolean>;
  /** 図の種類を変える。変えたら会話ごと捨てる。 */
  changeKind: (kind: DiagramKind) => void;
  /** いまのドラフトをキャンバスに置く。 */
  placeDraft: () => Promise<void>;
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
 */
export function useDiagramDraft({
  api,
  boardId,
  currentElements,
  updateElements,
  onPlaced,
}: Options): DiagramDrafting {
  const [chat, setChat] = useState<DiagramChat>(() => startChat("todo"));
  // 生成の世代。**保存では無効にしない。** 生成は保存済みシーンを読まないので、
  // 保存しても前提が変わらない（解釈との非対称、ADR 0041）。
  const [generations] = useState(createGenerations);
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
    async (prompt: string, internal = false): Promise<boolean> => {
      const generation = generations.start(DIAGRAM_KEY);
      // 送るのはいまの会話。**成立した往復だけがここに積まれている**ので、
      // 失敗した指示を「返した図」つきで送ることにはならない。
      const { kind } = chat;
      const history = historyOf(chat);
      setChat((prev) => beginTurn(prev, prompt, internal));

      try {
        const draft = await boardsApi.generateDiagram(boardId, kind, prompt, history);
        // 遅れて届いた応答を今の会話に混ぜない。種類を変えると会話ごと
        // 捨てるので、そのあとに古い図が積まれると土台が食い違う。
        if (!generations.isCurrent(DIAGRAM_KEY, generation)) return false;
        setChat((prev) => completeTurn(prev, draft));
        return true;
      } catch (e) {
        if (!generations.isCurrent(DIAGRAM_KEY, generation)) return false;
        // **パネルの中に出す。** 会話の続きで直せる失敗なので、画面上部の
        // 通知に出すと、直す場所と理由が離れる。
        setChat((prev) => failTurn(prev, describeFailure("生成できませんでした", e)));
        return false;
      }
    },
    [boardId, chat, generations],
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

      generations.start(DIAGRAM_KEY);
      setChat(next);
    },
    [chat, generations],
  );

  /**
   * 変換した要素をキャンバスに置く。図のドラフトと貼り付けで共有する。
   *
   * **既存の要素には一切触らない。追加するだけ**（#58 の原則）。置き場所は
   * 既存の絵の右外で、重ねない（ADR 0040）。**保存はしない。**
   */
  const placeElements = useCallback(
    (elements: readonly SceneElement[]) => {
      if (!api) return;
      const existing = currentElements();
      const placed = moveDraft(elements, draftOrigin(existing));
      updateElements([...existing, ...placed]);

      // 置いた先へ寄せる。既存の絵の外に置くので、寄せないと押したのに何も
      // 起きていないように見える（ADR 0040）。
      api.scrollToContent(placed as never, { fitToContent: true, animate: true });
      onPlaced?.();
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
   */
  const placeDraft = useCallback(async () => {
    // 変換は非同期。**押した時点で弾かないと、2 回目が同じ `draftOrigin` を
    // 得て、同じ図が同じ場所に重なる。** 取り消しで戻すしかなくなる。
    if (!api || chat.draft === null || !placing.enter()) return;

    try {
      const converted = await mermaidToElements(chat.draft.mermaid);
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

      placeElements(converted.elements);
    } finally {
      placing.leave();
    }
  }, [api, chat.draft, generate, placeElements, placing]);

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

  return useMemo(
    () => ({
      chat,
      generate,
      changeKind,
      placeDraft,
      pasteText,
      setPasteText,
      pasteMermaid,
    }),
    [chat, generate, changeKind, placeDraft, pasteText, pasteMermaid],
  );
}
