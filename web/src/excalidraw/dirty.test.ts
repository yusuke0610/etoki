import { describe, expect, it } from "vitest";

import { ETOKI_NAMESPACE, type SceneElement } from "./annotation";
import { sceneSignature } from "./dirty";

/** 背景色を変えていない場合の値。要素だけを見たいテストはこれで揃える。 */
const WHITE = "#ffffff";

function el(over: Partial<SceneElement> = {}): SceneElement {
  return { id: "a", type: "rectangle", version: 1, ...over };
}

describe("sceneSignature", () => {
  it("中身が同じなら同じ署名になる", () => {
    const before = [el({ id: "a" }), el({ id: "b", version: 3 })];
    const after = [el({ id: "a" }), el({ id: "b", version: 3 })];

    expect(sceneSignature(after, WHITE)).toBe(sceneSignature(before, WHITE));
  });

  // Excalidraw は選択やスクロールでも onChange を発火するが、そのとき要素の
  // version は上がらない。ここが「開いただけで未保存になる」を防ぐ要であり、
  // パネルからキャンバスへ寄せる focusFrame が未保存にしない根拠でもある
  // （ADR 0022）。
  it("version が変わらない限り署名は変わらない", () => {
    const selected = [el({ id: "a" })];
    // 選択状態は appState にあり要素には現れないので、同じ配列で表せる。
    expect(sceneSignature(selected, WHITE)).toBe(
      sceneSignature([el({ id: "a" })], WHITE),
    );
  });

  it("version が上がると署名が変わる", () => {
    expect(sceneSignature([el({ version: 2 })], WHITE)).not.toBe(
      sceneSignature([el({ version: 1 })], WHITE),
    );
  });

  it("要素が増えると署名が変わる", () => {
    expect(sceneSignature([el({ id: "a" }), el({ id: "b" })], WHITE)).not.toBe(
      sceneSignature([el({ id: "a" })], WHITE),
    );
  });

  // 注釈の付け外しは etoki 自身が customData を書き換える。Excalidraw が
  // version を上げるとは限らないので、customData も署名に含める。
  it("注釈を付けると署名が変わる", () => {
    const plain = [el({ type: "frame" })];
    const annotated = [
      el({ type: "frame", customData: { [ETOKI_NAMESPACE]: { granularity: "" } } }),
    ];

    expect(sceneSignature(annotated, WHITE)).not.toBe(sceneSignature(plain, WHITE));
  });

  it("粒度を変えると署名が変わる", () => {
    const auto = [
      el({ type: "frame", customData: { [ETOKI_NAMESPACE]: { granularity: "" } } }),
    ];
    const epic = [
      el({ type: "frame", customData: { [ETOKI_NAMESPACE]: { granularity: "epic" } } }),
    ];

    expect(sceneSignature(epic, WHITE)).not.toBe(sceneSignature(auto, WHITE));
  });

  // onChange は削除済みを含む配列を渡すが、getSceneElements() は含まない。
  // 同じシーンを別の入口から見て署名が食い違うと、編集していないのに未保存になる。
  it("削除済みの要素は無視するので、入口が違っても同じ署名になる", () => {
    const fromOnChange = [
      el({ id: "a" }),
      el({ id: "gone", isDeleted: true, version: 9 }),
    ];
    const fromGetSceneElements = [el({ id: "a" })];

    expect(sceneSignature(fromOnChange, WHITE)).toBe(
      sceneSignature(fromGetSceneElements, WHITE),
    );
  });

  // 背景色は `appState` にあって要素には現れないが、保存はシーン全体を書くので
  // 保存すべき変更（ADR 0044）。要素だけを見ると、色だけを変えたキャンバスが
  // 「未保存ではない」と出て確認なしで離れられる。
  it("要素が同じでも背景色が違えば署名が変わる", () => {
    const elements = [el({ id: "a" })];

    expect(sceneSignature(elements, "#ffeb3b")).not.toBe(sceneSignature(elements, WHITE));
  });

  it("背景色が同じなら署名も同じ", () => {
    const elements = [el({ id: "a" })];

    expect(sceneSignature(elements, WHITE)).toBe(sceneSignature(elements, WHITE));
  });

  it("要素を消すと署名が変わる", () => {
    const before = sceneSignature([el({ id: "a" }), el({ id: "b" })], WHITE);
    const after = sceneSignature(
      [el({ id: "a" }), el({ id: "b", isDeleted: true, version: 2 })],
      WHITE,
    );

    expect(after).not.toBe(before);
  });
});
