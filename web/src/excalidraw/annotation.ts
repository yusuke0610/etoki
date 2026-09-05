import type { DiagramKind, Granularity } from "../api/types";

/**
 * 注釈のメタデータを載せる customData のキー。
 *
 * Excalidraw の customData は任意の JSON を置ける共有領域なので、
 * 他のツールと衝突しないよう etoki の名前空間で包む。
 */
export const ETOKI_NAMESPACE = "etoki";

/**
 * 注釈のメタデータ。
 *
 * **Go 側（`internal/domain/scene.go` の `AnnotationMeta`）と揃える。片方だけ
 * 足すと壊れる。** フロントだけが読める形で持つと、サーバーからは見えず、
 * 解釈のプロンプトにも `content_hash` にも載らない。
 */
export type AnnotationMeta = {
  granularity: Granularity;
  /**
   * 開発者が選んだ図の種別。
   *
   * **省略可能。** 自分でフレームツールから作った注釈には種別が無い。
   * `Granularity` のように空文字を値に持たないのは、種別の語彙
   * （`DiagramKind`）を 1 つに保つため（`api/openapi.yaml` の `AnnotationStatus`）。
   */
  kind?: DiagramKind;
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
  const meta = metaOf(el);
  if (!meta) return undefined;
  return meta.granularity ?? "";
}

/**
 * 要素から図の種別を読む。注釈でないか、種別を選んでいなければ undefined。
 *
 * **「注釈ではない」と「種別を選んでいない」を分けない。** どちらも
 * 「この囲みが何の図かは分からない」で、呼び出し側の打ち手は同じ。分けると、
 * 使う側が 2 つの undefined を区別する理由を探し始める。
 */
export function kindOf(el: SceneElement): DiagramKind | undefined {
  return metaOf(el)?.kind;
}

/** 要素からメタデータを読む。注釈でなければ undefined。 */
function metaOf(el: SceneElement): AnnotationMeta | undefined {
  if (!isAnnotation(el)) return undefined;
  return el.customData?.[ETOKI_NAMESPACE] as AnnotationMeta;
}

/**
 * frame を注釈にする。すでに注釈なら粒度だけを差し替える。
 *
 * **粒度以外のメタデータは残す。** テンプレートから始めた注釈は種別を
 * 持っているので、丸ごと置き換えると粒度を選び直しただけで種別が消える。
 * 消えたことは画面に出ず、次の解釈で「何の図か」が伝わらなくなるところまで
 * 誰も気づかない。
 *
 * 残すのは**メタデータとして読める形で載っているときだけ**。他のツールが
 * 置いた値の上に粒度を足すと、etoki のメタデータでないものを etoki のものに
 * してしまう（`isAnnotation` の判定と同じ線）。
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
    const meta = metaOf(el);
    return {
      ...el,
      customData: { ...el.customData, [ETOKI_NAMESPACE]: { ...meta, granularity } },
    };
  });
}

/**
 * 注釈の図の種別を差し替える。注釈でなければ何もしない。
 *
 * **粒度と別の口にする。** どちらも同じメタデータに載るが、選ぶ場面が違う
 * （粒度は「どう分解させるか」、種別は「何の図として読ませるか」）。1 つの
 * 関数に 2 つの引数を並べると、片方だけ変えたい呼び出しがもう片方の現在値を
 * 読み直して渡すことになり、読み違えたときに黙って上書きする。
 *
 * **注釈でない frame を注釈にはしない。** 種別だけを持つ注釈という状態を
 * 作らないため。注釈にするのは `markAsAnnotation` の仕事。
 */
export function setAnnotationKind(
  elements: readonly SceneElement[],
  frameId: string,
  kind: DiagramKind | undefined,
): SceneElement[] {
  return elements.map((el) => {
    if (el.id !== frameId) return el;
    const meta = metaOf(el);
    if (!meta) return el;

    // **「指定なし」はキーごと落とす。** 空文字を置くと DiagramKind に無い値が
    // シーンに載り、契約（`AnnotationStatus.kind` は省略可能）と形が食い違う。
    const next: AnnotationMeta = { ...meta, kind };
    if (kind === undefined) delete next.kind;

    return { ...el, customData: { ...el.customData, [ETOKI_NAMESPACE]: next } };
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
