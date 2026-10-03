import { describe, expect, it } from "vitest";

import type { SceneElement } from "./annotation";
import {
  CELL_HEIGHT,
  CELL_WIDTH,
  TABLE_COLUMNS,
  TABLE_ROWS,
  createTable,
  tableCenter,
  type ViewportBox,
} from "./table";

const view: ViewportBox = { scrollX: 0, scrollY: 0, zoom: 1, width: 1000, height: 600 };

describe("createTable", () => {
  it("矩形が 3 列 × 3 行の 9 枚できる", () => {
    const table = createTable({ x: 0, y: 0 });
    expect(TABLE_COLUMNS * TABLE_ROWS).toBe(9);
    expect(table).toHaveLength(9);
    expect(table.every((el) => el.type === "rectangle")).toBe(true);
  });

  // 表として 1 つ動かせる形にする。group は frame と違い、範囲に入る要素の
  // 帰属を etoki が判定する必要が無い（ルートの CLAUDE.md の frame の線に触れない）。
  it("9 枚が同じ 1 つの group にまとまる", () => {
    const table = createTable({ x: 0, y: 0 });
    const groups = new Set(table.map((el) => el.groupIds?.join(",")));
    expect(groups.size).toBe(1);
    const [only] = [...groups];
    expect(only).toBeTruthy();
    expect(table.every((el) => el.groupIds?.length === 1)).toBe(true);
  });

  it("group の ID は呼ぶたびに別になる", () => {
    const a = createTable({ x: 0, y: 0 })[0]?.groupIds?.[0];
    const b = createTable({ x: 0, y: 0 })[0]?.groupIds?.[0];
    expect(a).not.toBe(b);
  });

  it("セルは隙間も重なりもなく格子に並ぶ", () => {
    const table = createTable({ x: 0, y: 0 });
    const xs = [...new Set(table.map((el) => el.x))].sort((p, q) => (p ?? 0) - (q ?? 0));
    const ys = [...new Set(table.map((el) => el.y))].sort((p, q) => (p ?? 0) - (q ?? 0));

    expect(xs).toHaveLength(TABLE_COLUMNS);
    expect(ys).toHaveLength(TABLE_ROWS);
    xs.slice(1).forEach((x, i) => expect((x ?? 0) - (xs[i] ?? 0)).toBe(CELL_WIDTH));
    ys.slice(1).forEach((y, i) => expect((y ?? 0) - (ys[i] ?? 0)).toBe(CELL_HEIGHT));
    expect(
      table.every((el) => el.width === CELL_WIDTH && el.height === CELL_HEIGHT),
    ).toBe(true);
  });

  it("表全体の中心が渡した点に来る", () => {
    const table = createTable({ x: 500, y: 300 });
    const left = Math.min(...table.map((el) => el.x ?? 0));
    const top = Math.min(...table.map((el) => el.y ?? 0));
    const right = Math.max(...table.map((el) => (el.x ?? 0) + (el.width ?? 0)));
    const bottom = Math.max(...table.map((el) => (el.y ?? 0) + (el.height ?? 0)));

    expect((left + right) / 2).toBeCloseTo(500);
    expect((top + bottom) / 2).toBeCloseTo(300);
  });

  // 手で描いた矩形と区別しない（ADR 0069）。区別すると、色や形から意味を
  // 読むコードを書きたくなる（中核思想 2）。
  it("customData を付けず、frame も作らず、中身は空", () => {
    const table = createTable({ x: 0, y: 0 });
    expect(table.some((el) => el.customData !== undefined)).toBe(false);
    expect(table.some((el) => el.type === "frame")).toBe(false);
    // 文字を入れると、消し忘れが content_hash と解釈の入力に並ぶ。
    expect(table.some((el) => el.type === "text")).toBe(false);
  });

  it("全セルが同じ見た目で、見出し行を区別しない", () => {
    const table = createTable({ x: 0, y: 0 }) as (SceneElement &
      Record<string, unknown>)[];
    const looks = new Set(
      table.map((el) =>
        JSON.stringify([el.backgroundColor, el.strokeColor, el.strokeWidth]),
      ),
    );
    expect(looks.size).toBe(1);
  });
});

describe("tableCenter", () => {
  it("見えている範囲の中央を返す", () => {
    expect(tableCenter(view)).toEqual({ x: 500, y: 300 });
  });

  // Excalidraw のスクロールはシーン座標で、ズームで表示座標に変わる。
  it("スクロールとズームを反映する", () => {
    const moved: ViewportBox = { ...view, scrollX: -200, scrollY: 100, zoom: 2 };
    // 幅 1000 / 2 / zoom 2 - (-200) = 250 + 200、高さ 600 / 2 / 2 - 100 = 50
    expect(tableCenter(moved)).toEqual({ x: 450, y: 50 });
  });
});

describe("createTable の戻り値", () => {
  // 呼び出し側は [...current, ...table] で組む。入力を取らず、呼ぶたびに新しい
  // 配列と新しい id を返すことを固定する。
  it("毎回新しい配列を返す", () => {
    const a = createTable({ x: 0, y: 0 });
    const b = createTable({ x: 0, y: 0 });
    expect(a).not.toBe(b);
    expect(a[0]?.id).not.toBe(b[0]?.id);
  });
});
