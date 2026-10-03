import { convertToExcalidrawElements } from "@excalidraw/excalidraw";

import type { SceneElement } from "./annotation";

/** 表の列数と行数。**固定にして、ダイアログを挟まない**（ADR 0069）。 */
export const TABLE_COLUMNS = 3;
export const TABLE_ROWS = 3;

/** セル 1 枚の大きさ（シーン座標）。文字を数語書ける横長にしてある。 */
export const CELL_WIDTH = 160;
export const CELL_HEIGHT = 60;

/**
 * セルの見た目。
 *
 * **全セル同じにして、見出し行を作らない。** 1 行目に色を付けると「1 行目は
 * 見出し」と etoki が決めたことになり、そこから見出しとして読ませたくなる
 * （中核思想 2）。塗り分けたければ人が Excalidraw の操作でやる。
 */
const CELL_STYLE = {
  backgroundColor: "#ffffff",
  strokeColor: "#1e1e1e",
  fillStyle: "solid",
  strokeWidth: 1,
  roughness: 0,
} as const;

/** 置き場所を決めるのに要る、いまのキャンバスの見え方。 */
export type ViewportBox = {
  scrollX: number;
  scrollY: number;
  zoom: number;
  /** キャンバスの表示幅（CSS ピクセル）。 */
  width: number;
  /** キャンバスの表示高さ（CSS ピクセル）。 */
  height: number;
};

/**
 * 表を置く中心のシーン座標。**いま見えている範囲の中央。**
 *
 * 視界の外に出すと、押したのに何も現れず、探すところから始まる。
 * Excalidraw のスクロールはシーン座標なので、表示中央は
 * `(表示幅 / 2) / zoom - scrollX` で出る。
 */
export function tableCenter(view: ViewportBox): { x: number; y: number } {
  return {
    x: view.width / 2 / view.zoom - view.scrollX,
    y: view.height / 2 / view.zoom - view.scrollY,
  };
}

/**
 * group の ID。
 *
 * **`crypto.randomUUID` は使わない。** secure context 専用で、認証を付けて
 * HTTP で公開した構成（README）の別端末からは呼べず、表が置けなくなる。
 * `getRandomValues` は secure context を要らない。
 */
function newGroupId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

/**
 * 表を作る。空の矩形を格子に並べ、全体を 1 つの group にする。
 *
 * **frame は作らない。** frame を自前で生成すると、境界にまたがる要素の
 * 帰属判定を etoki が持つことになる（ルートの `CLAUDE.md`）。group は
 * 所属を要素自身が持つだけで、その判定を etoki は持たない。
 *
 * **`customData` は付けない。** 手で描いた矩形と区別しない（ADR 0069）。
 * **文字も入れない。** 空のテキストを先に置くと、書かずに消したぶんまで
 * `content_hash` の入力に並ぶ。
 */
export function createTable(center: { x: number; y: number }): SceneElement[] {
  const left = center.x - (TABLE_COLUMNS * CELL_WIDTH) / 2;
  const top = center.y - (TABLE_ROWS * CELL_HEIGHT) / 2;
  const groupId = newGroupId();

  const cells = [];
  for (let row = 0; row < TABLE_ROWS; row++) {
    for (let col = 0; col < TABLE_COLUMNS; col++) {
      cells.push({
        type: "rectangle" as const,
        x: left + col * CELL_WIDTH,
        y: top + row * CELL_HEIGHT,
        width: CELL_WIDTH,
        height: CELL_HEIGHT,
        groupIds: [groupId],
        ...CELL_STYLE,
      });
    }
  }

  return convertToExcalidrawElements(cells as never) as unknown as SceneElement[];
}
