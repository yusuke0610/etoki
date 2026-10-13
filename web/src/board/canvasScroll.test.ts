import { describe, expect, it } from "vitest";

import {
  ARROW_SHIFT_RATIO,
  ARROW_STEP,
  EDGE_MAX_SPEED,
  EDGE_ZONE,
  arrowScrollDelta,
  edgeScrollVelocity,
  scrolledBy,
  visibleCanvasRect,
} from "./canvasScroll";

describe("arrowScrollDelta", () => {
  it.each([
    ["ArrowLeft", { x: -ARROW_STEP, y: 0 }],
    ["ArrowRight", { x: ARROW_STEP, y: 0 }],
    ["ArrowUp", { x: 0, y: -ARROW_STEP }],
    ["ArrowDown", { x: 0, y: ARROW_STEP }],
  ])("%s は向きに合わせて一定の量だけ動かす", (key, delta) => {
    expect(arrowScrollDelta(key, false, 1000, 600)).toEqual(delta);
  });

  // 幅と高さを別の値にして、横は幅、縦は高さから取っていることまで見る。
  it("Shift と一緒なら、見えている幅か高さの割合で動かす", () => {
    expect(arrowScrollDelta("ArrowRight", true, 1000, 600)).toEqual({
      x: 1000 * ARROW_SHIFT_RATIO,
      y: 0,
    });
    expect(arrowScrollDelta("ArrowUp", true, 1000, 600)).toEqual({
      x: 0,
      y: -600 * ARROW_SHIFT_RATIO,
    });
  });

  it.each(["a", "Enter", "PageDown", "Home"])("矢印キーでない %s は null", (key) => {
    expect(arrowScrollDelta(key, false, 1000, 600)).toBeNull();
  });
});

describe("edgeScrollVelocity", () => {
  const rect = { left: 100, top: 50, right: 900, bottom: 650 };

  it("端の帯に入っていなければ流さない", () => {
    expect(edgeScrollVelocity(500, 350, rect)).toEqual({ x: 0, y: 0 });
    // 帯のちょうど内側の縁。
    expect(
      edgeScrollVelocity(rect.right - EDGE_ZONE, rect.top + EDGE_ZONE, rect),
    ).toEqual({
      x: 0,
      y: 0,
    });
  });

  it("帯に入った深さに比例して速くする", () => {
    const half = edgeScrollVelocity(rect.right - EDGE_ZONE / 2, 350, rect);
    expect(half).toEqual({ x: EDGE_MAX_SPEED / 2, y: 0 });

    const quarter = edgeScrollVelocity(500, rect.top + (EDGE_ZONE * 3) / 4, rect);
    expect(quarter).toEqual({ x: 0, y: -EDGE_MAX_SPEED / 4 });
  });

  // 枠の外（パネルの上や窓の外）へ出ても止めない。運びたい先はもっと遠い。
  it("枠の外では最大の速さで流す", () => {
    expect(edgeScrollVelocity(rect.left - 200, 350, rect)).toEqual({
      x: -EDGE_MAX_SPEED,
      y: 0,
    });
    expect(edgeScrollVelocity(500, rect.bottom + 30, rect)).toEqual({
      x: 0,
      y: EDGE_MAX_SPEED,
    });
  });

  it("角では 2 方向に同時に流す", () => {
    expect(edgeScrollVelocity(rect.right, rect.bottom, rect)).toEqual({
      x: EDGE_MAX_SPEED,
      y: EDGE_MAX_SPEED,
    });
  });

  // 帯が重なると、真ん中に置いてもどちらかへ流れ続ける。
  it("狭い枠では、帯を枠の 1/4 に縮めて真ん中を空ける", () => {
    const narrow = { left: 0, top: 0, right: 120, bottom: 600 };
    expect(edgeScrollVelocity(60, 300, narrow)).toEqual({ x: 0, y: 0 });
    // 1/4（30px）の帯の中なら流す。
    expect(edgeScrollVelocity(15, 300, narrow).x).toBe(-EDGE_MAX_SPEED / 2);
  });
});

describe("visibleCanvasRect", () => {
  const frame = { left: 0, top: 0, right: 1000, bottom: 700 };

  it("何も重なっていなければ、キャンバスの枠そのもの", () => {
    expect(visibleCanvasRect(frame, [])).toEqual(frame);
  });

  // 重ねる置き方。パネルは上下を空けて右に浮いている。
  it("重ねたパネルが覆う右側を除く", () => {
    const panel = { left: 640, top: 72, right: 976, bottom: 628 };
    expect(visibleCanvasRect(frame, [panel])).toEqual({ ...frame, right: 640 });
  });

  // 並べる置き方。パネルはキャンバスの外にある。
  it("キャンバスの外に並べたパネルは引かない", () => {
    const panel = { left: 1000, top: 0, right: 1320, bottom: 700 };
    expect(visibleCanvasRect(frame, [panel])).toEqual(frame);
  });

  it("重なるものが複数なら、いちばん左の縁まで除く", () => {
    const rail = { left: 940, top: 72, right: 976, bottom: 628 };
    const panel = { left: 640, top: 72, right: 976, bottom: 628 };
    expect(visibleCanvasRect(frame, [rail, panel])?.right).toBe(640);
  });

  // スマホで全面に開いたパネル。ドラッグできる面が無い。
  it("枠を丸ごと覆われたら null", () => {
    expect(visibleCanvasRect(frame, [{ ...frame }])).toBeNull();
  });
});

describe("scrolledBy", () => {
  it("右へ動かすと scrollX が減り、ズームで割った量だけ動く", () => {
    expect(scrolledBy({ scrollX: 10, scrollY: 20, zoom: 2 }, { x: 100, y: -50 })).toEqual(
      {
        scrollX: 10 - 50,
        scrollY: 20 + 25,
      },
    );
  });
});
