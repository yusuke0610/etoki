import { describe, expect, it } from "vitest";

import type { InterpretationContext } from "../../api/types";
import { contextSummary } from "./interpretationContext";

function context(over: Partial<InterpretationContext> = {}): InterpretationContext {
  return { premise: "", otherItems: [], omittedOtherItems: 0, ...over };
}

describe("contextSummary", () => {
  it("何も添えていなければ出さない", () => {
    expect(contextSummary(context())).toBeNull();
  });

  it("背景だけなら背景だけを言う", () => {
    expect(contextSummary(context({ premise: "acme/web" }))).toBe(
      "絵の外から添えたもの：ボードの背景",
    );
  });

  // 打ち切っていないのに「添えていません」と言う実装を通さない。
  it("取り切ったなら、打ち切った件数を言わない", () => {
    expect(contextSummary(context({ otherItems: [{ kind: "issue", title: "a" }] }))).toBe(
      "絵の外から添えたもの：別の絵から作ったもの 1 件",
    );
  });

  it("打ち切ったなら、添えなかった件数を言う", () => {
    expect(
      contextSummary(
        context({
          premise: "acme/web",
          otherItems: [{ kind: "epic", title: "a" }],
          omittedOtherItems: 2,
        }),
      ),
    ).toBe(
      "絵の外から添えたもの：ボードの背景・別の絵から作ったもの 1 件（上限を超えた 2 件は添えていません）",
    );
  });
});
