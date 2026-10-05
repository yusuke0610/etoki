import { describe, expect, it } from "vitest";

import type { AnnotationStatus } from "../../api/types";
import { sortByState } from "./annotationOrder";

function annotation(id: string, state: AnnotationStatus["state"]): AnnotationStatus {
  return { id, name: id, granularity: "", state };
}

describe("sortByState", () => {
  it("変更あり・未作成・作成済みの順に並べる", () => {
    const sorted = sortByState([
      annotation("a", "created"),
      annotation("b", "uncreated"),
      annotation("c", "changed"),
    ]);

    expect(sorted.map((a) => a.id)).toEqual(["c", "b", "a"]);
  });

  // 組の中まで並べ替えると、同じ状態の注釈がどこへ行ったか追えなくなる。
  it("同じ状態の中では受け取った順を保つ", () => {
    const sorted = sortByState([
      annotation("x", "uncreated"),
      annotation("y", "created"),
      annotation("z", "uncreated"),
    ]);

    expect(sorted.map((a) => a.id)).toEqual(["x", "z", "y"]);
  });

  // 名前の無い注釈の番号は受け取った順で振る（annotationLabels）。呼び出し側が
  // 元の配列で採番できるよう、渡した配列を書き換えない。
  it("渡した配列は書き換えない", () => {
    const input = [annotation("a", "created"), annotation("b", "changed")];
    sortByState(input);

    expect(input.map((a) => a.id)).toEqual(["a", "b"]);
  });

  it("空でも落ちない", () => {
    expect(sortByState([])).toEqual([]);
  });
});
