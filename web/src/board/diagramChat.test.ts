import { describe, expect, it } from "vitest";

import type { DiagramDraft } from "../api/types";
import {
  beginTurn,
  canSend,
  changeKind,
  completeTurn,
  conversionRetryPrompt,
  failTurn,
  historyOf,
  startChat,
  turnLabel,
  turnsRemaining,
} from "./diagramChat";

const draft = (mermaid: string, turnsRemaining = 9): DiagramDraft => ({
  kind: "todo",
  mermaid,
  turnsRemaining,
});

const failure = { message: "生成できませんでした", detail: "" };

describe("会話を積む", () => {
  it("生成できた往復だけを積む", () => {
    let chat = startChat("todo");
    chat = beginTurn(chat, "注文の流れ");
    chat = completeTurn(chat, draft("flowchart TD\n  A --> B"));

    expect(chat.turns).toEqual([
      { prompt: "注文の流れ", mermaid: "flowchart TD\n  A --> B", internal: false },
    ]);
    expect(chat.pending).toBeNull();
  });

  // 失敗した往復を積むと、返ってこなかった指示を「返した図」つきで次に送る
  // ことになる。
  it("失敗した往復は積まない", () => {
    let chat = startChat("todo");
    chat = beginTurn(chat, "注文の流れ");
    chat = failTurn(chat, failure);

    expect(chat.turns).toEqual([]);
    expect(chat.pending).toBeNull();
    expect(chat.failure).toEqual(failure);
  });

  // 引き直しの失敗で前の結果まで消えると、やり直せば済むはずの失敗が
  // 取り返しのつかないものになる。
  it("失敗しても直前のドラフトは消さない", () => {
    let chat = startChat("todo");
    chat = completeTurn(beginTurn(chat, "1 回目"), draft("flowchart TD\n  A --> B"));
    const before = chat.draft;

    chat = failTurn(beginTurn(chat, "2 回目"), failure);

    expect(chat.draft).toBe(before);
  });

  it("送信を始めると前の失敗は消える", () => {
    let chat = failTurn(beginTurn(startChat("todo"), "1 回目"), failure);
    chat = beginTurn(chat, "2 回目");

    expect(chat.failure).toBeNull();
    expect(chat.pending?.prompt).toBe("2 回目");
  });

  // 送っていない指示が会話に混ざらないこと。世代の照合をすり抜けた応答が
  // 来ても、空の指示を積んで次の送信を壊さない。
  it("送っていない応答は積まない", () => {
    const chat = completeTurn(startChat("todo"), draft("flowchart TD\n  A --> B"));

    expect(chat.turns).toEqual([]);
    expect(chat.draft).not.toBeNull();
  });
});

describe("種類を変える", () => {
  // 積み上げた指示は前の種類の図に対するもの。引き継ぐと、シーケンス図への
  // 指示をフローチャートの続きとして送ることになる。
  it("会話ごと捨てる", () => {
    let chat = startChat("todo");
    chat = completeTurn(beginTurn(chat, "注文の流れ"), draft("flowchart TD\n  A --> B"));

    const next = changeKind(chat, "sequence");

    expect(next.kind).toBe("sequence");
    expect(next.turns).toEqual([]);
    expect(next.draft).toBeNull();
  });

  it("同じ種類なら何もしない", () => {
    const chat = completeTurn(
      beginTurn(startChat("todo"), "注文の流れ"),
      draft("flowchart TD\n  A --> B"),
    );

    expect(changeKind(chat, "todo")).toBe(chat);
  });
});

describe("canSend", () => {
  it("空白だけの指示は送らない", () => {
    const chat = startChat("todo");

    expect(canSend(chat, "")).toBe(false);
    expect(canSend(chat, " \n ")).toBe(false);
    expect(canSend(chat, "注文の流れ")).toBe(true);
  });

  // 待っているあいだに二重送信すると、同じ会話に対して 2 つの応答が返る。
  it("待っているあいだは送らない", () => {
    const chat = beginTurn(startChat("todo"), "注文の流れ");

    expect(canSend(chat, "もう一度")).toBe(false);
  });
});

describe("turnsRemaining", () => {
  // 数えるのはサーバー。**同じ判定を 2 つ持たない**ので、返ってきた値を
  // そのまま見せる。ここで数え直すと、上限を変えたときに片方だけ古くなる。
  it("サーバーが返した残りをそのまま返す", () => {
    const chat = completeTurn(
      beginTurn(startChat("todo"), "注文の流れ"),
      draft("flowchart TD\n  A --> B", 4),
    );

    expect(turnsRemaining(chat)).toBe(4);
  });

  it("まだ生成していなければ分からない", () => {
    expect(turnsRemaining(startChat("todo"))).toBeNull();
  });
});

// 投げ直しの文言は etoki が組み立てたもので、変換器の英語のメッセージを含む。
// **利用者の指示と同じ列で扱うと、書いた覚えのない英文が自分の指示として出る。**
describe("内部の投げ直しを見分ける", () => {
  const retry = (chat = startChat("todo")) =>
    completeTurn(
      beginTurn(chat, conversionRetryPrompt("Parse error on line 2"), true),
      draft("flowchart TD\n  A --> B"),
    );

  it("積んだ往復に印が残る", () => {
    const chat = retry();

    expect(chat.turns[0]?.internal).toBe(true);
  });

  it("利用者が打った指示には印を付けない", () => {
    const chat = completeTurn(
      beginTurn(startChat("todo"), "注文の流れ"),
      draft("flowchart TD\n  A --> B"),
    );

    expect(chat.turns[0]?.internal).toBe(false);
  });

  // 変換器のメッセージは画面に出さない（`web/CLAUDE.md`）。
  it("画面には固定文を出す", () => {
    const chat = retry();
    const label = turnLabel(chat.turns[0]!);

    expect(label).not.toContain("Parse error on line 2");
    expect(label).not.toBe(chat.turns[0]!.prompt);
  });

  it("利用者の指示はそのまま出す", () => {
    const chat = completeTurn(
      beginTurn(startChat("todo"), "注文の流れ"),
      draft("flowchart TD\n  A --> B"),
    );

    expect(turnLabel(chat.turns[0]!)).toBe("注文の流れ");
  });

  // 印は画面のためだけにある。契約にあるのは prompt と mermaid の 2 つだけ
  // （ADR 0011）。載せたまま送ると、契約に無いキーが混ざる。
  it("サーバーには印を送らない。文言は投げ直しのものを送る", () => {
    const chat = retry();

    expect(historyOf(chat)).toEqual([
      {
        prompt: conversionRetryPrompt("Parse error on line 2"),
        mermaid: "flowchart TD\n  A --> B",
      },
    ]);
  });
});

describe("conversionRetryPrompt", () => {
  // 構文エラーは会話の次の 1 往復として投げ直す（ADR 0041）。指摘を載せないと、
  // モデルは同じ出力を返すだけになる。
  it("変換器の指摘を載せて直させる", () => {
    const prompt = conversionRetryPrompt("Parse error on line 2");

    expect(prompt).toContain("Parse error on line 2");
    expect(prompt).toContain("mermaid");
  });
});

// 種類を変えたあとに古い応答が届いても、いまの会話に積まれてはならない。
// **積むかどうかを決めるのは世代の照合**（BoardPage）で、ここが確かめるのは
// 「捨て損ねた応答が届いたら何が起きるか」のほう。
describe("種類を変えたあとに古い応答が届く", () => {
  it("捨て損ねると、前の記法の図が土台として載ってしまう", () => {
    let chat = beginTurn(startChat("todo"), "注文の流れ");
    chat = changeKind(chat, "sequence");

    // 世代の照合をすり抜けた応答。**指示は積まれない**（pending は捨てられて
    // いる）が、図は載る。だから BoardPage 側で世代を進める必要がある。
    const leaked = completeTurn(chat, draft("flowchart TD\n  A --> B"));

    expect(leaked.turns).toEqual([]);
    expect(leaked.draft?.mermaid).toContain("flowchart");
  });
});
