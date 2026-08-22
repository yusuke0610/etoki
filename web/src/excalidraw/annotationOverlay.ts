import { sceneCoordsToViewportCoords } from "@excalidraw/excalidraw";
import type { NormalizedZoomValue } from "@excalidraw/excalidraw/types";

import type { Granularity } from "../api/types";
import { granularityOf, isAnnotation, type SceneElement } from "./annotation";

/**
 * キャンバスの見え方。Excalidraw の appState から要るぶんだけ受ける。
 *
 * appState をそのまま受けないのは、この計算が依存しているのが 3 つだけ
 * だと読めるようにするため。
 */
export type Viewport = {
  scrollX: number;
  scrollY: number;
  /** appState.zoom.value。1 が等倍。 */
  zoom: number;
};

/** 注釈の frame 1 つに重ねる枠。位置はキャンバス左上からの CSS ピクセル。 */
export type AnnotationBox = {
  id: string;
  granularity: Granularity;
  left: number;
  top: number;
  width: number;
  height: number;
};

/**
 * 注釈にした frame に重ねる枠を、いまの見え方に合わせて割り出す。
 *
 * **frame の見た目そのものは変えられない。** Excalidraw は frame を
 * ライブラリ側の定数で描いており、要素ごとの色や線種を持たない。注釈の frame は
 * ユーザーがフレームツールで作ったものなので、名前を書き換えて印を付ける手も
 * あるが、それはユーザーの持ちものを etoki が黙って変えることになる。
 * 要素に触らず上に重ねる形にしてあるのはそのため。**外すときに戻す処理も要らない。**
 *
 * 座標変換はライブラリの sceneCoordsToViewportCoords に任せる。同じ式を
 * 書き写すと、ズームの扱いが変わったときに枠だけが取り残される。
 * offsetLeft / offsetTop に 0 を渡しているのは、枠を置く親がキャンバスと
 * 同じ矩形だから（画面全体ではなくキャンバスの中の座標が要る）。
 */
export function annotationBoxes(
  elements: readonly SceneElement[],
  viewport: Viewport,
): AnnotationBox[] {
  const boxes: AnnotationBox[] = [];

  for (const el of elements) {
    if (!isAnnotation(el)) continue;
    // 位置と大きさの無い要素は重ねようがない。frame には必ずあるが、
    // 型の上では任意なので、無いものは黙って飛ばす。
    if (
      el.x === undefined ||
      el.y === undefined ||
      el.width === undefined ||
      el.height === undefined
    ) {
      continue;
    }

    const origin = {
      scrollX: viewport.scrollX,
      scrollY: viewport.scrollY,
      // ライブラリはズーム率に brand 付きの型を使う。値は appState から来た
      // ものをそのまま渡しているだけなので、ここで名乗り直す。
      zoom: { value: viewport.zoom as NormalizedZoomValue },
      offsetLeft: 0,
      offsetTop: 0,
    };
    const topLeft = sceneCoordsToViewportCoords({ sceneX: el.x, sceneY: el.y }, origin);
    const bottomRight = sceneCoordsToViewportCoords(
      { sceneX: el.x + el.width, sceneY: el.y + el.height },
      origin,
    );

    boxes.push({
      id: el.id,
      granularity: granularityOf(el) ?? "",
      left: topLeft.x,
      top: topLeft.y,
      width: bottomRight.x - topLeft.x,
      height: bottomRight.y - topLeft.y,
    });
  }

  return boxes;
}
