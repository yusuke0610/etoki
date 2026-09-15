import { describe, expect, it } from "vitest";

import type { AnnotationStatus } from "../api/types";
import { groupByState } from "./annotationGroups";

function annotation(id: string, state: AnnotationStatus["state"]): AnnotationStatus {
  return { id, name: id, granularity: "", state };
}

describe("groupByState", () => {
  // 手を打つ必要があるものから並べる。変更ありは作り直すか決める材料、
  // 未作成はまだ何もしていないもの、作成済みは見るだけでよいもの。
  it("変更あり・未作成・作成済みの順にまとめる", () => {
    const groups = groupByState([
      annotation("a", "created"),
      annotation("b", "uncreated"),
      annotation("c", "changed"),
    ]);

    expect(groups.map((g) => g.state)).toEqual(["changed", "uncreated", "created"]);
    expect(groups.map((g) => g.annotations.map((a) => a.id))).toEqual([
      ["c"],
      ["b"],
      ["a"],
    ]);
  });

  // 組の中まで並べ替えると、同じ状態の注釈がどこへ行ったか追えなくなる。
  it("同じ状態の中では受け取った順を保つ", () => {
    const groups = groupByState([
      annotation("x", "uncreated"),
      annotation("y", "created"),
      annotation("z", "uncreated"),
    ]);

    expect(groups[0]).toEqual({
      state: "uncreated",
      annotations: [annotation("x", "uncreated"), annotation("z", "uncreated")],
    });
  });

  // 空の見出しが並ぶと、本当に何かあるときに気づけない（ADR 0046 と同じ理由）。
  it("1 件も無い状態の組は出さない", () => {
    expect(groupByState([annotation("a", "created")]).map((g) => g.state)).toEqual([
      "created",
    ]);
    expect(groupByState([])).toEqual([]);
  });
});
