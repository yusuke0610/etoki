import { describe, expect, it } from "vitest";

import { ETOKI_NAMESPACE, type SceneElement } from "./annotation";
import { sceneSignature } from "./dirty";

function el(over: Partial<SceneElement> = {}): SceneElement {
  return { id: "a", type: "rectangle", version: 1, ...over };
}

describe("sceneSignature", () => {
  it("中身が同じなら同じ署名になる", () => {
    const before = [el({ id: "a" }), el({ id: "b", version: 3 })];
    const after = [el({ id: "a" }), el({ id: "b", version: 3 })];

    expect(sceneSignature(after)).toBe(sceneSignature(before));
  });

  // Excalidraw は選択やスクロールでも onChange を発火するが、そのとき要素の
  // version は上がらない。ここが「開いただけで未保存になる」を防ぐ要。
  it("version が変わらない限り署名は変わらない", () => {
    const selected = [el({ id: "a" })];
    // 選択状態は appState にあり要素には現れないので、同じ配列で表せる。
    expect(sceneSignature(selected)).toBe(sceneSignature([el({ id: "a" })]));
  });

  it("version が上がると署名が変わる", () => {
    expect(sceneSignature([el({ version: 2 })])).not.toBe(
      sceneSignature([el({ version: 1 })]),
    );
  });

  it("要素が増えると署名が変わる", () => {
    expect(sceneSignature([el({ id: "a" }), el({ id: "b" })])).not.toBe(
      sceneSignature([el({ id: "a" })]),
    );
  });

  // 注釈の付け外しは etoki 自身が customData を書き換える。Excalidraw が
  // version を上げるとは限らないので、customData も署名に含める。
  it("注釈を付けると署名が変わる", () => {
    const plain = [el({ type: "frame" })];
    const annotated = [
      el({ type: "frame", customData: { [ETOKI_NAMESPACE]: { granularity: "" } } }),
    ];

    expect(sceneSignature(annotated)).not.toBe(sceneSignature(plain));
  });

  it("粒度を変えると署名が変わる", () => {
    const auto = [
      el({ type: "frame", customData: { [ETOKI_NAMESPACE]: { granularity: "" } } }),
    ];
    const epic = [
      el({ type: "frame", customData: { [ETOKI_NAMESPACE]: { granularity: "epic" } } }),
    ];

    expect(sceneSignature(epic)).not.toBe(sceneSignature(auto));
  });

  // onChange は削除済みを含む配列を渡すが、getSceneElements() は含まない。
  // 同じシーンを別の入口から見て署名が食い違うと、編集していないのに未保存になる。
  it("削除済みの要素は無視するので、入口が違っても同じ署名になる", () => {
    const fromOnChange = [
      el({ id: "a" }),
      el({ id: "gone", isDeleted: true, version: 9 }),
    ];
    const fromGetSceneElements = [el({ id: "a" })];

    expect(sceneSignature(fromOnChange)).toBe(sceneSignature(fromGetSceneElements));
  });

  it("要素を消すと署名が変わる", () => {
    const before = sceneSignature([el({ id: "a" }), el({ id: "b" })]);
    const after = sceneSignature([
      el({ id: "a" }),
      el({ id: "b", isDeleted: true, version: 2 }),
    ]);

    expect(after).not.toBe(before);
  });
});
