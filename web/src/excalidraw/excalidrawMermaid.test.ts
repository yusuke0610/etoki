import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

import { describe, expect, it } from "vitest";

import { EXCALIDRAW_MERMAID_KINDS, isMaybeMermaidDefinition } from "./excalidrawMermaid";

describe("isMaybeMermaidDefinition", () => {
  // Excalidraw が貼り付けで変換に回す形。どれも止めないと守りなしで描かれる。
  it.each([
    ["受け付けない種類", "gantt\n title T\n section A\n task :a1, 2024-01-01, 30d"],
    ["-beta 付き", "xychart-beta\n x-axis [a, b]"],
    ["受け付ける種類でも", "flowchart TD\n A --> B"],
    ["設定の前置き", '%%{init: {"theme": "dark"}}%%\nsequenceDiagram\n A->>B: hi'],
    ["前後の空白", "\n\n  graph LR\n A --> B\n"],
  ])("%s は当たる", (_, text) => {
    expect(isMaybeMermaidDefinition(text)).toBe(true);
  });

  // 当たらないものは Excalidraw の既定どおりテキストとして貼られる。広げると
  // 普通の貼り付けまで止めてしまう。
  it.each([
    ["語の途中", "graphql のスキーマを見直す"],
    ["普通の文", "ログイン画面の文言を直す"],
    ["大文字", "Gantt で見る"],
    ["空", ""],
  ])("%s は当たらない", (_, text) => {
    expect(isMaybeMermaidDefinition(text)).toBe(false);
  });
});

function librarySource(): string {
  const require = createRequire(import.meta.url);
  // 読むのは dev 版。prod 版は変数名が縮められていて探せない。
  const entry = require
    .resolve("@excalidraw/excalidraw")
    .replace(/prod[/\\]index\.js$/, "dev/index.js");
  return readFileSync(entry, "utf8");
}

/**
 * 写しの元と種類の一覧が揃っていることを固定する（ADR 0067）。
 *
 * 元の `isMaybeMermaidDefinition` は export されていないので、配布物の本文から
 * 一覧を抜き出して比べる。**抜き出せなくなったときも落とす。** ライブラリを
 * 上げて形が変わったなら、写しを見直す合図になる。
 */
describe("Excalidraw の前提", () => {
  it("貼り付けで変換に回す種類の一覧が写しと同じ", () => {
    const source = librarySource();

    const body =
      /var isMaybeMermaidDefinition = [\s\S]*?const chartTypes = \[([\s\S]*?)\];/.exec(
        source,
      )?.[1];
    expect(body, "isMaybeMermaidDefinition が見つからない").toBeDefined();

    const kinds = [...(body ?? "").matchAll(/"([^"]+)"/g)].map((m) => m[1]);
    expect(kinds).toEqual([...EXCALIDRAW_MERMAID_KINDS]);
  });

  // 一覧が同じでも、組み立て方（前置き・大文字小文字・前後の扱い）が変われば
  // 当たる範囲が変わる。
  it("判定の組み立て方が写しと同じ", () => {
    const source = librarySource();

    const fn =
      /var isMaybeMermaidDefinition = [\s\S]*?const re = ([\s\S]*?);\s*return ([^;]*);/.exec(
        source,
      );
    expect(fn, "isMaybeMermaidDefinition が見つからない").not.toBeNull();
    expect(fn?.[1]?.replace(/\s+/g, "")).toBe(
      'newRegExp(`^(?:%%{.*?}%%[\\\\s\\\\n]*)?\\\\b(?:${chartTypes.map((x)=>`\\\\s*${x}(-beta)?`).join("|")})\\\\b`)',
    );
    expect(fn?.[2]).toBe("re.test(text.trim())");
  });
});
