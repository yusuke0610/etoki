import type { AnnotationStatus, SyncState } from "../api/types";

/**
 * 状態でまとめた注釈の組。
 *
 * **まとめるのは見せ方だけで、状態の判定には触らない。** 状態はサーバーが
 * 保存済みシーンから決めたもの（3 状態、`internal/CLAUDE.md`）をそのまま使う。
 */
export type StateGroup = {
  state: SyncState;
  annotations: AnnotationStatus[];
};

/**
 * 組を並べる順。手を打つ必要があるものから並べる。
 *
 * 変更ありは作り直すかどうかを決める材料で、未作成はまだ何もしていないもの、
 * 作成済みは見るだけでよいもの。
 */
const ORDER: SyncState[] = ["changed", "uncreated", "created"];

/**
 * 注釈を状態ごとにまとめる。
 *
 * **組の中の並びは変えない。** 並べ替えると、同じ状態の注釈がどこへ行ったか
 * 追えなくなる。**1 件も無い組は出さない。** 空の見出しが並ぶと、本当に何か
 * あるときに気づけない。
 */
export function groupByState(annotations: AnnotationStatus[]): StateGroup[] {
  return ORDER.map((state) => ({
    state,
    annotations: annotations.filter((a) => a.state === state),
  })).filter((g) => g.annotations.length > 0);
}
