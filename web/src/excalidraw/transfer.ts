import { loadFromBlob, serializeAsJSON } from "@excalidraw/excalidraw";

import type { SceneElement } from "./annotation";
import type { SceneSource } from "./image";

/**
 * ボードの持ち出しと取り込み（ADR 0042）。
 *
 * **書き出しはキャンバスから出す。** 保存済みシーンから出すと、未保存の
 * 描き足しが黙って落ちる（中核思想 3）。
 *
 * **取り込みはここでは何も確定させない。** 返すのはキャンバスに載せるものだけで、
 * サーバーへの反映は人が保存を押したときにだけ起きる。
 *
 * **大きさの上限は持たない。** 判定はサーバーだけが持つ（ADR 0018 / 0038）。
 */

/** 書き出したファイルの拡張子。Excalidraw 本体が読める標準の形。 */
const EXTENSION = ".excalidraw";

/** ファイル名にできない文字を置き換えたときの代わり。 */
const REPLACEMENT = "_";

/** ボード名から作れる名前が無かったときの代わり。 */
const FALLBACK_NAME = "board";

/**
 * ファイル名の長さの上限（拡張子を除く文字数）。
 *
 * ボード名は自由入力なので、そのままでは 255 バイトを超えて保存に失敗する
 * 環境がある。**バイトではなく文字で数える。** 日本語のボード名で「まだ余裕が
 * あるのに切られた」と読める境界を作らないため、余裕を見て短く取ってある。
 */
const MAX_NAME_LENGTH = 80;

/**
 * ボード名から書き出すファイル名を作る。
 *
 * **安全のための門番ではない。** ブラウザは `download` 属性の値を独自に
 * 均すので、ここを通り抜けた文字がそのままファイルシステムに渡るわけではない。
 * 目的は、書き出したファイルがどのボードのものか読める名前にすること。
 *
 * 置き換えるのは、主要な OS のどれかで名前に使えない文字と制御文字。区切り
 * 文字（`/` と `\`）もここに含まれる。
 */
export function exportFileName(boardName: string): string {
  const replaced = boardName
    // eslint-disable-next-line no-control-regex -- 制御文字そのものを落とす
    .replace(/[\u0000-\u001f\u007f<>:"/\\|?*]/g, REPLACEMENT)
    .trim()
    .slice(0, MAX_NAME_LENGTH)
    // 切り詰めで末尾に空白が出ることがある。ドットで終わる名前を嫌う環境も
    // あるので一緒に落とす。
    .replace(/[.\s]+$/, "");

  // 空白だけの名前と、ドットだけの名前（`.` / `..`）がここに落ちる。名前が
  // 無いことを空文字で表すと、拡張子だけの隠しファイルになる。
  const name = replaced === "" || /^\.+$/.test(replaced) ? FALLBACK_NAME : replaced;

  return `${name}${EXTENSION}`;
}

/**
 * 保存に送るのと同じシーン JSON を作る。
 *
 * **保存・大きさの計測・書き出しの 3 つが同じものを見る**（ADR 0042）。別々に
 * 書くと、書き出したファイルとヘッダーに出ている大きさと実際に保存される
 * バイト列が、少しずつ違うものになりうる。
 *
 * `getFiles()` ごと直列化するので、貼った画像も base64 で乗る（ADR 0038）。
 */
export function sceneJSON(api: SceneSource): string {
  return serializeAsJSON(
    api.getSceneElements() as never,
    api.getAppState() as never,
    api.getFiles() as never,
    "local",
  );
}

/** 取り込んだシーンのうち、キャンバスに載せるもの。 */
export type ImportedScene = {
  elements: SceneElement[];
  /** 貼ってあった画像。`addFiles` に渡す形。 */
  files: unknown[];
  /**
   * キャンバスの背景色。保存はこれを含めて書くので、取り込みでも運ぶ。
   *
   * undefined は「ファイルにも今の画面にも無い」。**既定値をここで決めない。**
   * 決めると、Excalidraw が既定を変えた日に取り込みだけ色が変わる。
   */
  viewBackgroundColor: string | undefined;
};

/** loadFromBlob のうち、ここで使う部分だけの形。 */
export type LoadFromBlob = (
  blob: Blob,
  localAppState: never,
  localElements: never,
) => Promise<{
  elements: readonly unknown[];
  appState: { viewBackgroundColor?: string | null };
  files?: Record<string, unknown>;
}>;

/**
 * `.excalidraw` ファイルを読む。読めなければ投げる。
 *
 * **読めるかどうかの判定はライブラリに任せる**（ADR 0042）。判定を自前で書くと、
 * Excalidraw が読める形の集合を etoki 側に複製することになり、ライブラリを
 * 上げた日にずれる。サーバーの `validateScene` とも二重定義ではない。あちらが
 * 見るのは「JSON として読めるか」と「上限に収まるか」で、目的が違う。
 *
 * **表示状態は取り込まない。** スクロール位置とズームは今の画面のものを渡して
 * 復元させる。取り込んだ絵の位置へは呼び出し側が寄せる（ADR 0042）。
 *
 * load は差し替えられるようにしてある。ライブラリの実物は Blob の読み取りが
 * 要り jsdom では動かないが、返ってきたものをどう詰め替えるかは確かめる必要が
 * あるため（`image.ts` の `exportBlob` と同じ形）。
 */
export async function readSceneFile(
  file: Blob,
  api: SceneSource,
  load: LoadFromBlob = loadFromBlob as unknown as LoadFromBlob,
): Promise<ImportedScene> {
  const restored = await load(
    file,
    api.getAppState() as never,
    api.getSceneElements() as never,
  );

  return {
    elements: restored.elements as SceneElement[],
    // `addFiles` は配列で受け取る。ライブラリが返すのは ID をキーにした表。
    files: Object.values(restored.files ?? {}),
    // ファイルに無ければ今の色を保つ。`restore` は渡した appState から埋める
    // ので普段は前者で決まるが、**無かったときに既定色を持ち出さない。**
    viewBackgroundColor:
      restored.appState.viewBackgroundColor ??
      (api.getAppState() as { viewBackgroundColor?: string }).viewBackgroundColor,
  };
}
