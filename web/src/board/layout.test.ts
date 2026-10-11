import { describe, expect, it } from "vitest";

import { boardLayout, isMobileBreakpoint } from "./layout";

const PANEL = 320;

describe("isMobileBreakpoint", () => {
  // Excalidraw 0.18.1 の式。幅 730 未満、または高さ 500 未満かつ幅 1000 未満。
  it.each([
    [729, 900, true],
    [730, 900, false],
    [999, 499, true],
    [999, 500, false],
    [1000, 499, false],
  ])("%i × %i は %s", (width, height, mobile) => {
    expect(isMobileBreakpoint(width, height)).toBe(mobile);
  });
});

describe("boardLayout", () => {
  // 並べる・重ねるの境目。パネルの幅を引いた残りが 730 に届くか。
  it.each([
    [1049, 900, "overlay"],
    [1050, 900, "side"],
  ])("%i × %i は %s（パネルを引いた幅で決まる）", (width, height, layout) => {
    expect(boardLayout(width, height, PANEL)).toBe(layout);
  });

  // 重ねる・スマホの境目。幅 730〜999 では高さ 500 で分かれる。
  it.each([
    [900, 499, "phone"],
    [900, 500, "overlay"],
  ])("%i × %i は %s（高さで決まる）", (width, height, layout) => {
    expect(boardLayout(width, height, PANEL)).toBe(layout);
  });

  // 重ねる・スマホの境目。幅だけで決まる側。
  it.each([
    [729, 900, "phone"],
    [730, 900, "overlay"],
  ])("%i × %i は %s（幅で決まる）", (width, height, layout) => {
    expect(boardLayout(width, height, PANEL)).toBe(layout);
  });

  // 低い画面で並べるには、引いた残りが 1000 に届く必要がある。
  it.each([
    [1319, 499, "overlay"],
    [1320, 499, "side"],
  ])("%i × %i は %s（低い画面）", (width, height, layout) => {
    expect(boardLayout(width, height, PANEL)).toBe(layout);
  });

  // 撮影と E2E が使う大きさ。issue の表のとおりになっていること。
  it.each([
    [1440, 900, "side"],
    [1280, 720, "side"],
    [1024, 768, "overlay"],
    [375, 812, "phone"],
    [812, 375, "phone"],
  ])("%i × %i は %s", (width, height, layout) => {
    expect(boardLayout(width, height, PANEL)).toBe(layout);
  });

  // パネルの幅が読めなければ押し縮めない。並べてしまうと、狭い画面で
  // キャンバスだけがモバイル用 UI に切り替わりうる。
  it("パネルの幅が読めなければ並べない", () => {
    expect(boardLayout(1440, 900, null)).toBe("overlay");
    expect(boardLayout(375, 812, null)).toBe("phone");
  });

  // 並べる幅は、パネルの幅から引く。決め打ちの境目を持たない。
  it("並べる境目はパネルの幅で動く", () => {
    expect(boardLayout(1000, 900, 270)).toBe("side");
    expect(boardLayout(1000, 900, 271)).toBe("overlay");
  });
});
