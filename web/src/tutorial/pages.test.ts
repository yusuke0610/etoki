import { describe, expect, it } from "vitest";

import { TUTORIAL_PAGES } from "./pages";

/** ページの文言。見出しと本文をつなげて見る。 */
function textOf(index: number): string {
  const page = TUTORIAL_PAGES[index];
  if (page === undefined) throw new Error(`${index + 1} ページ目が無い`);
  return `${page.heading}\n${page.body}`;
}

describe("TUTORIAL_PAGES", () => {
  it("描く・絵解く・作るの 3 ページ", () => {
    expect(TUTORIAL_PAGES).toHaveLength(3);
  });

  // **構造の語彙を前倒ししない**（中核思想 1、ADR 0081）。使い方が最初に
  // epic / issue を教えると、ブレストの前に構造を意識させることになる。
  it("1・2 ページ目には epic も issue も出ない", () => {
    for (const index of [0, 1]) {
      expect(textOf(index), `${index + 1} ページ目`).not.toMatch(/epic|issue/i);
    }
  });

  // 出さないだけだと、3 ページ目からも消した実装が通る。作るものの説明は
  // ここで初めて出す。
  it("3 ページ目で初めて epic と issue が出る", () => {
    expect(textOf(2)).toMatch(/epic/);
    expect(textOf(2)).toMatch(/issue/);
  });

  // フィールド名は ETOKI_GITHUB_KIND_FIELD / ETOKI_GITHUB_PARENT_FIELD で
  // 変えられる。名前を言い切ると、設定を変えた人には嘘になる（ADR 0081）。
  it("Project のフィールド名は既定の名前として書く", () => {
    expect(textOf(2)).toMatch(/既定の名前[^。]*Kind/);
    expect(textOf(2)).toMatch(/既定の名前[^。]*Parent/);
  });
});
