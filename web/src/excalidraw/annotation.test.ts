import { describe, expect, it } from "vitest";

import {
  frameIds,
  granularityOf,
  isAnnotation,
  kindOf,
  markAsAnnotation,
  setAnnotationKind,
  selectableFrames,
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
/** ひな形から始めた注釈。種別を持っている。 */
const templated: SceneElement = {
  id: "f3",
  type: "frame",
  name: "シーケンス図",
  customData: { etoki: { granularity: "", kind: "sequence" } },
};

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

  // **オブジェクト以外は注釈にしない。** customData は他のツールも書ける共有
  // 領域なので、etoki 以外が置いた値が来うる。キーの有無だけを見ると、Go が
  // AnnotationMeta として読めない形をここだけが注釈と認めてしまう。
  it.each([
    ["文字列", "x"],
    ["数値", 5],
    ["真偽値", true],
    ["配列", []],
  ])("customData.etoki が%sの frame は注釈ではない", (_name, meta) => {
    expect(isAnnotation({ ...plainFrame, customData: { etoki: meta } })).toBe(false);
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

  // ひな形から始めた注釈で粒度を選び直しただけで種別が消えると、次の解釈で
  // 「何の図か」が伝わらなくなる。消えたことは画面に出ないので、気づくのは
  // LLM の出力を見たとき。
  it("粒度を差し替えても種別を消さない", () => {
    const got = markAsAnnotation([templated], "f3", "epic");

    expect(granularityOf(got[0]!)).toBe("epic");
    expect(kindOf(got[0]!)).toBe("sequence");
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

describe("kindOf", () => {
  it("ひな形から始めた注釈は種別を返す", () => {
    expect(kindOf(templated)).toBe("sequence");
  });

  // **「注釈ではない」と「種別を選んでいない」を分けない。** どちらも
  // 「何の図かは分からない」で、呼び出し側の打ち手は同じ。
  it("注釈でないか、種別を選んでいなければ undefined", () => {
    expect(kindOf(plainFrame)).toBeUndefined();
    expect(kindOf(annotation)).toBeUndefined();
  });
});

describe("setAnnotationKind", () => {
  it("種別を差し替える", () => {
    const got = setAnnotationKind([annotation], "f2", "er");

    expect(kindOf(got[0]!)).toBe("er");
    // 粒度は触らない。同じメタデータに載るが、選ぶ場面が違う。
    expect(granularityOf(got[0]!)).toBe("epic");
  });

  // 空文字を置くと DiagramKind に無い値がシーンに載り、契約
  // （AnnotationStatus.kind は省略可能）と形が食い違う。
  it("指定なしはキーごと落とす", () => {
    const got = setAnnotationKind([templated], "f3", undefined);

    expect(kindOf(got[0]!)).toBeUndefined();
    expect(got[0]!.customData?.etoki).not.toHaveProperty("kind");
    expect(isAnnotation(got[0]!)).toBe(true);
  });

  // 種別だけを持つ注釈という状態を作らない。注釈にするのは markAsAnnotation。
  it("注釈でない frame は注釈にしない", () => {
    const got = setAnnotationKind([plainFrame], "f1", "er");

    expect(got[0]).toBe(plainFrame);
    expect(isAnnotation(got[0]!)).toBe(false);
  });
});

describe("selectableFrames", () => {
  it("選択中の frame だけを名前つきで返す", () => {
    const got = selectableFrames([plainFrame, annotation, text], {
      f1: true,
      t1: true,
    });

    expect(got).toEqual([{ id: "f1", name: "ただの枠" }]);
  });

  // Excalidraw の frame は既定で name が null。パネルはこれを空文字として
  // 受け取り、見出しの決め方を 1 箇所（annotationLabel）に寄せる。
  it("名前が無ければ空文字にする", () => {
    expect(selectableFrames([{ id: "f1", type: "frame" }], { f1: true })).toEqual([
      { id: "f1", name: "" },
    ]);
    expect(
      selectableFrames([{ id: "f1", type: "frame", name: null }], { f1: true }),
    ).toEqual([{ id: "f1", name: "" }]);
  });

  it("削除済みの frame は除く", () => {
    const got = selectableFrames([{ ...plainFrame, isDeleted: true }], { f1: true });

    expect(got).toEqual([]);
  });

  it("選択が無ければ空", () => {
    expect(selectableFrames([plainFrame], {})).toEqual([]);
  });
});

describe("frameIds", () => {
  it("注釈かどうかに関係なく frame を返す", () => {
    expect(frameIds([plainFrame, annotation, text])).toEqual(["f1", "f2"]);
  });

  it("削除済みは除く", () => {
    expect(frameIds([{ ...plainFrame, isDeleted: true }])).toEqual([]);
  });
});
