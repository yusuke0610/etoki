import type { InterpretedItem } from "../api/boards";

/** 表示用の epic とその配下の issue。 */
export type InterpretationGroup = {
  /** epic。epic に属さない issue をまとめるグループでは undefined。 */
  epic?: InterpretedItem;
  issues: InterpretedItem[];
};

/**
 * 解釈結果を epic ← issue の 2 階層に組み直す。
 *
 * バックエンドは階層を持たない配列で返す（parentLocalId による擬似リンク）。
 * 表示のたびに組み立て直すと親子の解決が UI に散らばるので、ここに寄せる。
 *
 * epic に属さない issue は末尾のグループにまとめる。落とすと、開発者が
 * 「これから作られるもの」の全体を見られなくなる。
 */
export function groupByEpic(items: InterpretedItem[]): InterpretationGroup[] {
  const groups = new Map<string, InterpretationGroup>();
  const orphans: InterpretedItem[] = [];

  // epic を出現順に並べる。issue を先に見ても入れ先があるようにする。
  for (const item of items) {
    if (item.kind === "epic") {
      groups.set(item.localId, { epic: item, issues: [] });
    }
  }

  for (const item of items) {
    if (item.kind === "epic") continue;

    const group = item.parentLocalId ? groups.get(item.parentLocalId) : undefined;
    if (group) {
      group.issues.push(item);
    } else {
      // 親が指定されていない、または指す epic が無い。後者は検証を通って
      // いれば起きないが、落とさずに見せる。
      orphans.push(item);
    }
  }

  const out = [...groups.values()];
  if (orphans.length > 0) {
    out.push({ issues: orphans });
  }
  return out;
}
