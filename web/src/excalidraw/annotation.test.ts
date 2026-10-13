import { describe, expect, it } from "vitest";

import {
  annotationMetas,
  annotationNames,
  frameIds,
  granularityOf,
  isAnnotation,
  kindOf,
  markAsAnnotation,
  setAnnotationKind,
  setAnnotationName,
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

// Excalidraw は要素が変わったかを version で見る。上げないと「元に戻す」に
// 積まれず、戻すと直前に描いたものが消える（#144）。
describe("注釈の付け外しは version と versionNonce を変える", () => {
  const versioned: SceneElement = { ...annotation, version: 3 };

  it("注釈にする", () => {
    const [el] = markAsAnnotation(
      [{ ...plainFrame, version: 3, versionNonce: 11 }],
      "f1",
      "epic",
    );
    expect(el?.version).toBe(4);
    // 履歴が見ているのはこちら。version だけ上げても積まれない。
    expect(el?.versionNonce).not.toBe(11);
  });

  it("注釈を外す", () => {
    const [el] = unmarkAnnotation([versioned], versioned.id);
    expect(el?.version).toBe(4);
  });

  it("種別を変える", () => {
    const [el] = setAnnotationKind([versioned], versioned.id, "sequence");
    expect(el?.version).toBe(4);
  });

  // 触っていない要素まで上げると、関係ない要素が変わったことになる。
  it("対象でない要素は上げない", () => {
    const other: SceneElement = { ...text, version: 7 };
    const [, el] = markAsAnnotation([{ ...plainFrame, version: 3 }, other], "f1", "epic");
    expect(el).toBe(other);
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

describe("annotationMetas", () => {
  // 注釈の詳細の選択欄が出す値。保存済みの値ではなく、キャンバスにいま在る値。
  it("注釈の frame だけを、粒度と種別で引けるようにする", () => {
    expect(annotationMetas([plainFrame, annotation, text, templated])).toEqual({
      f2: { granularity: "epic", kind: undefined },
      f3: { granularity: "", kind: "sequence" },
    });
  });

  it("粒度が無ければ指定なし、削除済みは除く", () => {
    expect(
      annotationMetas([
        { ...plainFrame, customData: { etoki: {} } },
        { ...annotation, isDeleted: true },
      ]),
    ).toEqual({ f1: { granularity: "", kind: undefined } });
  });
});

describe("setAnnotationName", () => {
  // 名前は frame の `name` そのものに書く（#249）。キャンバスのラベルにも同じ名前が
  // 出るので、一覧とキャンバスの名前が揃う。
  it("注釈の frame の名前を書き換え、履歴に積めるよう version を上げる", () => {
    const [el] = setAnnotationName([{ ...annotation, version: 3 }], "f2", "決済の流れ");
    expect(el?.name).toBe("決済の流れ");
    expect(el?.version).toBe(4);
  });

  it("前後の空白は落とし、空なら名前を外す", () => {
    expect(setAnnotationName([annotation], "f2", "  決済  ")[0]?.name).toBe("決済");
    // 外すと Excalidraw の既定（null）に戻る。一覧は「絵N」と補う。
    expect(setAnnotationName([annotation], "f2", "   ")[0]?.name).toBeNull();
  });

  // 同じ名前で書くと、何も変えていないのに未保存になり、「元に戻す」にも積まれる。
  it("名前が変わらなければ要素をそのまま返す", () => {
    const unnamed: SceneElement = { ...annotation, name: null };
    expect(setAnnotationName([annotation], "f2", "決済まわり")[0]).toBe(annotation);
    expect(setAnnotationName([unnamed], "f2", "")[0]).toBe(unnamed);
  });

  // 名前を付けられるのは詳細を開ける注釈だけ。ブレスト中に人が使った frame の
  // 名前を etoki が書き換えない。
  it("注釈でない frame とほかの要素には触らない", () => {
    const [frame, other] = setAnnotationName([plainFrame, text], "f1", "名前");
    expect(frame).toBe(plainFrame);
    expect(other).toBe(text);
  });
});

describe("annotationNames", () => {
  // 注釈の詳細の名前欄が出す値。保存済みの値ではなく、キャンバスにいま在る値。
  it("注釈の frame だけを、名前で引けるようにする。名前が無ければ空文字", () => {
    expect(
      annotationNames([plainFrame, annotation, text, { ...templated, name: null }]),
    ).toEqual({ f2: "決済まわり", f3: "" });
  });

  it("削除済みは除く", () => {
    expect(annotationNames([{ ...annotation, isDeleted: true }])).toEqual({});
  });
});
