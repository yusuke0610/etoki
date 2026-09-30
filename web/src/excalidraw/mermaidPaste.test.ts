import { describe, expect, it, vi } from "vitest";

import { MERMAID_MAX_TEXT_SIZE, type MermaidParser } from "./mermaid";
import {
  canPaste,
  isAcceptedKind,
  pasteToElements,
  stripComments,
  unwrapFence,
} from "./mermaidPaste";

/**
 * 変換器の代わり。**呼ばれたかどうかを数える。**
 *
 * 前検査で拒んだものは mermaid に 1 度も渡らないことが守りたいもの
 * （ADR 0062）。描いただけでタブが固まる種類があるので、「変換してから
 * 拒む」実装では遅い。返り値の検査だけだと、その実装でも通ってしまう。
 */
const spyParser = () =>
  vi.fn<MermaidParser>(async () => ({ elements: [{ type: "rectangle" }] }));

describe("unwrapFence", () => {
  it.each([
    ["mermaid の印つき", "```mermaid\nerDiagram\n  A ||--o{ B : r\n```"],
    ["印なし", "```\nerDiagram\n  A ||--o{ B : r\n```"],
    ["前後の空白と CRLF", "  \r\n```mermaid\r\nerDiagram\r\n  A ||--o{ B : r\r\n```  \n"],
  ])("全体を囲むフェンスを剥がす（%s）", (_name, text) => {
    expect(unwrapFence(text).replace(/\r/g, "")).toBe("erDiagram\n  A ||--o{ B : r");
  });

  // 複数の図や前後の文を含む Markdown から、どれを置くかを etoki が選ばない。
  it.each([
    ["前に文がある", "説明\n```mermaid\nerDiagram\n```"],
    ["フェンスが 2 つ", "```mermaid\nerDiagram\n```\n```mermaid\nflowchart TD\n```"],
    ["閉じていない", "```mermaid\nerDiagram\n  A ||--o{ B : r"],
    ["フェンスが無い", "erDiagram\n  A ||--o{ B : r"],
  ])("全体を囲んでいなければ触らない（%s）", (_name, text) => {
    expect(unwrapFence(text)).toBe(text);
  });
});

describe("isAcceptedKind", () => {
  it.each([
    ["erDiagram", "erDiagram\n  A ||--o{ B : r"],
    ["sequenceDiagram", "sequenceDiagram\n  A->>B: hi"],
    ["flowchart", "flowchart TD\n  A --> B"],
    ["flowchart だけの行", "flowchart\n  A --> B"],
    ["graph", "graph LR\n  A --> B"],
    ["先頭の空行と字下げ", "\n\n   erDiagram\n  A ||--o{ B : r"],
    ["% コメント", "%% 注文まわり\nerDiagram\n  A ||--o{ B : r"],
    ["init の指示", '%%{init: {"theme": "dark"}}%%\nflowchart TD\n  A --> B'],
    ["複数行の init", '%%{init: {\n  "theme": "dark"\n}}%%\nflowchart TD\n  A --> B'],
    ["frontmatter", "---\ntitle: 注文\n---\nerDiagram\n  A ||--o{ B : r"],
  ])("受け付ける（%s）", (_name, definition) => {
    expect(isAcceptedKind(definition)).toBe(true);
  });

  // 描いただけで固まる種類（gantt / xychart / radar）と、プロトタイプ汚染の
  // ある種類（architecture）が入っている。state 図は HTML の注入が残る。
  it.each([
    ["gantt", "gantt\n  title x"],
    ["xychart", "xychart-beta\n  x-axis [a]"],
    ["radar", "radar-beta\n  axis a"],
    ["architecture", "architecture-beta\n  service a(server)[A]"],
    ["classDiagram", "classDiagram\n  class A"],
    ["stateDiagram", "stateDiagram-v2\n  [*] --> A"],
    ["mindmap", "mindmap\n  root"],
    // mermaid の検出は前方一致なので、これらも flowchart や er の仲間として拾う。
    // etoki が確かめていない描き方なので、語の境界で切る。
    ["flowchart-elk", "flowchart-elk TD\n  A --> B"],
    ["語の続き", "erDiagramX\n  A ||--o{ B : r"],
    ["コメントの中だけ", "%% erDiagram\ngantt\n  title x"],
    ["frontmatter の中だけ", "---\ntitle: erDiagram\n---\ngantt\n  title x"],
    ["空", ""],
  ])("拒む（%s）", (_name, definition) => {
    expect(isAcceptedKind(definition)).toBe(false);
  });
});

describe("stripComments", () => {
  // mermaid 11.13.0 の `detectType` が使う正規表現。etoki はこれを写さずに
  // 書き直した（2 乗の時間がかかる）ので、先頭の語が同じになることを見る。
  const mermaidStrip = (text: string) => text.replace(/\s*%%.*\n/gm, "\n");
  const headOf = (text: string) => /^\s*([^\s]+)/.exec(text)?.[1];

  it.each([
    ["コメント行", "%% x\nerDiagram"],
    ["行末のコメント", "erDiagram %% x\n  A"],
    ["語に続くコメント", "erDiagram%%x\n  A"],
    ["改行で終わらないコメント", "%% erDiagram"],
    ["CRLF", "%% x\r\nerDiagram"],
    ["CR の後ろのコメント", "a\r%% x\nerDiagram"],
    ["U+2028 の前のコメント", "%% x\u2028erDiagram\n"],
    ["空行を挟む", "\n\n  %% x\n\n  erDiagram"],
    ["% が 1 つ", "% x\nerDiagram"],
  ])("先頭の語が mermaid と同じになる（%s）", (_name, text) => {
    expect(headOf(stripComments(text))).toBe(headOf(mermaidStrip(text)));
  });

  // 手で並べた例の外も突き合わせる。取り違えると、etoki が erDiagram と
  // 見た文字列を mermaid が別の種類として描く抜け道になる。
  it("短い文字列を総当たりしても、先頭の語が mermaid と同じになる", () => {
    const alphabet = ["%", "\n", "\r", " ", "a", "\u2028"];
    const walk = (prefix: string, depth: number) => {
      expect(headOf(stripComments(prefix)), JSON.stringify(prefix)).toBe(
        headOf(mermaidStrip(prefix)),
      );
      if (depth === 0) return;
      for (const c of alphabet) walk(prefix + c, depth - 1);
    };
    walk("", 6);
  });

  // 上限いっぱいの 1 行でもメインスレッドを止めない。
  it.each([
    ["改行の無い %", "%".repeat(MERMAID_MAX_TEXT_SIZE)],
    ["改行の無い空白", " ".repeat(MERMAID_MAX_TEXT_SIZE)],
  ])("上限いっぱいでもすぐ終わる（%s）", (_name, text) => {
    const start = performance.now();
    stripComments(text);
    expect(performance.now() - start).toBeLessThan(100);
  });
});

describe("canPaste", () => {
  it("空白だけなら押させない", () => {
    expect(canPaste("")).toBe(false);
    expect(canPaste(" \n\t ")).toBe(false);
    expect(canPaste("erDiagram")).toBe(true);
  });
});

describe("pasteToElements", () => {
  it("受け付けた種類は、フェンスを剥がして変換器に渡す", async () => {
    const parse = spyParser();

    const got = await pasteToElements(
      "```mermaid\nerDiagram\n  A ||--o{ B : r\n```",
      parse,
    );

    expect(got.ok).toBe(true);
    expect(parse).toHaveBeenCalledTimes(1);
    expect(parse).toHaveBeenCalledWith("erDiagram\n  A ||--o{ B : r");
  });

  it("受け付けない種類は、変換器に渡さずに kind で返す", async () => {
    const parse = spyParser();

    const got = await pasteToElements("gantt\n  title x", parse);

    expect(got).toEqual({ ok: false, reason: "kind", detail: "" });
    expect(parse).not.toHaveBeenCalled();
  });

  // mermaid は上限を超えると投げずに「Maximum text size in diagram exceeded」の
  // 1 ノードにすり替える。前で拒まないと、それが置けたものとして置かれる。
  it("上限ちょうどは通し、1 字でも超えたら変換器に渡さずに tooLarge で返す", async () => {
    const head = "flowchart TD\n  A --> B\n";
    // 埋め草は改行で終えない。改行の無い長い `%%` 行は、mermaid の正規表現を
    // そのまま写すと 2 乗の時間がかかった（`stripComments`）。**戻すと、ここが
    // テストの時間上限で落ちる。**
    const atLimit = head + "%".repeat(MERMAID_MAX_TEXT_SIZE - head.length);
    expect(atLimit).toHaveLength(MERMAID_MAX_TEXT_SIZE);

    const parse = spyParser();
    expect((await pasteToElements(atLimit, parse)).ok).toBe(true);
    expect(parse).toHaveBeenCalledTimes(1);

    const over = spyParser();
    const got = await pasteToElements(atLimit + "%", over);
    expect(got).toEqual({ ok: false, reason: "tooLarge", detail: "" });
    expect(over).not.toHaveBeenCalled();
  });

  // 上限はフェンスを剥がしたあとの長さで見る。mermaid に渡るのはそちら。
  it("上限はフェンスを剥がしたあとの長さで比べる", async () => {
    const head = "flowchart TD\n  A --> B\n";
    const atLimit = head + "%".repeat(MERMAID_MAX_TEXT_SIZE - head.length);

    const got = await pasteToElements("```mermaid\n" + atLimit + "\n```", spyParser());

    expect(got.ok).toBe(true);
  });

  it("フェンスの中身が空なら empty で返す", async () => {
    const parse = spyParser();

    const got = await pasteToElements("```mermaid\n\n```", parse);

    expect(got).toEqual({ ok: false, reason: "empty", detail: "" });
    expect(parse).not.toHaveBeenCalled();
  });

  // 構文エラーは人に直してもらうしかないので、手掛かり（パーサの位置つきの
  // メッセージ）を落とさずに返す。
  it("変換器が投げたら syntax で、メッセージを添えて返す", async () => {
    const got = await pasteToElements("erDiagram\n  A ||--", async () => {
      throw Object.assign(new Error("Parse error on line 2"), { hash: { line: 2 } });
    });

    expect(got).toEqual({ ok: false, reason: "syntax", detail: "Parse error on line 2" });
  });

  // パーサ内部の例外の英文は、貼った人が直す手掛かりにならない。
  it("構文と無関係な例外は unsupported で返す", async () => {
    const got = await pasteToElements("erDiagram\n  A ||--o{ B : has", async () => {
      throw new TypeError("x is undefined");
    });

    expect(got.ok).toBe(false);
    if (got.ok) return;
    expect(got.reason).toBe("unsupported");
  });

  // 変換してから拒む門番（画像・frame、ADR 0040）も同じく効く。
  it("画像で返ったら unsupported で返す", async () => {
    const got = await pasteToElements("flowchart TD\n  A --> B", async () => ({
      elements: [{ type: "image" }],
    }));

    expect(got.ok).toBe(false);
    if (got.ok) return;
    expect(got.reason).toBe("unsupported");
  });
});
