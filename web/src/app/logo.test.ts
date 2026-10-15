import { existsSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

/**
 * ロゴの SVG（ADR 0082）が、配っていないものに頼っていないこと。
 *
 * **回帰止め。切れると何が起きるか。** ロゴは `<img>` で置くので、SVG の中で
 * 書体を名指しした字は見る人の手元の書体で描かれ、外を指す参照は読まれずに
 * 欠ける。どちらも失敗にならず、絵が黙って崩れる。案（`<text>` で字を置いて
 * いる）から書き出し直すときに、そのまま持ち込むのがいちばん起きやすい。
 *
 * バンドラの解決を挟まず fs で読むのは、中身の文字列を見たいから。vitest は
 * web/ を root に走るので、そこからの相対で解く。
 */
const LOGO_FILES = ["logo.svg", "logoMark.svg", "favicon.svg"];

function read(path: string): string {
  return readFileSync(resolve(process.cwd(), path), "utf8");
}

describe.each(LOGO_FILES)("%s", (name) => {
  const svg = read(`src/app/${name}`);

  it("字を書体で描かず、輪郭で持つ", () => {
    expect(svg).not.toMatch(/<text[\s>]/);
    expect(svg).not.toMatch(/font-family|@font-face|@import/);
  });

  it("参照するのは自分の中の定義だけ", () => {
    const urls = [...svg.matchAll(/url\(\s*['"]?([^'")\s]*)/g)].map((m) => m[1]);
    const hrefs = [...svg.matchAll(/\bhref\s*=\s*["']([^"']*)["']/g)].map((m) => m[1]);

    for (const ref of [...urls, ...hrefs]) expect(ref).toMatch(/^#/);
  });
});

/**
 * **回帰止め。** `index.html` の `<link rel="icon">` が指す先が無くても、Vite の
 * ビルドは警告を出して href をそのまま残すだけで落ちない。タブの絵が既定に
 * 戻っても誰も気づかないので、ファイルを動かしたら追随していることをここで見る。
 */
it("index.html のファビコンが、実在するロゴの SVG を指す", () => {
  const href = /<link rel="icon"[^>]*href="([^"]+)"/.exec(read("index.html"))?.[1] ?? "";

  // 上の検査が掛かっているファイルのどれかであること。
  expect(LOGO_FILES.map((f) => `/src/app/${f}`)).toContain(href);
  expect(existsSync(resolve(process.cwd(), `.${href}`))).toBe(true);
});
