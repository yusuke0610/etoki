import { convertToExcalidrawElements } from "@excalidraw/excalidraw";

import type { SceneElement } from "./annotation";

/**
 * 付箋 1 枚の 1 辺（シーン座標）。
 *
 * 正方形にしてあるのは、書く前から「これは付箋だ」と読めるようにするため。
 * 矩形ツールで描くと寸法は毎回変わるので、既定の形を持つこと自体が導線になる。
 */
export const STICKY_SIZE = 180;

/**
 * 付箋の見た目。
 *
 * **色は 1 つだけ持つ。** 色を選ばせると、選んだ色に意味を持たせたくなり、
 * その意味を読むコードを書きたくなる。構造は座標や色から推測せず LLM に
 * 解釈させる（中核思想 2）ので、etoki 側が色から何かを決めることはない。
 * 人が塗り分けたければ Excalidraw の既存の操作でできる。
 *
 * **テンプレート（`template.ts`）も同じものを使う。** 「付箋はこの見た目」を
 * 2 箇所に持つと、片方だけ変えたときに、1 枚置いた付箋とひな形の付箋が
 * 別の色で並ぶ。
 */
export const STICKY_STYLE = {
  backgroundColor: "#fff3bf",
  strokeColor: "#e8b339",
  fillStyle: "solid",
  strokeWidth: 1,
  roughness: 0,
} as const;

/** 重なりを避けるときにずらす量。 */
const STICKY_OFFSET = 24;

/**
 * ずらす回数の上限。
 *
 * 空きを探し続けるのではなく、諦めて重ねる。付箋を置けないことより、押しても
 * 何も起きないことのほうが困る。
 */
const MAX_OFFSET_STEPS = 20;

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
 * 付箋を置くシーン座標を決める。
 *
 * **いま見えている範囲の中央に置く。** 原点や既存要素の外側に置くと、押した
 * のに画面のどこにも現れず、探すところから始まる。描いている最中に 1 手で
 * 置けることがこの機能の値打ちなので、視界の外に出さない。
 *
 * すでに同じ場所に付箋があるときは少しずつずらす。ぴったり重ねると、2 枚目を
 * 置いたことも、1 枚目がまだあることも見えない。
 */
export function stickyNotePosition(
  view: ViewportBox,
  elements: readonly SceneElement[] = [],
): { x: number; y: number } {
  // Excalidraw のスクロールはシーン座標。表示中央のシーン座標は
  // (表示幅 / 2) / zoom - scrollX で出る。
  const centerX = view.width / 2 / view.zoom - view.scrollX;
  const centerY = view.height / 2 / view.zoom - view.scrollY;

  let x = centerX - STICKY_SIZE / 2;
  let y = centerY - STICKY_SIZE / 2;

  for (let i = 0; i < MAX_OFFSET_STEPS && occupied(elements, x, y); i++) {
    x += STICKY_OFFSET;
    y += STICKY_OFFSET;
  }

  return { x, y };
}

/** その座標に付箋がすでに置かれているか。 */
function occupied(elements: readonly SceneElement[], x: number, y: number): boolean {
  return elements.some((el) => !el.isDeleted && el.x === x && el.y === y);
}

/**
 * 付箋 1 枚ぶんの要素を作る。
 *
 * **frame は作らない。** etoki が frame を自前生成しないという線は、境界に
 * またがる要素の帰属判定を自分で持たないために引いてある。付箋は矩形なので
 * その線に触れない。帰属は Excalidraw の `frameId` / `containerId` がこれまで
 * どおり決める（`Scene.AnnotationTexts` はどちらも辿る）。
 *
 * **文字は入れない。** 空のテキスト要素を先に置くと、書かずに消した付箋の
 * ぶんまで `content_hash` の入力に並ぶ。選択した状態で返すので、Enter か
 * ダブルクリックでそのまま書き始められる。
 */
export function createStickyNote(x: number, y: number): SceneElement[] {
  return convertToExcalidrawElements([
    {
      type: "rectangle",
      x,
      y,
      width: STICKY_SIZE,
      height: STICKY_SIZE,
      ...STICKY_STYLE,
    },
  ]) as unknown as SceneElement[];
}
