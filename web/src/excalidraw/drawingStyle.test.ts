import { FONT_FAMILY } from "@excalidraw/excalidraw";
import { describe, expect, it } from "vitest";

import {
  CLEAN_FONT_FAMILY,
  CLEAN_ROUGHNESS,
  DRAWING_DEFAULTS,
  withCleanStyle,
} from "./drawingStyle";

describe("清書調の値", () => {
  // 線を揺らさない値と、日本語を手書き風の書体に落とさない書体。Excalifont に
  // 戻すと、日本語が Xiaolai で描かれて手書き風に戻る。
  it("揺れない線と、Excalifont 以外の書体", () => {
    expect(CLEAN_ROUGHNESS).toBe(0);
    expect(CLEAN_FONT_FAMILY).toBe(FONT_FAMILY.Nunito);
    expect(CLEAN_FONT_FAMILY).not.toBe(FONT_FAMILY.Excalifont);
  });

  // 人が描くときの既定。キーの綴りを間違えると Excalidraw は黙って無視し、
  // 手書き風のまま描き始める。
  it("人が描く既定は Excalidraw のツールの初期値のキーで渡す", () => {
    expect(DRAWING_DEFAULTS).toEqual({
      currentItemRoughness: 0,
      currentItemFontFamily: FONT_FAMILY.Nunito,
    });
  });
});

describe("withCleanStyle", () => {
  it("どの骨格も線を揺らさない", () => {
    const got = withCleanStyle([
      { type: "rectangle", roughness: 2 },
      { type: "arrow" },
      { type: "line" },
      { type: "text", text: "a" },
    ]);
    expect(got.map((el) => el.roughness)).toEqual([0, 0, 0, 0]);
  });

  it("文字とラベルに書体を当て、書体の欄が無い要素には足さない", () => {
    const got = withCleanStyle<{
      type: string;
      label?: { text: string; fontFamily?: number };
      fontFamily?: number;
    }>([
      { type: "text", fontFamily: FONT_FAMILY.Excalifont },
      { type: "rectangle", label: { text: "ラベル" } },
      { type: "ellipse" },
    ]);
    expect(got[0]?.fontFamily).toBe(FONT_FAMILY.Nunito);
    expect(got[1]?.label).toEqual({
      text: "ラベル",
      roughness: 0,
      fontFamily: FONT_FAMILY.Nunito,
    });
    expect(got[1]).not.toHaveProperty("fontFamily");
    expect(got[2]).not.toHaveProperty("fontFamily");
  });

  // 骨格はほかの用途（ドラフトの見せ直しなど）でも読まれうる。書き換えると、
  // 呼んだ側が持っている骨格まで見た目が変わる。
  it("渡した骨格を書き換えない", () => {
    const label = { text: "a" };
    const input = [{ type: "rectangle", roughness: 1, label }];
    withCleanStyle(input);
    expect(input[0]?.roughness).toBe(1);
    expect(label).toEqual({ text: "a" });
  });
});
