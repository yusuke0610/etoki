import { serializeAsJSON } from "@excalidraw/excalidraw";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { isAnnotation, type SceneElement } from "./annotation";
import {
  DRAFT_GAP,
  draftOrigin,
  type ElementSkeleton,
  mermaidToElements,
  moveDraft,
} from "./mermaid";

/**
 * mermaid が図を組み立てるのに要る SVG の実測。**jsdom には無いので補う。**
 *
 * `web/src/test-setup.ts` の canvas スタブと違って全体には置かない。あちらは
 * excalidraw を import するだけで要るが、こちらが要るのはこのファイルだけで、
 * しかも**返しているのは本物ではない寸法**だから。置き場所を分けておけば、
 * この嘘の届く範囲がファイル 1 つに見える。
 */
const originalGetBBox = Object.getOwnPropertyDescriptor(SVGElement.prototype, "getBBox");

beforeAll(() => {
  Object.defineProperty(SVGElement.prototype, "getBBox", {
    configurable: true,
    value: function getBBox(this: SVGElement) {
      const width = (this.textContent ?? "").length * 8;
      return { x: 0, y: 0, width, height: 20 } as DOMRect;
    },
  });
});

afterAll(() => {
  if (originalGetBBox) {
    Object.defineProperty(SVGElement.prototype, "getBBox", originalGetBBox);
  } else {
    delete (SVGElement.prototype as unknown as Record<string, unknown>).getBBox;
  }
});

/** 種類ごとの数。何が出たかを取りこぼさずに断言するために数える。 */
const countByType = (elements: readonly SceneElement[]): Record<string, number> =>
  elements.reduce<Record<string, number>>((acc, el) => {
    acc[el.type] = (acc[el.type] ?? 0) + 1;
    return acc;
  }, {});

/** 変換器の代わり。骨格をそのまま返す。 */
const returning = (elements: ElementSkeleton[]) => async () => ({ elements });

/** 変換器の代わり。構文エラーのように投げる。 */
const throwing = (message: string) => async () => {
  throw new Error(message);
};

describe("mermaidToElements", () => {
  // flowchart と sequence は jsdom でも本物の変換を通る。ここが落ちるのは、
  // 依存の組み合わせが崩れて変換そのものが読めなくなったとき。
  it("flowchart を要素にする", async () => {
    const got = await mermaidToElements("flowchart TD\n  A[はじめ] --> B[おわり]");

    expect(got.ok).toBe(true);
    if (!got.ok) return;
    // ノード 2 つ、矢印 1 本、ラベル 2 つ。「非空」や「矩形がある」で済ませると、
    // 画像 1 枚に落ちた結果でも通ってしまう。
    expect(countByType(got.elements)).toEqual({ rectangle: 2, arrow: 1, text: 2 });
  });

  it("sequence を要素にする", async () => {
    const got = await mermaidToElements(
      "sequenceDiagram\n  Alice->>Bob: おはよう\n  Bob-->>Alice: やあ",
    );

    expect(got.ok).toBe(true);
    if (!got.ok) return;
    // 登場人物の箱が上下に 2 つずつ、生存線が 2 本、やりとりが 2 本。
    expect(countByType(got.elements)).toEqual({
      rectangle: 4,
      line: 2,
      arrow: 2,
      text: 6,
    });
  });

  // #58 の原則。生成物を自動で注釈にしない。ここが切れると、bot が描いた絵が
  // そのまま解釈の対象になり、LLM の出力を LLM に読み直させることになる。
  it("customData を付けず、注釈にもしない", async () => {
    const got = await mermaidToElements("flowchart TD\n  A[はじめ] --> B[おわり]");

    expect(got.ok).toBe(true);
    if (!got.ok) return;
    for (const el of got.elements) {
      expect(el.customData ?? null).toBeNull();
      expect(isAnnotation(el)).toBe(false);
    }
  });

  it("構文エラーは投げずに syntax で返す", async () => {
    const got = await mermaidToElements("flowchart TD\n  A[[[[ -->");

    expect(got.ok).toBe(false);
    if (got.ok) return;
    expect(got.reason).toBe("syntax");
    // 投げ直しの手掛かりになるものが載っていること。空で返すと、再送しても
    // 同じ出力が返るだけになる。
    expect(got.detail).toContain("Parse error");
  });

  it("変換器が投げたら syntax で返し、例外を素通しにしない", async () => {
    const got = await mermaidToElements("なんでもよい", throwing("boom"));

    expect(got).toEqual({ ok: false, reason: "syntax", detail: "boom" });
  });

  // 図の種類が変換器の守備範囲の外だと、mermaid は SVG を描いて画像 1 枚に
  // して返す。置くと手で直せない絵がドラフトの顔をして残る。
  it("画像 1 枚で返ってきたら置かない", async () => {
    const got = await mermaidToElements("なんでもよい", returning([{ type: "image" }]));

    expect(got.ok).toBe(false);
    if (got.ok) return;
    expect(got.reason).toBe("unsupported");
    expect(got.detail).toContain("image");
  });

  // etoki は frame を自前生成しない。変換器が返すようになったら、注釈にできる
  // frame を etoki が配ったことになるので、そこで止める。
  it("frame が混ざっていたら置かない", async () => {
    const got = await mermaidToElements(
      "なんでもよい",
      returning([{ type: "rectangle" }, { type: "frame" }]),
    );

    expect(got.ok).toBe(false);
    if (got.ok) return;
    expect(got.reason).toBe("unsupported");
    expect(got.detail).toContain("frame");
  });

  // 本物の変換器が今日 frame を返さないことを固定する。上の門番と対で、
  // 「返さない」と「返ってきたら止める」の両方が要る。片方だけだと、門番が
  // 効くのか、門番が要るのかのどちらかが確かめられない。
  it.each([
    ["flowchart", "flowchart TD\n  A[はじめ] --> B[おわり]"],
    ["sequence", "sequenceDiagram\n  Alice->>Bob: おはよう"],
  ])("本物の変換は frame を返さない（%s）", async (_name, definition) => {
    const got = await mermaidToElements(definition);

    expect(got.ok).toBe(true);
    if (!got.ok) return;
    expect(got.elements.some((el) => el.type === "frame")).toBe(false);
  });
});

const at = (id: string, x: number, y: number, width = 10, height = 10): SceneElement => ({
  id,
  type: "rectangle",
  x,
  y,
  width,
  height,
});

describe("draftOrigin", () => {
  it("何も無ければ原点", () => {
    expect(draftOrigin([])).toEqual({ x: 0, y: 0 });
  });

  it("既存の絵の右外に、間を空けて置く", () => {
    const existing = [at("a", 0, 40, 100, 50), at("b", 200, 10, 60, 20)];

    // 右端は 260、上端は 10。
    expect(draftOrigin(existing)).toEqual({ x: 260 + DRAFT_GAP, y: 10 });
  });

  // 重ねないことがこの関数の目的。ここが切れると、生成物と手描きの区別が
  // つかなくなり、気に入らないドラフトを選び分けて消せない。
  it("既存の絵と重ならない", () => {
    const existing = [at("a", 0, 0, 300, 200)];
    const draft = [at("d1", -1000, -1000, 400, 400)];

    const placed = moveDraft(draft, draftOrigin(existing));

    // 既存の絵の右端は 300。ドラフトの左端がそれを越えていれば重ならない。
    expect(placed[0]?.x).toBeGreaterThan(300);
  });

  it("削除済みの要素は数えない", () => {
    const existing = [at("a", 0, 0, 100, 100), { ...at("b", 9000, 0), isDeleted: true }];

    expect(draftOrigin(existing)).toEqual({ x: 100 + DRAFT_GAP, y: 0 });
  });

  // 位置を持たない要素まで原点扱いすると、置き場所が理由もなく左上に寄る。
  it("位置を持たない要素は数えない", () => {
    const existing = [at("a", 500, 500, 10, 10), { id: "b", type: "text" }];

    expect(draftOrigin(existing)).toEqual({ x: 510 + DRAFT_GAP, y: 500 });
  });
});

describe("moveDraft", () => {
  it("左上を origin に合わせ、要素どうしの位置関係は保つ", () => {
    const draft = [at("a", 10, 20, 10, 10), at("b", 40, 60, 10, 10)];

    const got = moveDraft(draft, { x: 1000, y: 2000 });

    expect(got.map((el) => [el.x, el.y])).toEqual([
      [1000, 2000],
      [1030, 2040],
    ]);
  });

  // その場で書き換えると、Excalidraw が同じ要素だと見て再描画しないことがある。
  it("元の要素を書き換えない", () => {
    const draft = [at("a", 10, 20)];

    moveDraft(draft, { x: 500, y: 500 });

    expect(draft[0]).toEqual(expect.objectContaining({ x: 10, y: 20 }));
  });

  it("空なら空のまま返す", () => {
    expect(moveDraft([], { x: 100, y: 100 })).toEqual([]);
  });
});

describe("変換して置くまで", () => {
  // #58 の原則。既存の要素には一切触らない。追加するだけ。
  it("既存の要素は 1 つも変わらない", async () => {
    const existing = [at("a", 0, 0, 100, 100), at("b", 150, 30, 40, 40)];
    const before = structuredClone(existing);

    const draft = await mermaidToElements("flowchart TD\n  A[はじめ] --> B[おわり]");
    expect(draft.ok).toBe(true);
    if (!draft.ok) return;

    const next = [...existing, ...moveDraft(draft.elements, draftOrigin(existing))];

    expect(next.slice(0, existing.length)).toEqual(before);
    expect(next.length).toBeGreaterThan(existing.length);
  });

  // 壊れた JSON を保存するとボードごと開けなくなる。サーバーの検査
  // （usecase.validateScene）は読めることと大きさしか見ないので、フロントで
  // 確かめられるのは読めるほうまで。
  //
  // **大きさの上限はここでは書かない。** 判定を持つのはサーバーだけ
  // （ADR 0038、`web/CLAUDE.md`）。代わりに、上限へ近づく唯一の経路である
  // 画像がシーンに載っていないことを見る。
  it("置いたあとのシーンが読める JSON になり、画像を持ち込まない", async () => {
    const draft = await mermaidToElements("flowchart TD\n  A[はじめ] --> B[おわり]");
    expect(draft.ok).toBe(true);
    if (!draft.ok) return;

    const placed = moveDraft(draft.elements, draftOrigin([]));
    const scene = serializeAsJSON(placed as never[], {} as never, {}, "local");

    const parsed = JSON.parse(scene) as { elements: unknown[]; files?: unknown };
    expect(parsed.elements).toHaveLength(placed.length);
    expect(parsed.files ?? {}).toEqual({});
  });
});
