import { exportToBlob } from "@excalidraw/excalidraw";

import type { AnnotationImage } from "../api/types";
import { isAnnotation, type SceneElement } from "./annotation";

/**
 * 書き出す画像の長辺の上限（px）。
 *
 * バイト数の上限はサーバーが持つ（ADR 0018）。ここで抑えるのは解像度だけで、
 * 何バイトになったかの判定は持たない。両方に持たせると、片方だけ変えたときに
 * 「フロントは通すがサーバーが弾く」がどちらの言い分か分からなくなる。
 */
export const MAX_IMAGE_DIMENSION = 2048;

/**
 * Excalidraw からシーンを読むのに要るものだけを取り出した形。
 *
 * 画像の書き出しと、持ち出し・取り込み（`transfer.ts`）が共有する。
 */
export type SceneSource = {
  getSceneElements: () => readonly unknown[];
  getAppState: () => unknown;
  getFiles: () => unknown;
};

/** exportToBlob のうち、ここで使う部分だけの形。 */
export type ExportToBlob = (opts: {
  elements: readonly never[];
  appState?: never;
  files: never;
  maxWidthOrHeight?: number;
  exportingFrame?: never;
  mimeType?: string;
}) => Promise<Blob>;

/**
 * 注釈の frame 要素を返す。注釈でなければ undefined。
 *
 * 画像は frame の範囲だけを写す。囲みの外にある別の話題まで一緒に渡すと、
 * 「囲んだ範囲を解釈する」という約束が崩れる（ADR 0018）。
 */
export function findAnnotationFrame(
  elements: readonly SceneElement[],
  annotationId: string,
): SceneElement | undefined {
  return elements.find((el) => el.id === annotationId && isAnnotation(el));
}

/**
 * バイト列を契約の画像に詰める。
 *
 * `data` は base64。契約が `format: byte` なので、サーバーはこれを復号して
 * バイト列として受け取る。
 */
export function toAnnotationImage(bytes: Uint8Array): AnnotationImage {
  let binary = "";
  // btoa は文字列しか受け取らない。大きな配列を一度に展開すると引数の上限に
  // 当たるので、区切って詰める。
  const chunk = 0x8000;
  for (let i = 0; i < bytes.length; i += chunk) {
    binary += String.fromCharCode(...bytes.subarray(i, i + chunk));
  }

  return { mediaType: "image/png", data: btoa(binary) };
}

/**
 * 注釈範囲を PNG に書き出す。
 *
 * 写すのは**画面上の状態**である。保存済みシーンとは限らない。テキストは
 * 保存済みシーンから取るので、両者を揃えるのは呼び出し側の責務にしてある
 * （ADR 0018。UI は未保存のあいだ解釈させない）。
 *
 * 注釈が見つからなければ undefined を返す。画像なしでも解釈は成立するので、
 * ここで解釈そのものを止めない。
 *
 * exportBlob は差し替えられるようにしてある。書き出しそのものは canvas が要り
 * jsdom では動かないが、frame を渡していることは確かめる必要があるため。
 */
export async function exportAnnotationImage(
  api: SceneSource,
  annotationId: string,
  exportBlob: ExportToBlob = exportToBlob as ExportToBlob,
): Promise<AnnotationImage | undefined> {
  const elements = api.getSceneElements() as readonly SceneElement[];
  const frame = findAnnotationFrame(elements, annotationId);
  if (!frame) return undefined;

  const blob = await exportBlob({
    elements: elements as readonly never[],
    appState: api.getAppState() as never,
    files: api.getFiles() as never,
    // frame を渡すと、その範囲だけが写る。要素を自分で絞り込まないのは、
    // 境界にまたがる要素の帰属判定を持ちたくないため。frame を選んだ理由が
    // そこにある。
    exportingFrame: frame as never,
    maxWidthOrHeight: MAX_IMAGE_DIMENSION,
    mimeType: "image/png",
  });

  return toAnnotationImage(new Uint8Array(await blob.arrayBuffer()));
}
