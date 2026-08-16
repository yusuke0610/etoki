import { describe, expect, it } from "vitest";

import type { AnnotationStatus } from "../api/types";
import { annotationLabel, annotationLabels, frameLabel } from "./annotationLabel";

function status(id: string, name: string): AnnotationStatus {
  return { id, name, granularity: "", state: "uncreated" };
}

describe("annotationLabel", () => {
  it("名前があればそのまま使う", () => {
    expect(annotationLabel("ログイン", 0)).toBe("ログイン");
  });

  it("名前が無ければ一覧上の位置で採番する", () => {
    expect(annotationLabel("", 1)).toBe("注釈 2");
  });

  // Excalidraw は空白だけの名前も受け取る。見た目が「名前なし」と同じものを
  // 名前として扱うと、見出しが空欄のカードが並ぶ。
  it("空白だけの名前は名前なしとして扱う", () => {
    expect(annotationLabel("   ", 2)).toBe("注釈 3");
  });
});

describe("annotationLabels", () => {
  // 採番は名前の有無ではなく一覧上の位置で振る。名前ありを飛ばして数えると、
  // 「注釈 2」が 2 番目のカードを指さなくなる。
  it("名前ありを飛ばさずに位置で採番する", () => {
    const got = annotationLabels([
      status("a", "ログイン"),
      status("b", ""),
      status("c", ""),
    ]);

    expect(got.get("a")).toBe("ログイン");
    expect(got.get("b")).toBe("注釈 2");
    expect(got.get("c")).toBe("注釈 3");
  });

  it("空の一覧では何も引けない", () => {
    expect(annotationLabels([]).size).toBe(0);
  });
});

describe("frameLabel", () => {
  it("名前があればそのまま使う", () => {
    expect(frameLabel("ただの枠")).toBe("ただの枠");
  });

  // まだ注釈でない frame は一覧に並んでいないので番号を持たない。
  // 番号を振ると、状態欄の「注釈 n」と食い違う番号が同じ画面に 2 種類出る。
  it("名前が無ければ番号を振らない", () => {
    expect(frameLabel("")).toBe("名前のないフレーム");
  });
});
