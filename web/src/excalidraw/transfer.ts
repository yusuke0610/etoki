import { loadFromBlob, serializeAsJSON } from "@excalidraw/excalidraw";

import type { SceneElement } from "./annotation";
import type { SceneSource } from "./image";

/**
 * ボードの持ち出しと取り込み（ADR 0045）。
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
 * ボード名は自由入力なので、そのままでは長すぎる名前が作れてしまう。日本語の
 * ボード名で「まだ余裕があるのに切られた」と読める境界を作らないため、余裕を
 * 見て短く取ってある。
 */
const MAX_NAME_LENGTH = 80;

/**
 * ファイル名の大きさの上限（拡張子を除く UTF-8 のバイト数）。
 *
 * **文字数だけでは足りない。** 主要なファイルシステムが 1 要素あたり 255 バイト
 * で、そこには拡張子も入る。絵文字は 1 文字 4 バイトなので、80 文字に収めても
 * バイトでは 255 を超えて保存に失敗する環境がある。拡張子のぶんを引いた残りが
 * 名前に使える。
 *
 * 拡張子は ASCII だけなので、文字数がそのままバイト数になる。
 */
const MAX_NAME_BYTES = 255 - EXTENSION.length;

/**
 * 文字数とバイト数の両方に収まるところで切る。
 *
 * **`slice` を使わない。** あれは UTF-16 のコードユニットで切るので、境界が
 * サロゲート対の途中に落ちると、半分だけの絵文字がファイル名に残る。
 * `for...of` はコードポイントで回すので、1 文字が割れない。
 *
 * 絵文字の連なり（ZWJ で繋いだ家族など）はコードポイントの境界で切れうるが、
 * 残るのはどれも 1 文字として読める形なので、そこまでは見ない。
 */
function truncate(name: string): string {
  const encoder = new TextEncoder();
  let truncated = "";
  let characters = 0;
  let bytes = 0;

  for (const character of name) {
    const size = encoder.encode(character).length;
    if (characters + 1 > MAX_NAME_LENGTH || bytes + size > MAX_NAME_BYTES) break;
    truncated += character;
    characters += 1;
    bytes += size;
  }

  return truncated;
}

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
  const cleaned = boardName
    // eslint-disable-next-line no-control-regex -- 制御文字そのものを落とす
    .replace(/[\u0000-\u001f\u007f<>:"/\\|?*]/g, REPLACEMENT)
    .trim();

  const replaced = truncate(cleaned)
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
 * **保存・大きさの計測・書き出しの 3 つが同じものを見る**（ADR 0045）。別々に
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

/** 取り込んだ画像。衝突した ID を差し替えるため、使うフィールドだけ型に出す。 */
export type ImportedFile = {
  id: string;
  dataURL?: string;
  [key: string]: unknown;
};

/** 取り込んだ要素。画像だけが持つ fileId を衝突時に差し替える。 */
export type ImportedElement = SceneElement & { fileId?: string | null };

/** 取り込んだシーンのうち、キャンバスに載せるもの。 */
export type ImportedScene = {
  elements: ImportedElement[];
  /** 貼ってあった画像。要素の fileId と同じ ID をキーにする。 */
  files: Record<string, ImportedFile>;
  /**
   * キャンバスの背景色。保存はこれを含めて書くので、取り込みでも運ぶ。
   *
   * undefined は「ファイルにも今の画面にも無い」。**既定値をここで決めない。**
   * 決めると、Excalidraw が既定を変えた日に取り込みだけ色が変わる。
   */
  viewBackgroundColor: string | undefined;
};

/**
 * loadFromBlob のうち、ここで使う部分だけの形。
 *
 * **引数はライブラリの公開型から導出する。** 手で並べると、ライブラリが引数を足した
 * 日にこちらだけが古くなり、しかもキャストで実物を押し込んでいるので気づけない。
 * 導出してあれば実物がそのまま入る（`as unknown as` が要らない）。
 *
 * **戻り値は絞ったまま。** こちらも導出すると、テストのフェイクが
 * `RestoredAppState` の 85 個のフィールドを埋めることになり、**production から
 * 消したキャストをテスト 5 箇所に増やす**ことになる。絞っても実物は代入できる
 * ので、ライブラリとの食い違いは `tsc` が見つける（戻り値を絞るのは `image.ts` の
 * `ExportToBlob` と同じ。あちらは引数がまだ手書きで、直すなら同じ形にする）。
 */
export type LoadFromBlob = (...args: Parameters<typeof loadFromBlob>) => Promise<{
  elements: readonly unknown[];
  appState: { viewBackgroundColor?: string | null };
  files?: Record<string, unknown>;
}>;

/** いまのキャンバスが持つ画像のうち、衝突判定に使う部分。 */
type ExistingFiles = Record<string, { dataURL?: string }>;

/** 新しい画像 ID を作る。引数に分けて、衝突時の対応を固定値でテストできるようにする。 */
const newFileId = () => crypto.randomUUID();

/**
 * 取り込む画像 ID が別の画像に使われていたら、取り込む側を新しい ID に移す。
 *
 * Excalidraw の `addFiles` は既存の ID を別データで上書きしない。要素を取り込んだ
 * ものへ差し替えても、files 側に同じ ID の古い画像が残っていると、画面には古い
 * 画像が出る。同じ dataURL なら同じ画像なので ID は保つ。
 */
export function remapImportedFileIds(
  imported: ImportedScene,
  existingFiles: ExistingFiles,
  createId: () => string = newFileId,
): ImportedScene {
  let elements = imported.elements;
  const files = { ...imported.files };
  const usedIds = new Set([...Object.keys(existingFiles), ...Object.keys(files)]);

  for (const [fileId, file] of Object.entries(imported.files)) {
    const existing = existingFiles[fileId];
    if (existing === undefined || existing.dataURL === file.dataURL) continue;

    let nextId = createId();
    while (usedIds.has(nextId)) nextId = createId();
    usedIds.add(nextId);

    delete files[fileId];
    files[nextId] = { ...file, id: nextId };
    elements = elements.map((element) =>
      element.type === "image" && element.fileId === fileId
        ? { ...element, fileId: nextId }
        : element,
    );
  }

  return { ...imported, elements, files };
}

/**
 * `.excalidraw` ファイルを読む。読めなければ投げる。
 *
 * **読めるかどうかの判定はライブラリに任せる**（ADR 0045）。判定を自前で書くと、
 * Excalidraw が読める形の集合を etoki 側に複製することになり、ライブラリを
 * 上げた日にずれる。サーバーの `validateScene` とも二重定義ではない。あちらが
 * 見るのは「JSON として読めるか」と「上限に収まるか」で、目的が違う。
 *
 * **表示状態は取り込まない。** スクロール位置とズームは今の画面のものを渡して
 * 復元させる。取り込んだ絵の位置へは呼び出し側が寄せる（ADR 0045）。
 *
 * load は差し替えられるようにしてある。ライブラリの実物は Blob の読み取りが
 * 要り jsdom では動かないが、返ってきたものをどう詰め替えるかは確かめる必要が
 * あるため（`image.ts` の `exportBlob` と同じ形）。
 */
export async function readSceneFile(
  file: Blob,
  api: SceneSource,
  load: LoadFromBlob = loadFromBlob,
): Promise<ImportedScene> {
  const restored = await load(
    file,
    api.getAppState() as never,
    api.getSceneElements() as never,
  );

  return {
    elements: restored.elements as ImportedElement[],
    // キーを保っておく。現在のキャンバスと ID が衝突したとき、要素の fileId と
    // 同じ写像で差し替えてから `addFiles` に渡すために要る。
    files: (restored.files ?? {}) as Record<string, ImportedFile>,
    // ファイルに無ければ今の色を保つ。`restore` は渡した appState から埋める
    // ので普段は前者で決まるが、**無かったときに既定色を持ち出さない。**
    viewBackgroundColor:
      restored.appState.viewBackgroundColor ??
      (api.getAppState() as { viewBackgroundColor?: string }).viewBackgroundColor,
  };
}
