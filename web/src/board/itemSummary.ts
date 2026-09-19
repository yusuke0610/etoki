import type { SyncItem } from "../api/types";

/**
 * 作った / 書き換えたものの内訳を 1 行にする（ADR 0026）。
 *
 * **作成の結果と実行の履歴の両方から使う。** 同じ内訳を 2 通りに数えると、
 * 同じ run が画面の場所によって違う件数で出る。
 */

/**
 * 作成結果の内訳を 1 行にする（ADR 0026）。
 *
 * 件数だけでは、GitHub 側に何が増えたのかが分からない。更新は増えないので、
 * 「5 件を作成しました」と出しておいて実際に増えたのが 2 件、ということが起きる。
 */
export function resultSummary(items: SyncItem[]): string {
  const { created, updated } = countByAction(items);

  if (updated === 0) return `${created} 件を作成しました`;
  if (created === 0) return `${updated} 件を更新しました`;

  return `${created} 件を作成し、${updated} 件を更新しました`;
}

/**
 * 途中で失敗した run の内訳（ADR 0009 / 0026）。
 *
 * **完了したときとは言い回しを変える。** ここで伝えたいのは「もう GitHub 側に
 * 在る」ことで、何も作られていないと誤解させると再実行で重複が増える。
 */
export function partialSummary(items: SyncItem[]): string {
  const { created, updated } = countByAction(items);

  if (updated === 0) return `${created} 件は作成済み`;
  if (created === 0) return `${updated} 件は更新済み`;

  return `${created} 件は作成済み、${updated} 件は更新済み`;
}

function countByAction(items: SyncItem[]): { created: number; updated: number } {
  const updated = items.filter((it) => it.action === "updated").length;
  return { created: items.length - updated, updated };
}
