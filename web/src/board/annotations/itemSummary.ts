import type { SyncItem } from "../../api/types";
import type { RenamedEpic, SelectedActions } from "./interpretationDraft";

/**
 * 作る / 書き換えるものの内訳を 1 行にする（ADR 0026）。
 *
 * **作った結果は、作成の結果と実行の履歴の両方から使う。** 同じ内訳を 2 通りに
 * 数えると、同じ run が画面の場所によって違う件数で出る。押す前の内訳
 * （`creationNotice`）もここに置く。
 */

/**
 * 押す直前に帯で読む文（#289）。何件を作り、何件を書き換えるか。
 *
 * **作成と更新を同じ言い方に畳まない。** どちらも取り消せないが、更新は前の内容を
 * 消す（ADR 0026）。項目ごとの「更新」の印は、本文が長いと帯から見える範囲に
 * 入らないので、帯の文でも分けて言う。
 *
 * 取り残しは本文にも一覧で出すが、帯は下端に固定しているので件数をここにも
 * 添える（作成ボタンの手前に出す約束、`web/CLAUDE.md`）。
 */
export function creationNotice(
  { create, update }: SelectedActions,
  target: string,
  leftBehind: number,
): string {
  const tail = leftBehind > 0 ? `前回作った ${leftBehind} 件は書き換わりません。` : "";

  if (update === 0) {
    return `作成は取り消せません。選んだ ${create} 件を ${target} に作ります。${tail}`;
  }
  if (create === 0) {
    return `更新は取り消せません。${target} の draft issue ${update} 件を書き換えます。${tail}`;
  }
  return `作成も更新も取り消せません。${target} に ${create} 件を作り、${update} 件を書き換えます。${tail}`;
}

/**
 * 触らないほうの issue が、親の epic を失うことの案内（#298）。
 *
 * epic のタイトルを書き換えると、今回送らない issue の `Parent`（親のタイトル、
 * ADR 0006）は前のタイトルを指したまま残る。**親を失うことは黙って起こさない**
 * （ADR 0024）。張り直すかは開発者が選ぶ。今回の絵解きで選び直せば張り直される。
 *
 * どの issue がどの epic に属していたかは結びつけない（`renamedEpics`）ので、
 * 「属していたものは」と条件で言う。タイトルが変わる epic が無いか、触らない
 * ものに issue が無ければ、親を失うものは無いので何も言わない。
 */
export function leftBehindParentNote(
  renamed: RenamedEpic[],
  leftBehind: SyncItem[],
): string | undefined {
  if (renamed.length === 0 || !leftBehind.some((it) => it.kind === "issue")) {
    return undefined;
  }

  const changes = renamed
    .map(({ from, to }) => `epic「${from}」のタイトルを「${to}」に`)
    .join("、");
  const which = renamed.length === 1 ? "この epic" : "これらの epic";
  const before = renamed.map(({ from }) => `「${from}」`).join("・");
  return (
    `今回、${changes}書き換えます。` +
    `上の issue のうち、${which} に属していたものは、` +
    `GitHub の Parent が${before}を指したまま残ります。`
  );
}

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

/**
 * 何件作って何件更新したか。
 *
 * **届いたか分からないものは数えない**（ADR 0056）。混ぜると「2 件は作成済み」が
 * 嘘になる。そちらは件数ではなく 1 件ずつ `UnconfirmedItems` に出す。確かめ方が
 * 違う（GitHub を見にいく）ので、同じ数に足し込まない。
 */
function countByAction(items: SyncItem[]): { created: number; updated: number } {
  const confirmed = items.filter((it) => it.confirmed);
  const updated = confirmed.filter((it) => it.action === "updated").length;
  return { created: confirmed.length - updated, updated };
}
