import type { Viewport } from "../excalidraw/annotationOverlay";

/**
 * キャンバスの移動を楽にする（#250）。矢印キーでのスクロールと、ドラッグ中に
 * 端へ寄せたときのスクロールの量を決める純関数。
 *
 * Excalidraw 0.18.1 はどちらも持っていない。スクロールを動かすのはホイール・
 * 手のひら・スクロールバー・PageUp/PageDown だけで、矢印キーは選んだ要素を
 * 動かすだけ（選択が空なら何もしない）。
 *
 * **量は画面の px で決め、シーンへはズームで割って渡す**（`scrolledBy`）。
 * シーンの座標で決めると、縮小しているときほど 1 回で大きく飛ぶ。
 */

/** 画面の座標での矩形。 */
export interface Rect {
  left: number;
  top: number;
  right: number;
  bottom: number;
}

/** 画面の px での移動量。正は右と下（中身が左と上へ流れる）。 */
export interface ScrollDelta {
  x: number;
  y: number;
}

/** 矢印キー 1 回で動かす量（画面の px）。 */
export const ARROW_STEP = 100;

/**
 * Shift と一緒に押したときに動かす割合（見えている幅か高さに対して）。
 * **1 画面ぶんにしない。** 前に見えていたものが端に残らないと、どこから
 * 来たのか見失う。
 */
export const ARROW_SHIFT_RATIO = 0.8;

/**
 * 矢印キーで動かす量。矢印キーでなければ null。
 *
 * `width` と `height` はキャンバスの見えている大きさ（画面の px）。
 */
export function arrowScrollDelta(
  key: string,
  shiftKey: boolean,
  width: number,
  height: number,
): ScrollDelta | null {
  const horizontal = shiftKey ? width * ARROW_SHIFT_RATIO : ARROW_STEP;
  const vertical = shiftKey ? height * ARROW_SHIFT_RATIO : ARROW_STEP;
  switch (key) {
    case "ArrowLeft":
      return { x: -horizontal, y: 0 };
    case "ArrowRight":
      return { x: horizontal, y: 0 };
    case "ArrowUp":
      return { x: 0, y: -vertical };
    case "ArrowDown":
      return { x: 0, y: vertical };
    default:
      return null;
  }
}

/**
 * 端の帯の幅（画面の px）。ポインターがキャンバスの枠からこの内側に入ったら
 * スクロールし始める。
 */
export const EDGE_ZONE = 48;

/** 端でのスクロールのいちばん速いとき（画面の px / 秒）。 */
export const EDGE_MAX_SPEED = 900;

/**
 * 1 軸ぶん。帯に入った深さに比例して速くし、枠の外では最大にする。
 *
 * **帯は枠の 1/4 を超えない。** 狭い枠で両側の帯が重なると、真ん中に置いても
 * どちらかへ流れ続ける。
 */
function edgeAxis(position: number, min: number, max: number): number {
  const zone = Math.min(EDGE_ZONE, (max - min) / 4);
  if (zone <= 0) return 0;
  const depth =
    position < min + zone
      ? -(min + zone - position)
      : position > max - zone
        ? position - (max - zone)
        : 0;
  return Math.sign(depth) * Math.min(Math.abs(depth) / zone, 1) * EDGE_MAX_SPEED;
}

/**
 * ドラッグ中のポインターの位置から、端でのスクロールの速さ（画面の px / 秒）を
 * 決める。端の帯に入っていなければ 0。
 *
 * `rect` は**見えているキャンバスの枠**で、重ねたパネルが覆うぶんを除いたもの
 * （`visibleCanvasRect`）。
 */
export function edgeScrollVelocity(x: number, y: number, rect: Rect): ScrollDelta {
  return {
    x: edgeAxis(x, rect.left, rect.right),
    y: edgeAxis(y, rect.top, rect.bottom),
  };
}

/**
 * 見えているキャンバスの枠。`frame` はキャンバスの枠（`.canvas-frame`）、
 * `covers` はその上に重なりうるパネル（右のパネルと畳んだ帯）の矩形。
 *
 * **右から覆うものだけを引く。** 重ねる置き方（`overlay`）では、パネルは
 * キャンバスの右に浮いている。並べる置き方ではキャンバスの外にあるので、
 * 何も引かれない。スマホで全面に開いたパネルは枠を丸ごと覆うので、空になって
 * null を返す（ドラッグできる面が無い）。
 */
export function visibleCanvasRect(frame: Rect, covers: readonly Rect[]): Rect | null {
  let right = frame.right;
  for (const cover of covers) {
    const overlaps =
      cover.left < right &&
      cover.right > frame.left &&
      cover.top < frame.bottom &&
      cover.bottom > frame.top;
    if (overlaps) right = Math.min(right, cover.left);
  }
  if (right <= frame.left || frame.bottom <= frame.top) return null;
  return { left: frame.left, top: frame.top, right, bottom: frame.bottom };
}

/**
 * 画面の px で `delta` だけ動かしたあとのスクロール位置。
 *
 * Excalidraw は `画面の x = (シーンの x + scrollX) × zoom` で描くので、右へ
 * 動かす（中身を左へ流す）には `scrollX` を減らす。
 */
export function scrolledBy(
  viewport: Viewport,
  delta: ScrollDelta,
): { scrollX: number; scrollY: number } {
  return {
    scrollX: viewport.scrollX - delta.x / viewport.zoom,
    scrollY: viewport.scrollY - delta.y / viewport.zoom,
  };
}
