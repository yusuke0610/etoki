import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { MENU_TRIGGER_NAME, useMenuTriggerName } from "./useMenuTriggerName";

/** Excalidraw が描くメニューの入口（≡）と同じ形。名前を持たない。 */
function trigger(): HTMLButtonElement {
  const button = document.createElement("button");
  button.type = "button";
  button.dataset.testid = "main-menu-trigger";
  return button;
}

/** MutationObserver の通知はマイクロタスクで届く。 */
function flushObservers(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function mount(frame: HTMLElement, ready: unknown = {}) {
  return renderHook(({ ready }) => useMenuTriggerName({ current: frame }, ready), {
    initialProps: { ready },
  });
}

describe("useMenuTriggerName", () => {
  afterEach(() => {
    document.body.replaceChildren();
  });

  it("描かれているメニューの入口に名前を付ける", () => {
    const frame = document.createElement("div");
    const button = trigger();
    frame.append(button);
    document.body.append(frame);

    mount(frame);

    expect(button.getAttribute("aria-label")).toBe(MENU_TRIGGER_NAME);
  });

  // 置き方が変わる（広い画面 ⇄ スマホ）と、Excalidraw は入口を作り直す。最初の
  // 1 つにだけ付ける形だと、切り替えたあとの入口は名前を失う。
  it("あとから作り直された入口にも名前を付ける", async () => {
    const frame = document.createElement("div");
    document.body.append(frame);
    mount(frame);

    const button = trigger();
    frame.append(button);
    await flushObservers();

    expect(button.getAttribute("aria-label")).toBe(MENU_TRIGGER_NAME);
  });

  // ライブラリが自分で名前を付けるようになったら、そちらに任せる。上書きすると、
  // 言語の設定に沿った名前を etoki が固定の文字列で潰す。
  it("すでに名前があれば触らない", () => {
    const frame = document.createElement("div");
    const button = trigger();
    button.setAttribute("aria-label", "Main menu");
    frame.append(button);
    document.body.append(frame);

    mount(frame);

    expect(button.getAttribute("aria-label")).toBe("Main menu");
  });

  // Excalidraw が描かれる前（API がまだ無い）は見ない。描かれてから見始める。
  it("描かれる前は見ず、描かれたら名前を付ける", () => {
    const frame = document.createElement("div");
    const button = trigger();
    frame.append(button);
    document.body.append(frame);

    const { rerender } = mount(frame, null);
    expect(button.hasAttribute("aria-label")).toBe(false);

    rerender({ ready: {} });
    expect(button.getAttribute("aria-label")).toBe(MENU_TRIGGER_NAME);
  });

  it("外したあとは見張らない", async () => {
    const frame = document.createElement("div");
    document.body.append(frame);
    const { unmount } = mount(frame);
    unmount();

    const button = trigger();
    frame.append(button);
    await flushObservers();

    expect(button.hasAttribute("aria-label")).toBe(false);
  });
});
