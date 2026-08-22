import { describe, expect, it } from "vitest";

import type { SceneElement } from "./annotation";
import { annotationBoxes } from "./annotationOverlay";

const frame = (overrides: Partial<SceneElement> = {}): SceneElement => ({
  id: "f1",
  type: "frame",
  x: 100,
  y: 50,
  width: 200,
  height: 120,
  customData: { etoki: { granularity: "" } },
  ...overrides,
});

/** 等倍・スクロールなし。要素の座標がそのまま画面の座標になる。 */
const identity = { scrollX: 0, scrollY: 0, zoom: 1 };

describe("annotationBoxes", () => {
  it("注釈の frame だけを返す", () => {
    const boxes = annotationBoxes(
      [
        frame(),
        frame({ id: "f2", customData: undefined }),
        { id: "t1", type: "text", x: 0, y: 0, width: 10, height: 10 },
      ],
      identity,
    );

    expect(boxes.map((b) => b.id)).toEqual(["f1"]);
  });

  it("削除済みの注釈には重ねない", () => {
    expect(annotationBoxes([frame({ isDeleted: true })], identity)).toEqual([]);
  });

  it("等倍なら要素の座標と大きさをそのまま返す", () => {
    expect(annotationBoxes([frame()], identity)).toEqual([
      { id: "f1", granularity: "", left: 100, top: 50, width: 200, height: 120 },
    ]);
  });

  // スクロールとズームを反映しないと、枠だけがキャンバスに置き去りになる。
  it("スクロールぶんだけずらす", () => {
    const [box] = annotationBoxes([frame()], { scrollX: -40, scrollY: 10, zoom: 1 });

    expect(box).toMatchObject({ left: 60, top: 60, width: 200, height: 120 });
  });

  it("ズームで位置も大きさも縮む", () => {
    const [box] = annotationBoxes([frame()], { scrollX: 0, scrollY: 0, zoom: 0.5 });

    expect(box).toMatchObject({ left: 50, top: 25, width: 100, height: 60 });
  });

  it("スクロールとズームが重なっても、左上と右下を同じ式で写す", () => {
    const [box] = annotationBoxes([frame()], { scrollX: -100, scrollY: -50, zoom: 2 });

    expect(box).toMatchObject({ left: 0, top: 0, width: 400, height: 240 });
  });

  // 粒度はキャンバス上で見分けたい対象そのもの。epic の注釈と issue の注釈を
  // 同じ見た目にすると、パネルを開かないと違いが分からない。
  it("粒度を添えて返す", () => {
    const boxes = annotationBoxes(
      [
        frame({ id: "e1", customData: { etoki: { granularity: "epic" } } }),
        frame({ id: "i1", customData: { etoki: { granularity: "issue" } } }),
      ],
      identity,
    );

    expect(boxes.map((b) => b.granularity)).toEqual(["epic", "issue"]);
  });

  it("位置や大きさを持たない要素は飛ばす", () => {
    expect(annotationBoxes([frame({ width: undefined })], identity)).toEqual([]);
  });
});
