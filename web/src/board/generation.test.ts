import { describe, expect, it } from "vitest";

import { createGenerations } from "./generation";

describe("createGenerations", () => {
  it("開始した世代が最新になる", () => {
    const g = createGenerations();

    const gen = g.start("a");
    expect(g.isCurrent("a", gen)).toBe(true);
  });

  // 連打すると応答が追い越すことがある。古い方を採用してはいけない。
  it("開始し直すと前の世代は古くなる", () => {
    const g = createGenerations();

    const first = g.start("a");
    const second = g.start("a");

    expect(g.isCurrent("a", first)).toBe(false);
    expect(g.isCurrent("a", second)).toBe(true);
  });

  it("キーごとに独立している", () => {
    const g = createGenerations();

    const a = g.start("a");
    g.start("b");
    g.start("b");

    expect(g.isCurrent("a", a)).toBe(true);
  });

  // 保存すると解釈の対象が変わる。実行中のものも無効にする必要がある。
  it("invalidateAll で実行中のものがすべて古くなる", () => {
    const g = createGenerations();

    const a = g.start("a");
    const b = g.start("b");
    g.invalidateAll();

    expect(g.isCurrent("a", a)).toBe(false);
    expect(g.isCurrent("b", b)).toBe(false);
  });

  it("invalidateAll のあとに開始したものは最新になる", () => {
    const g = createGenerations();

    g.start("a");
    g.invalidateAll();
    const next = g.start("a");

    expect(g.isCurrent("a", next)).toBe(true);
  });

  it("一度も開始していないキーは最新ではない", () => {
    const g = createGenerations();

    expect(g.isCurrent("a", 1)).toBe(false);
    expect(g.isCurrent("a", 0)).toBe(false);
  });

  it("何も開始していなくても invalidateAll は安全", () => {
    const g = createGenerations();

    expect(() => g.invalidateAll()).not.toThrow();
  });
});
