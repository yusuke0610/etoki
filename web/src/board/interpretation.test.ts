import { describe, expect, it } from "vitest";

import type { InterpretedItem } from "../api/boards";
import { groupByEpic, type InterpretationGroup } from "./interpretation";

/** 添字アクセスを型で絞る。件数は各テストで先に確認している。 */
function at(groups: InterpretationGroup[], i: number): InterpretationGroup {
  const g = groups[i];
  if (!g) throw new Error(`groups[${i}] が無い`);
  return g;
}

function epic(localId: string, title = localId): InterpretedItem {
  return { localId, kind: "epic", title, body: "" };
}

function issue(localId: string, parentLocalId?: string): InterpretedItem {
  return { localId, kind: "issue", title: localId, body: "", parentLocalId };
}

describe("groupByEpic", () => {
  it("issue を親の epic に入れる", () => {
    const groups = groupByEpic([epic("e1"), issue("i1", "e1"), issue("i2", "e1")]);

    expect(groups).toHaveLength(1);
    expect(at(groups, 0).epic?.localId).toBe("e1");
    expect(at(groups, 0).issues.map((i) => i.localId)).toEqual(["i1", "i2"]);
  });

  it("epic の出現順を保つ", () => {
    const groups = groupByEpic([epic("e1"), epic("e2"), issue("i1", "e2")]);

    expect(groups.map((g) => g.epic?.localId)).toEqual(["e1", "e2"]);
    expect(at(groups, 0).issues).toHaveLength(0);
    expect(at(groups, 1).issues.map((i) => i.localId)).toEqual(["i1"]);
  });

  it("issue が先に並んでいても親に入る", () => {
    const groups = groupByEpic([issue("i1", "e1"), epic("e1")]);

    expect(groups).toHaveLength(1);
    expect(at(groups, 0).issues.map((i) => i.localId)).toEqual(["i1"]);
  });

  // 落とすと「これから作られるもの」の全体が見えなくなる。
  it("親のない issue を末尾にまとめる", () => {
    const groups = groupByEpic([epic("e1"), issue("i1", "e1"), issue("i2")]);

    expect(groups).toHaveLength(2);
    expect(at(groups, 1).epic).toBeUndefined();
    expect(at(groups, 1).issues.map((i) => i.localId)).toEqual(["i2"]);
  });

  it("親が存在しない issue も落とさない", () => {
    const groups = groupByEpic([epic("e1"), issue("i1", "e9")]);

    expect(groups).toHaveLength(2);
    expect(at(groups, 1).issues.map((i) => i.localId)).toEqual(["i1"]);
  });

  it("epic が無く issue だけなら 1 グループ", () => {
    const groups = groupByEpic([issue("i1"), issue("i2")]);

    expect(groups).toHaveLength(1);
    expect(at(groups, 0).epic).toBeUndefined();
    expect(at(groups, 0).issues).toHaveLength(2);
  });

  it("空なら空", () => {
    expect(groupByEpic([])).toEqual([]);
  });
});
