import { describe, expect, it } from "vitest";

import {
  granularityOf,
  isAnnotation,
  markAsAnnotation,
  selectableFrameIds,
  unmarkAnnotation,
  type SceneElement,
} from "./annotation";

const plainFrame: SceneElement = { id: "f1", type: "frame", name: "ただの枠" };
const annotation: SceneElement = {
  id: "f2",
  type: "frame",
  name: "決済まわり",
  customData: { etoki: { granularity: "epic" } },
};
const text: SceneElement = { id: "t1", type: "text" };

describe("isAnnotation", () => {
  // ブレスト中にユーザーが使った frame を注釈と誤認しないこと。
  // この規則はバックエンドの internal/domain と一致していなければならない。
  it("customData.etoki を持たない frame は注釈ではない", () => {
    expect(isAnnotation(plainFrame)).toBe(false);
  });

  it("customData.etoki を持つ frame は注釈", () => {
    expect(isAnnotation(annotation)).toBe(true);
  });

  it("frame でなければ注釈にならない", () => {
    expect(isAnnotation({ ...text, customData: { etoki: { granularity: "" } } })).toBe(
      false,
    );
  });

  it("削除済みは注釈として扱わない", () => {
    expect(isAnnotation({ ...annotation, isDeleted: true })).toBe(false);
  });
});

describe("markAsAnnotation", () => {
  it("frame に粒度を付ける", () => {
    const got = markAsAnnotation([plainFrame, text], "f1", "issue");

    expect(granularityOf(got[0]!)).toBe("issue");
    expect(isAnnotation(got[0]!)).toBe(true);
  });

  it("すでに注釈なら粒度だけ差し替える", () => {
    const got = markAsAnnotation([annotation], "f2", "issue");

    expect(granularityOf(got[0]!)).toBe("issue");
  });

  it("customData の他のキーを壊さない", () => {
    const withOther: SceneElement = {
      ...plainFrame,
      customData: { otherTool: { keep: true } },
    };

    const got = markAsAnnotation([withOther], "f1", "epic");

    expect(got[0]!.customData?.otherTool).toEqual({ keep: true });
    expect(granularityOf(got[0]!)).toBe("epic");
  });

  // Excalidraw は要素の同一性で再描画を判断するため、その場で書き換えると
  // 更新が反映されないことがある。
  it("元の配列と要素を書き換えない", () => {
    const elements = [plainFrame, text];
    const got = markAsAnnotation(elements, "f1", "epic");

    expect(elements[0]!.customData).toBeUndefined();
    expect(got).not.toBe(elements);
    expect(got[0]).not.toBe(elements[0]);
    // 対象外の要素は同じ参照のまま返す（不要な再描画を避けるため）
    expect(got[1]).toBe(elements[1]);
  });

  it("frame 以外は対象にしない", () => {
    const got = markAsAnnotation([text], "t1", "epic");

    expect(isAnnotation(got[0]!)).toBe(false);
  });

  it("該当 ID がなければそのまま返す", () => {
    const got = markAsAnnotation([plainFrame], "no-such-id", "epic");

    expect(got[0]).toBe(plainFrame);
  });
});

describe("unmarkAnnotation", () => {
  it("注釈の指定を外すが frame は残す", () => {
    const got = unmarkAnnotation([annotation], "f2");

    expect(isAnnotation(got[0]!)).toBe(false);
    expect(got[0]!.type).toBe("frame");
    expect(got[0]!.name).toBe("決済まわり");
  });

  it("customData の他のキーは残す", () => {
    const withOther: SceneElement = {
      ...annotation,
      customData: { etoki: { granularity: "epic" }, otherTool: { keep: true } },
    };

    const got = unmarkAnnotation([withOther], "f2");

    expect(got[0]!.customData?.otherTool).toEqual({ keep: true });
    expect(got[0]!.customData?.etoki).toBeUndefined();
  });
});

describe("granularityOf", () => {
  it("注釈でなければ undefined", () => {
    expect(granularityOf(plainFrame)).toBeUndefined();
  });

  it("granularity が無ければ指定なしとして空文字", () => {
    expect(granularityOf({ ...plainFrame, customData: { etoki: {} } })).toBe("");
  });
});

describe("selectableFrameIds", () => {
  it("選択中の frame だけを返す", () => {
    const got = selectableFrameIds([plainFrame, annotation, text], {
      f1: true,
      t1: true,
    });

    expect(got).toEqual(["f1"]);
  });

  it("削除済みの frame は除く", () => {
    const got = selectableFrameIds([{ ...plainFrame, isDeleted: true }], { f1: true });

    expect(got).toEqual([]);
  });

  it("選択が無ければ空", () => {
    expect(selectableFrameIds([plainFrame], {})).toEqual([]);
  });
});
