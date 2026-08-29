import type { Failure } from "../api/errorMessage";
import type { DiagramDraft, DiagramKind, DiagramTurn } from "../api/types";

/**
 * 図のドラフトを作るチャットの状態。
 *
 * **フロントのメモリだけで持つ**（ADR 0041）。保存すると「誰の会話か」
 * 「他のメンバーに見えるか」が立ち上がる。**会話は成果物ではなく、成果物は
 * キャンバス。** ボードを切り替えたら捨てる（`BoardPage` は `key={board.id}`
 * で作り直されるので、持ち越されない）。
 */
export type DiagramChat = {
  /** 何の図を描かせるか。会話の途中では変えない（下の `changeKind` を参照）。 */
  kind: DiagramKind;
  /**
   * 成立したやりとり。**サーバーに毎回まるごと送る。**
   *
   * 失敗した往復は入れない。入れると、返ってこなかった指示を「返した図」
   * つきで送ることになる。
   */
  turns: DiagramTurn[];
  /** 送信中の指示。null なら待っていない。 */
  pending: string | null;
  /** いちばん新しいドラフト。null ならまだ 1 つも生成していない。 */
  draft: DiagramDraft | null;
  /** 直前の失敗。次の送信で消える。 */
  failure: Failure | null;
};

/** 種類を選んだだけの、まだ何も送っていない会話。 */
export function startChat(kind: DiagramKind): DiagramChat {
  return { kind, turns: [], pending: null, draft: null, failure: null };
}

/**
 * 図の種類を変える。**会話ごと捨てる。**
 *
 * 積み上げた指示は前の種類の図に対するもので、記法が変われば土台にできない。
 * 引き継ぐと、シーケンス図への指示をフローチャートの続きとして送ることになる。
 */
export function changeKind(chat: DiagramChat, kind: DiagramKind): DiagramChat {
  if (kind === chat.kind) return chat;
  return startChat(kind);
}

/** 送信を始める。**失敗はここで消す。** 前の失敗が結果の隣に残らないように。 */
export function beginTurn(chat: DiagramChat, prompt: string): DiagramChat {
  return { ...chat, pending: prompt, failure: null };
}

/**
 * 生成できた。やりとりを 1 往復ぶん積む。
 *
 * `pending` が空なら積まない。応答が遅れて届いたときに、送っていない指示が
 * 会話に混ざるのを防ぐ（照合そのものは呼び出し側の世代が行う）。
 */
export function completeTurn(chat: DiagramChat, draft: DiagramDraft): DiagramChat {
  const turns =
    chat.pending === null
      ? chat.turns
      : [...chat.turns, { prompt: chat.pending, mermaid: draft.mermaid }];

  return { ...chat, turns, pending: null, draft, failure: null };
}

/**
 * 生成に失敗した。**直前のドラフトは消さない。**
 *
 * 引き直しの失敗で前の結果まで消えると、やり直せば済むはずの失敗が取り返しの
 * つかないものになる（引いた解釈の履歴と同じ扱い、`web/CLAUDE.md`）。
 */
export function failTurn(chat: DiagramChat, failure: Failure): DiagramChat {
  return { ...chat, pending: null, failure };
}

/** 送信できるか。空白だけの指示は送らない（サーバーも 400 で弾く）。 */
export function canSend(chat: DiagramChat, prompt: string): boolean {
  return chat.pending === null && prompt.trim() !== "";
}

/**
 * あと何往復できるか。まだ 1 度も生成していなければ null。
 *
 * **数えるのはサーバー。** 上限を持つのはあちらなので、こちらは返ってきた値を
 * そのまま見せる。同じ判定を 2 つ持つと、片方だけ変えたときに「画面は送れると
 * 言うがサーバーが弾く」がどちらの言い分か分からなくなる（ADR 0038 と同じ）。
 */
export function turnsRemaining(chat: DiagramChat): number | null {
  return chat.draft?.turnsRemaining ?? null;
}

/**
 * 変換に失敗した図を直させるための、次の指示。
 *
 * **構文エラーは会話の次の 1 往復として投げ直す**（ADR 0041）。mermaid として
 * 読めるかではなく Excalidraw の要素として置けるかを知っているのは変換器だけ
 * なので、投げ直せるのは変換を試したここしかない。
 *
 * `detail` は変換器が返した英語のメッセージ。**画面には出さず、モデルにだけ
 * 渡す**（`web/CLAUDE.md` の「例外の中身は画面に出さない」）。
 */
export function conversionRetryPrompt(detail: string): string {
  return (
    "直前の図は変換できませんでした。mermaid の構文を直して、" +
    "同じ内容の図をもう一度出力してください。\n" +
    `変換器の指摘: ${detail}`
  );
}
