/**
 * 貼った画像をシーンとは別に保存するための、画像の参照の読み方（ADR 0074、#102）。
 */

/** 画像を参照しうる要素。参照の判定に要るフィールドだけを型に出す。 */
export type FileReferencing = {
  isDeleted?: boolean;
  fileId?: string | null;
};

/**
 * シーンが参照している画像の ID を、要素の並び順で重複なく返す。
 *
 * **参照しているのは、削除されていない要素の fileId。** 要素の種類は見ない。
 * Excalidraw が保存する画像を選ぶとき（`serializeAsJSON` の
 * filterOutDeletedFiles）と同じ規則にしてある。
 *
 * 規則は Go 側（`internal/domain/scene.go` の `FileIDs`）と揃える。**ずれると、
 * 送らなかった画像をサーバーが参照ありとして待つか、送った画像を参照なしとして
 * 400 で弾く。** 判定対象は `testdata/file-reference-rule.json` に置いて両方から
 * 読ませている。
 */
export function referencedFileIds(elements: readonly FileReferencing[]): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  for (const element of elements) {
    const id = element.fileId;
    if (element.isDeleted || typeof id !== "string" || id === "" || seen.has(id))
      continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}
