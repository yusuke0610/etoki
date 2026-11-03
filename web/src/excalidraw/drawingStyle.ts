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
/** Excalidraw が書体を登録している名前。読み込みを待つときはこの名前で頼む。 */
const CLEAN_FONT_NAME = "Nunito";
export const CLEAN_FONT_FAMILY = FONT_FAMILY[CLEAN_FONT_NAME];

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

/**
 * 骨格の文字を清書調の書体で測れるよう、書体を読み込み終えるのを待つ。
 * 要素に詰め替える直前に呼ぶ。
 *
 * **待たずに詰め替えると、字の箱が代わりの書体の寸法で決まる。** ブラウザは
 * 書体を使われるまで読まないので、まだ Nunito で描いた文字が無いボードでは、
 * 詰め替えの計測が代わりの書体で行われる。Excalidraw は読み込み終えたあとに
 * 描き直すだけで、箱の寸法は測り直さない（書体を選び直す操作では、読み込みを
 * 待ってから測り直している）。
 *
 * **読めなくても置く。** 読み込みの失敗で図を置けなくするほうが失うものが
 * 大きい。箱がずれるだけで、文字は描ける。CSS Font Loading API の無い環境
 * （jsdom）でも何もせずに返る。
 */
export async function loadCleanFont(
  skeletons: readonly { type: string; text?: unknown; label?: unknown }[],
): Promise<void> {
  const fonts = typeof document === "undefined" ? undefined : document.fonts;
  if (!fonts) return;
  const chars = skeletons
    .flatMap((el) => [el.text, labelText(el.label)])
    .filter((t): t is string => typeof t === "string")
    .join("");
  if (chars === "") return;
  try {
    await fonts.load(`16px ${CLEAN_FONT_NAME}`, chars);
  } catch {
    // 上のとおり、読めなくても置く。
  }
}

function labelText(label: unknown): unknown {
  return typeof label === "object" && label !== null
    ? (label as { text?: unknown }).text
    : undefined;
}
