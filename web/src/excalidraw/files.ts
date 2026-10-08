import type { SceneSource } from "./image";

/**
 * 貼った画像をシーンとは別に保存する（ADR 0074、#102）。
 *
 * 保存は画像を抜いたシーン（`savedSceneJSON`）に、**サーバーがまだ持っていない
 * 画像だけ**を添えて送る。持っている画像は開いたときの `files` のキーと、保存の
 * 応答の `fileIds` で知る。**何が残って何が消えるかを手元で導かない。** その
 * 規則はサーバーが持つ。
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

/**
 * 保存に添える画像。画像の ID → Excalidraw の画像データを JSON にした文字列。
 *
 * 送るのは、シーンが参照していて、サーバーがまだ持っていない（`held` に無い）
 * もの。**キャンバスに実体の無い画像は送らない。** 貼っている途中の要素は実体が
 * 遅れて届くので、次の保存で送られる（`held` に入らないまま残るため）。
 */
export function filesToSave(
  api: SceneSource,
  held: ReadonlySet<string>,
): Record<string, string> {
  const files = (api.getFiles() ?? {}) as Record<string, unknown>;
  const out: Record<string, string> = {};

  for (const id of referencedFileIds(api.getSceneElements() as FileReferencing[])) {
    if (held.has(id)) continue;
    const file = files[id];
    if (file === undefined || file === null) continue;
    out[id] = JSON.stringify(file);
  }
  return out;
}

/**
 * 開いたボードの画像（`BoardWithFiles.files`）を、キャンバスに渡せる形にする。
 *
 * **読めない画像は落とす。** サーバーは保存のときに JSON のオブジェクトである
 * ことを確かめているので、ここに来るのは手で書き換えられた行だけ。落とした
 * 画像は、Excalidraw が空白の画像として描くので見えなくはならない。サーバーは
 * 持ったままなので、次の保存でも失われない（送らないが、参照しているかぎり
 * 消されない）。
 */
export function openedFiles(files: Record<string, string>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [id, data] of Object.entries(files)) {
    try {
      const file: unknown = JSON.parse(data);
      if (file !== null && typeof file === "object" && !Array.isArray(file)) {
        out[id] = file;
      }
    } catch {
      // 上の doc コメントのとおり、落とすだけにする。
    }
  }
  return out;
}
