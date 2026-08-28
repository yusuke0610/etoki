import type { Granularity } from "../api/types";

/**
 * 注釈のメタデータを載せる customData のキー。
 *
 * Excalidraw の customData は任意の JSON を置ける共有領域なので、
 * 他のツールと衝突しないよう etoki の名前空間で包む。
 */
export const ETOKI_NAMESPACE = "etoki";

export type AnnotationMeta = {
  granularity: Granularity;
};

/** 最低限の要素形。ライブラリの型に依存せず純粋にテストするために持つ。 */
export type SceneElement = {
  id: string;
  type: string;
  isDeleted?: boolean;
  name?: string | null;
  customData?: Record<string, unknown>;
  /** シーン座標での位置と大きさ。注釈の枠を重ねる位置の計算に使う（annotationOverlay.ts）。 */
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  /** Excalidraw が中身を変えるたびに上げる番号。未保存の判定に使う（dirty.ts）。 */
  version?: number;
};

/**
 * 注釈かどうかを判定する。
 *
 * frame であることに加えて customData.etoki の存在を条件にしているのは、
 * ブレスト中にユーザーが自分の用途で frame を使っても注釈と誤認しないため。
 * この規則はバックエンド（internal/domain）と一致させる必要がある。
 */
export function isAnnotation(el: SceneElement): boolean {
  return el.type === "frame" && !el.isDeleted && hasMeta(el);
}

/**
 * customData.etoki が「メタデータとして読める形」で載っているか。
 *
 * **キーの有無ではなく中身の形を見る。** customData は他のツールも書ける共有
 * 領域なので、etoki 以外が置いた値が来うる。Go 側は AnnotationMeta 構造体と
 * して読むので、オブジェクト以外はメタデータにならない。キーの有無だけを見ると
 * ここだけが注釈と認め、画面に出る注釈がサーバーの解釈対象とずれる。
 *
 * 配列を外すのは、JavaScript では配列も `typeof === "object"` になるため。
 */
function hasMeta(el: SceneElement): boolean {
  const meta = el.customData?.[ETOKI_NAMESPACE];
  return typeof meta === "object" && meta !== null && !Array.isArray(meta);
}

/** 要素から粒度を読む。注釈でなければ undefined。 */
export function granularityOf(el: SceneElement): Granularity | undefined {
  if (!isAnnotation(el)) return undefined;
  const meta = el.customData?.[ETOKI_NAMESPACE] as AnnotationMeta;
  return meta.granularity ?? "";
}

/**
 * frame を注釈にする。すでに注釈なら粒度だけを差し替える。
 *
 * 要素は書き換えずに新しい配列を返す。Excalidraw は要素の同一性で再描画を
 * 判断するため、その場で書き換えると更新が反映されないことがある。
 */
export function markAsAnnotation(
  elements: readonly SceneElement[],
  frameId: string,
  granularity: Granularity,
): SceneElement[] {
  return elements.map((el) => {
    if (el.id !== frameId || el.type !== "frame") return el;
    return {
      ...el,
      customData: { ...el.customData, [ETOKI_NAMESPACE]: { granularity } },
    };
  });
}

/** 注釈の指定を外す。frame 自体は残す。 */
export function unmarkAnnotation(
  elements: readonly SceneElement[],
  frameId: string,
): SceneElement[] {
  return elements.map((el) => {
    if (el.id !== frameId) return el;
    const rest = { ...el.customData };
    delete rest[ETOKI_NAMESPACE];
    return { ...el, customData: rest };
  });
}

/** 選択中の frame 1 つ。名前はパネルが見出しを出すために持つ。 */
export type SelectableFrame = {
  id: string;
  /** frame のラベル。Excalidraw の既定は null なので空文字に寄せてある。 */
  name: string;
};

/**
 * 選択中の要素のうち、注釈にできる frame を返す。
 *
 * 注釈は frame にしか付けられないので、UI 側で「何を選べばよいか」を
 * 案内できるようにここで絞り込む。ID だけでなく名前も返すのは、複数を選んだ
 * ときにパネルの項目が区別できなければ案内にならないため（ADR 0022）。
 */
export function selectableFrames(
  elements: readonly SceneElement[],
  selectedIds: Readonly<Record<string, boolean>>,
): SelectableFrame[] {
  return elements
    .filter((el) => el.type === "frame" && !el.isDeleted && selectedIds[el.id])
    .map((el) => ({ id: el.id, name: el.name ?? "" }));
}

/** シーンにいま在る frame の ID。パネルが「押しても飛べない」項目を出すために使う。 */
export function frameIds(elements: readonly SceneElement[]): string[] {
  return elements.filter((el) => el.type === "frame" && !el.isDeleted).map((el) => el.id);
}
