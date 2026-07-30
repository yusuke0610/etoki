import { restore, serializeAsJSON } from "@excalidraw/excalidraw";
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types";
import { describe, expect, it } from "vitest";

/**
 * etoki が @excalidraw/excalidraw に対して置いている前提を固定するテスト。
 *
 * 注釈の粒度指定を要素の customData に持たせる設計は、customData が
 * シリアライズと restore を越えて残ることに依存している。ライブラリの
 * 更新でここが壊れると注釈のメタデータが黙って消えるため、前提そのものを
 * テストにしている。
 */

function rectangle(overrides: Partial<ExcalidrawElement> = {}): ExcalidrawElement {
  return {
    id: "rect-1",
    type: "rectangle",
    x: 0,
    y: 0,
    width: 100,
    height: 100,
    angle: 0,
    strokeColor: "#1e1e1e",
    backgroundColor: "transparent",
    fillStyle: "solid",
    strokeWidth: 1,
    strokeStyle: "solid",
    roughness: 1,
    opacity: 100,
    groupIds: [],
    frameId: null,
    roundness: null,
    seed: 1,
    version: 1,
    versionNonce: 1,
    isDeleted: false,
    boundElements: null,
    updated: 1,
    link: null,
    locked: false,
    index: null,
    ...overrides,
  } as ExcalidrawElement;
}

/** シーンを JSON にしてから復元する。実際の保存・読み込み経路を模す。 */
function roundTrip(elements: ExcalidrawElement[]): ExcalidrawElement[] {
  const json = serializeAsJSON(elements, {}, {}, "local");
  const parsed = JSON.parse(json) as {
    elements: ExcalidrawElement[];
    appState: Record<string, unknown>;
  };
  return restore({ elements: parsed.elements, appState: parsed.appState }, null, null)
    .elements as unknown as ExcalidrawElement[];
}

describe("@excalidraw/excalidraw の前提", () => {
  it("customData が serialize と restore を越えて残る", () => {
    const [got] = roundTrip([
      rectangle({ customData: { etoki: { granularity: "epic" } } }),
    ]);

    expect(got?.customData).toEqual({ etoki: { granularity: "epic" } });
  });

  it("customData を持たない要素は undefined のまま", () => {
    const [got] = roundTrip([rectangle()]);

    expect(got?.customData).toBeUndefined();
  });

  it("要素 ID は round trip で保たれる", () => {
    const [got] = roundTrip([rectangle({ id: "annotation-1" })]);

    expect(got?.id).toBe("annotation-1");
  });
});
