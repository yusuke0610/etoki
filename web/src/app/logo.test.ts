import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

/**
 * ファビコン（ADR 0084）が、配っていないものに頼っていないこと。
 *
 * **回帰止め。切れると何が起きるか。** ファビコンはページの外で描かれるので、
 * SVG の中で書体を名指しした字は見る人の手元の書体で描かれ、外を指す参照は
 * 読まれずに欠ける。どちらも失敗にならず、絵が黙って崩れる。
 *
 * バンドラの解決を挟まず fs で読むのは、中身の文字列を見たいから。vitest は
 * web/ を root に走るので、そこからの相対で解く。
 */
const FAVICON = "src/app/favicon.svg";

function read(path: string): string {
  return readFileSync(resolve(process.cwd(), path), "utf8");
}

describe("favicon.svg", () => {
  const svg = read(FAVICON);

  /**
   * **回帰止め。** 壊れた SVG は失敗を出さず、タブが既定の絵に戻るだけ。
   * XML のコメントに `--` を書いただけでも壊れる（変数の名前を書きがち）。
   */
  it("XML として読める", () => {
    const doc = new DOMParser().parseFromString(svg, "image/svg+xml");

    expect(doc.getElementsByTagName("parsererror")).toHaveLength(0);
    expect(doc.documentElement.tagName).toBe("svg");
  });

  it("字を書体で描かず、輪郭で持つ", () => {
    expect(svg).not.toMatch(/<text[\s>]/);
    expect(svg).not.toMatch(/font-family|@font-face|@import/);
  });

  it("参照するのは自分の中の定義だけ", () => {
    const urls = [...svg.matchAll(/url\(\s*['"]?([^'")\s]*)/g)].map((m) => m[1]);
    const hrefs = [...svg.matchAll(/\bhref\s*=\s*["']([^"']*)["']/g)].map((m) => m[1]);

    for (const ref of [...urls, ...hrefs]) expect(ref).toMatch(/^#/);
  });

  /**
   * **回帰止め。** 地の緑はロゴの付箋（`--brand-note`）と同じ値を、ここに写して
   * 持つ（ページの変数を読めないため）。片方だけ変えると、タブとページで印の色が
   * 割れる。どちらの値も推測せず、両方のファイルから引いて比べる。
   */
  it("地の緑は、ロゴの付箋の変数と同じ値", () => {
    const token = /--brand-note:\s*(#[0-9a-fA-F]{6})\s*;/.exec(
      read("src/app/tokens.css"),
    )?.[1];
    const ground = /<rect\b[^>]*\bfill="(#[0-9a-fA-F]{6})"/.exec(svg)?.[1];

    expect(token).toMatch(/^#/);
    expect(ground?.toLowerCase()).toBe(token?.toLowerCase());
  });
});

/**
 * **回帰止め。** `index.html` の `<link rel="icon">` が指す先が無くても、Vite の
 * ビルドは警告を出して href をそのまま残すだけで落ちない。タブの絵が既定に
 * 戻っても誰も気づかないので、ファイルを動かしたら追随していることをここで見る。
 */
it("index.html のファビコンが、上の検査を掛けた SVG を指す", () => {
  const href = /<link rel="icon"[^>]*href="([^"]+)"/.exec(read("index.html"))?.[1] ?? "";

  expect(href).toBe(`/${FAVICON}`);
  expect(existsSync(resolve(process.cwd(), `.${href}`))).toBe(true);
});
