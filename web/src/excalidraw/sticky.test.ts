import { describe, expect, it } from "vitest";

import type { SceneElement } from "./annotation";
import { createStickyNote, stickyNotePosition, STICKY_SIZE } from "./sticky";

const view = { scrollX: 0, scrollY: 0, zoom: 1, width: 800, height: 600 };

describe("stickyNotePosition", () => {
  it("見えている範囲の中央に置く", () => {
    // 中央は (400, 300)。付箋の左上はそこから半辺ぶん戻る。
    expect(stickyNotePosition(view)).toEqual({
      x: 400 - STICKY_SIZE / 2,
      y: 300 - STICKY_SIZE / 2,
    });
  });

  // スクロールとズームを無視すると、画面の外に置かれて「押しても何も出ない」
  // ように見える。ここが切れると、その回帰に気づけない。
  it("スクロールとズームを織り込む", () => {
    const got = stickyNotePosition({ ...view, scrollX: -1000, scrollY: -500, zoom: 2 });

    // 表示中央のシーン座標は (800/2)/2 - (-1000) = 1200、(600/2)/2 + 500 = 650。
    expect(got).toEqual({ x: 1200 - STICKY_SIZE / 2, y: 650 - STICKY_SIZE / 2 });
  });

  it("同じ場所に付箋があればずらす", () => {
    const first = stickyNotePosition(view);
    const existing: SceneElement[] = [
      { id: "a", type: "rectangle", x: first.x, y: first.y },
    ];

    const second = stickyNotePosition(view, existing);

    expect(second.x).toBeGreaterThan(first.x);
    expect(second.y).toBeGreaterThan(first.y);
  });

  // 削除済みの要素は場所を占めない。数えると、消した付箋のぶんだけ新しい
  // 付箋が中央から離れていく。
  it("削除済みの要素は避けない", () => {
    const first = stickyNotePosition(view);
    const deleted: SceneElement[] = [
      { id: "a", type: "rectangle", x: first.x, y: first.y, isDeleted: true },
    ];

    expect(stickyNotePosition(view, deleted)).toEqual(first);
  });
});

describe("createStickyNote", () => {
  it("指定した位置に既定の大きさの矩形を 1 つ作る", () => {
    const [note, ...rest] = createStickyNote(10, 20);

    expect(rest).toEqual([]);
    expect(note?.type).toBe("rectangle");
    expect(note?.x).toBe(10);
    expect(note?.y).toBe(20);
    expect(note?.width).toBe(STICKY_SIZE);
    expect(note?.height).toBe(STICKY_SIZE);
  });

  // frame を作らないことが「境界にまたがる要素の帰属判定を持たない」という
  // 線の中身。ここが frame になると、その判定を etoki が抱えることになる。
  it("frame を作らない", () => {
    expect(createStickyNote(0, 0).some((el) => el.type === "frame")).toBe(false);
  });

  // 空のテキスト要素を置くと、書かずに消した付箋のぶんまで content_hash の
  // 入力に並ぶ。矩形 1 つだけであることを固定する。
  it("テキスト要素を作らない", () => {
    expect(createStickyNote(0, 0).some((el) => el.type === "text")).toBe(false);
  });

  it("置くたびに別の ID になる", () => {
    expect(createStickyNote(0, 0)[0]?.id).not.toBe(createStickyNote(0, 0)[0]?.id);
  });
});
