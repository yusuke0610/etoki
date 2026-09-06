import { ETOKI_NAMESPACE, type SceneElement } from "./annotation";

const SEPARATE_SIGNATURE_FIELDS: ReadonlySet<string> = new Set([
  "id",
  "version",
  "versionNonce",
  "updated",
  "index",
  "isDeleted",
]);

/**
 * シーンの中身を表す署名。保存済みのものと突き合わせて未保存かどうかを決める。
 *
 * Excalidraw の `onChange` は選択・スクロール・マウント時にも発火する。発火した
 * ことをそのまま「編集した」と扱うと、ボードを開いただけで未保存になる。
 * 要素の保存される中身を正規化して比べるので、選択状態は入らない一方、取り込んだ
 * 要素が同じ `id` / `version` を持っていても位置やテキストの差は残る。
 *
 * `customData` も含めるのは、注釈の付け外しが etoki 自身による書き換えであり、
 * Excalidraw が `version` を上げるとは限らないため。
 * 画像の `fileId` も含める。画像の実体は要素とは別の files にあり、取り込み時に
 * 同じ ID の別画像を避けるため参照だけを振り直すことがあるため。
 *
 * **背景色も含める。** あれは `appState` にあって要素には現れないが、保存は
 * シーン全体（`serializeAsJSON`）を書くので、背景色だけを変えた状態も保存すべき
 * 変更になる。要素だけを見ると、色を変えただけのキャンバスが「未保存ではない」
 * と出て、確認なしで離れられる（ADR 0021 / 0044）。**引数は省略可にしない。**
 * 入口ごとに渡し忘れると、同じシーンの署名が食い違って編集していないのに未保存に
 * なる。必須にしてあれば、入口が増えた日に `tsc` が落ちる。
 *
 * 削除済みの要素は落とす。`onChange` は削除済みを含む配列を渡す一方、
 * `getSceneElements()` は含まない。同じシーンを別の入口から見たときに署名が
 * 食い違うと、編集していないのに未保存になる。
 *
 * **`content_hash` とは別物。** あちらは 3 状態の判定用でテキストしか見ないが、
 * こちらは「保存すべき変更があるか」を見る。保存はシーン全体を書くので、図形を
 * 動かしただけでも未保存にする必要がある。揃えてはならない。
 */
export function sceneSignature(
  elements: readonly SceneElement[],
  viewBackgroundColor: string | undefined,
): string {
  // 要素ごとの署名。**区切り文字で連結せず、JSON の構造で分ける。** 背景色も
  // 要素の id もファイルから来るので（取り込み、ADR 0045）、`|` や `:` を含む
  // 値を渡されうる。連結すると、違うシーンが同じ署名に化けて未保存が消える。
  const parts: [string, number, string, string, string][] = [];

  for (const el of elements) {
    if (el.isDeleted) continue;

    const meta = el.customData?.[ETOKI_NAMESPACE];
    // メタデータは文字列にしてから入れる。「無い」（undefined）と「null が
    // 載っている」を別物として残すため。JSON.stringify(undefined) は値を落とす。
    parts.push([
      el.id,
      el.version ?? 0,
      meta === undefined ? "" : JSON.stringify(meta),
      el.type === "image" ? (el.fileId ?? "") : "",
      normalizedPersistedContent(el),
    ]);
  }

  return JSON.stringify([viewBackgroundColor ?? "", parts]);
}

/** 保存される要素の中身を、オブジェクトのキー順に依存しない JSON にする。 */
function normalizedPersistedContent(el: SceneElement): string {
  // 版管理と並びのための内部値は別に扱う。id / version は parts の先頭にあり、
  // versionNonce / updated は version と同じ変更の管理値。index は Excalidraw が
  // マウント時にも振り直しうるので、要素の配列順そのものを署名に残す。
  const content = Object.fromEntries(
    Object.entries(el).filter(([key]) => !SEPARATE_SIGNATURE_FIELDS.has(key)),
  );

  // Excalidraw の保存処理は line / arrow の編集中の点を null にして書く。同じ内容を
  // 署名にも使い、保存されない一時値だけで未保存にならないようにする。
  const persisted =
    el.type === "line" || el.type === "arrow"
      ? { ...content, lastCommittedPoint: null }
      : content;
  return JSON.stringify(normalizeJSON(persisted));
}

/** JSON の配列順は保ち、オブジェクトのキーだけを再帰的に揃える。 */
function normalizeJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(normalizeJSON);
  if (value === null || typeof value !== "object") return value;

  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>)
      .sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0))
      .map(([key, nested]) => [key, normalizeJSON(nested)]),
  );
}
