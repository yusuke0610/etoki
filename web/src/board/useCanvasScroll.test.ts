import { afterEach, describe, expect, it } from "vitest";

import { canArrowScroll, focusIsOnCanvas } from "./useCanvasScroll";

describe("focusIsOnCanvas", () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it("焦点がどこにも無い（body）なら、キャンバスとみなす", () => {
    expect(focusIsOnCanvas(document.body)).toBe(true);
    expect(focusIsOnCanvas(null)).toBe(true);
  });

  it("キャンバスの枠そのものなら、キャンバスとみなす", () => {
    const container = document.createElement("div");
    container.className = "excalidraw excalidraw-container";
    expect(focusIsOnCanvas(container)).toBe(true);
  });

  // ツールバーのラジオやメニューは、矢印キーを自分で使う。
  it("キャンバスの枠の中の部品は含めない", () => {
    const container = document.createElement("div");
    container.className = "excalidraw excalidraw-container";
    const radio = document.createElement("input");
    radio.type = "radio";
    container.append(radio);
    document.body.append(container);
    expect(focusIsOnCanvas(radio)).toBe(false);
  });

  it("etoki の部品（タブ・入力欄）は含めない", () => {
    const tab = document.createElement("button");
    tab.setAttribute("role", "tab");
    expect(focusIsOnCanvas(tab)).toBe(false);
    expect(focusIsOnCanvas(document.createElement("input"))).toBe(false);
  });
});

describe("canArrowScroll", () => {
  const idle = {
    selectedElementIds: {},
    editingTextElement: null,
    openDialog: null,
    contextMenu: null,
  } as Parameters<typeof canArrowScroll>[0];

  it("何も選んでおらず、何も開いていなければ使う", () => {
    expect(canArrowScroll(idle)).toBe(true);
  });

  it.each([
    ["要素を選んでいる", { selectedElementIds: { a: true } }],
    ["文字を編集している", { editingTextElement: {} }],
    ["ダイアログが開いている", { openDialog: { name: "help" } }],
    ["右クリックのメニューが開いている", { contextMenu: { items: [], top: 0, left: 0 } }],
  ])("%s あいだは使わない", (_label, patch) => {
    expect(
      canArrowScroll({ ...idle, ...patch } as Parameters<typeof canArrowScroll>[0]),
    ).toBe(false);
  });
});
