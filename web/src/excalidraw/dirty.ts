import { ETOKI_NAMESPACE, type SceneElement } from "./annotation";

/**
 * シーンの中身を表す署名。保存済みのものと突き合わせて未保存かどうかを決める。
 *
 * Excalidraw の `onChange` は選択・スクロール・マウント時にも発火する。発火した
 * ことをそのまま「編集した」と扱うと、ボードを開いただけで未保存になる。
 * 要素の `version` は中身を変えたときだけ上がるので、そこを見て区別する。
 *
 * `customData` も含めるのは、注釈の付け外しが etoki 自身による書き換えであり、
 * Excalidraw が `version` を上げるとは限らないため。
 *
 * 削除済みの要素は落とす。`onChange` は削除済みを含む配列を渡す一方、
 * `getSceneElements()` は含まない。同じシーンを別の入口から見たときに署名が
 * 食い違うと、編集していないのに未保存になる。
 *
 * **`content_hash` とは別物。** あちらは 3 状態の判定用でテキストしか見ないが、
 * こちらは「保存すべき変更があるか」を見る。保存はシーン全体を書くので、図形を
 * 動かしただけでも未保存にする必要がある。揃えてはならない。
 */
export function sceneSignature(elements: readonly SceneElement[]): string {
  const parts: string[] = [];

  for (const el of elements) {
    if (el.isDeleted) continue;

    const meta = el.customData?.[ETOKI_NAMESPACE];
    parts.push(
      `${el.id}:${el.version ?? 0}:${meta === undefined ? "" : JSON.stringify(meta)}`,
    );
  }

  return parts.join("|");
}
