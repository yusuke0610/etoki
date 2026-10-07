import type { AnnotationStatus, SyncState } from "../../api/types";

/**
 * 並べる順。手を打つ必要があるものから。
 *
 * 変更ありは作り直すかどうかを決める材料で、未作成はまだ何もしていないもの、
 * 作成済みは見るだけでよいもの。
 */
const ORDER: SyncState[] = ["changed", "uncreated", "created"];

/**
 * 注釈を、手を打つ必要があるものから並べる。
 *
 * **並べるだけで、見出しでまとめない。** 状態はカードに出ており、位置は
 * 保存で状態が変わるたびに動く。**状態の判定には触らない。** サーバーが保存済み
 * シーンから決めた 3 状態をそのまま使う。
 *
 * **同じ状態の中では受け取った順を保つ。** 並べ替えると、同じ状態の注釈が
 * どこへ行ったか追えなくなる。**元の配列は変えない。**
 */
export function sortByState(annotations: AnnotationStatus[]): AnnotationStatus[] {
  return ORDER.flatMap((state) => annotations.filter((a) => a.state === state));
}
