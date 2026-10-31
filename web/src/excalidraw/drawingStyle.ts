import { FONT_FAMILY } from "@excalidraw/excalidraw";

/**
 * etoki のキャンバスで使う線と文字の見た目（ADR 0095）。
 *
 * **手書き風をやめ、揺れない線と普通の書体にする。** Excalidraw の既定は
 * 線を揺らし（roughness 1）、書体は Excalifont。Excalifont で書いた日本語は
 * 手書き風の中国語書体（Xiaolai）で描かれるので、日本語まで崩れて見える。
 * Excalifont 以外の書体ではその置き換えが起きない。
 *
 * **見た目だけを決める。** 線の揺れも書体も `content_hash` の入力ではなく、
 * 解釈が読むのは文字と画像なので、ここを変えても状態の判定は動かない。
 */
export const CLEAN_ROUGHNESS = 0;
export const CLEAN_FONT_FAMILY = FONT_FAMILY.Nunito;

/**
 * 人が描くときの既定（ツールの初期値）。`initialData.appState` に重ねる。
 *
 * **保存したシーンには載らない。** Excalidraw はこの 2 つを書き出しの対象に
 * していないので、開くたびに渡す必要がある。ボードを開いてから人が選び
 * 直した値は、そのボードを開いているあいだだけ効く。
 */
export const DRAWING_DEFAULTS = {
  currentItemRoughness: CLEAN_ROUGHNESS,
  currentItemFontFamily: CLEAN_FONT_FAMILY,
} as const;

type Skeleton = { type: string; label?: unknown };

/**
 * etoki が置く図形の骨格に、同じ見た目を当てる。要素に詰め替える前に呼ぶ。
 *
 * **詰め替えたあとの要素に当てない。** 文字の大きさは詰め替えのときに書体で
 * 測るので、あとから書体だけ差し替えると、文字の箱が別の書体の寸法のまま残る。
 *
 * 書体を当てるのは文字（`text`）と図形・矢印のラベルだけ。ほかの要素には
 * 書体の欄が無い。
 */
export function withCleanStyle<T extends Skeleton>(skeletons: readonly T[]): T[] {
  return skeletons.map((el) => {
    const styled: T = { ...el, roughness: CLEAN_ROUGHNESS };
    if (el.type === "text") return { ...styled, fontFamily: CLEAN_FONT_FAMILY };
    if (typeof el.label === "object" && el.label !== null) {
      // ラベルは詰め替えで文字の要素になる。線の欄もそちらへ写るので、人が
      // 書いた文字（ツールの既定から 0 になる）と同じ値にしておく。
      return {
        ...styled,
        label: { ...el.label, roughness: CLEAN_ROUGHNESS, fontFamily: CLEAN_FONT_FAMILY },
      };
    }
    return styled;
  });
}
