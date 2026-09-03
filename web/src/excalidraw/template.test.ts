import { describe, expect, it } from "vitest";

import { diagramKinds } from "../board/diagramLabels";
import { isAnnotation, type SceneElement } from "./annotation";
import { BLANK_TEMPLATE, templateElements, templateScene } from "./template";

describe("templateScene", () => {
  // 空白のときはシーンを組み立てず、サーバーの既定に任せる。手元でも空の
  // シーンを作ると、同じものが 2 箇所になる。
  it("空白ならシーンを送らない", () => {
    expect(templateScene(BLANK_TEMPLATE)).toBeUndefined();
  });

  it("ひな形は読めるシーン JSON になる", () => {
    for (const kind of diagramKinds()) {
      const raw = templateScene(kind);
      expect(raw, kind).toBeTypeOf("string");

      const scene = JSON.parse(raw as string) as {
        type: string;
        elements: SceneElement[];
      };
      // サーバーの usecase.emptyScene と同じ形。読めない JSON を保存すると
      // 次に開いたときにボードごと開けなくなるので、入口の形は固定する。
      expect(scene.type, kind).toBe("excalidraw");
      expect(scene.elements.length, kind).toBeGreaterThan(0);
    }
  });
});

describe("templateElements", () => {
  // **ひな形が配るのは絵だけ**（ADR 0044）。etoki は frame を自前生成しない、
  // という線をひな形のためにも曲げない。どこを issue 化の対象にするかは、
  // これまでどおり人がフレームツールで囲んで決める。
  it("frame を作らず、注釈も作らない", () => {
    for (const kind of diagramKinds()) {
      const elements = templateElements(kind);

      expect(
        elements.filter((el) => el.type === "frame").map((el) => el.id),
        kind,
      ).toEqual([]);
      expect(
        elements.filter(isAnnotation).map((el) => el.id),
        kind,
      ).toEqual([]);
    }
  });

  // 人が囲んだあとに `Scene.AnnotationTexts`（Go 側）が拾えるかどうかは、
  // 「frame の直接の子」か「frame の子である図形のラベル」であるかで決まる。
  // **入れ子の frame は辿らない**（ADR 0044）ので、囲んだときに 1 段で
  // 収まる形になっていることをここで固定する。文字が抜けても画面には出ず、
  // 気づくのは LLM の出力を見たときになる。
  it("囲めば 1 段で拾える形になっている", () => {
    for (const kind of diagramKinds()) {
      const elements = templateElements(kind);

      const texts = elements.filter((el) => el.type === "text");
      expect(texts.length, kind).toBeGreaterThan(0);

      // frame に入れれば直接の子になる要素の ID。ひな形は frame を作らないので、
      // ここでは「frame に属していない要素」がそのまま候補になる。
      const top = new Set(
        elements.filter((el) => frameIdOf(el) === null).map((el) => el.id),
      );

      const lost = texts.filter((el) => {
        const container = containerIdOf(el);
        return !top.has(el.id) && !(container !== null && top.has(container));
      });
      expect(
        lost.map((el) => el.id),
        kind,
      ).toEqual([]);
    }
  });

  // ブレストフェーズに etoki の構造（epic / issue）を持ち込まない（中核思想 1）。
  // ひな形が与えるのは設計の構造までで、構造化は解釈と開発者フェーズの仕事。
  it("ひな形に etoki の構造語を置かない", () => {
    for (const kind of diagramKinds()) {
      const words = templateElements(kind)
        .flatMap((el) => [(el as unknown as { text?: string }).text ?? "", el.name ?? ""])
        .join(" ")
        .toLowerCase();

      expect(words, kind).not.toContain("epic");
      expect(words, kind).not.toContain("issue");
    }
  });
});

/** `SceneElement` は etoki が使う分だけの形なので、帰属の 2 つは実体から読む。 */
function frameIdOf(el: SceneElement): string | null {
  return (el as unknown as { frameId?: string | null }).frameId ?? null;
}

function containerIdOf(el: SceneElement): string | null {
  return (el as unknown as { containerId?: string | null }).containerId ?? null;
}
