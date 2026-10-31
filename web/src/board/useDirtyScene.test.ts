import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useConfirmLeave, useDirtyScene } from "./useDirtyScene";

/**
 * 未保存かどうか（#146 で `BoardPage` から切り出し、ADR 0021）。
 *
 * **守りたいのは、描いたものを未保存のまま残すこと。** 保存の待ち時間の
 * 描き足しを保存済みに数えると、確認なしで離れられて消える。待ちを挟んだ判定が
 * 古い値を読んでも同じことが起きる。
 */

function setup() {
  const onDirtyChange = vi.fn<(dirty: boolean) => void>();
  const hook = renderHook(() => useDirtyScene(onDirtyChange));
  return { hook, onDirtyChange };
}

describe("useDirtyScene", () => {
  it("最初に届いた署名は保存済みシーンとして覚え、未保存にしない", () => {
    const { hook } = setup();

    // Excalidraw はマウント時にも onChange を出す。その 1 回目は保存済みシーン。
    act(() => hook.result.current.applySignature("saved"));

    expect(hook.result.current.dirty).toBe(false);
  });

  it("違う署名が来たら未保存にし、保存済みと同じに戻れば下ろす", () => {
    const { hook } = setup();
    act(() => hook.result.current.applySignature("saved"));

    act(() => hook.result.current.applySignature("edited"));
    expect(hook.result.current.dirty).toBe(true);

    act(() => hook.result.current.applySignature("saved"));
    expect(hook.result.current.dirty).toBe(false);
  });

  it("送った署名で保存が済んだら、それを新しい基準にして下ろす", () => {
    const { hook } = setup();
    act(() => hook.result.current.applySignature("saved"));
    act(() => hook.result.current.applySignature("edited"));

    act(() => hook.result.current.markSaved("edited"));

    expect(hook.result.current.dirty).toBe(false);
    // 基準が進んだので、前の保存済みシーンに戻すのは未保存。
    act(() => hook.result.current.applySignature("saved"));
    expect(hook.result.current.dirty).toBe(true);
  });

  it("保存の待ち時間に描き足されていたら、保存が済んでも未保存のまま残す", () => {
    const { hook } = setup();
    act(() => hook.result.current.applySignature("saved"));
    act(() => hook.result.current.applySignature("sent"));

    // 保存が "sent" を送ったあと、応答を待つあいだに描き足した。
    act(() => hook.result.current.applySignature("drawn-while-saving"));
    act(() => hook.result.current.markSaved("sent"));

    expect(hook.result.current.dirty).toBe(true);
    expect(hook.result.current.isDirty()).toBe(true);
  });

  it("isDirty は描き直しを待たずに、いまの値を返す", () => {
    const { hook } = setup();
    act(() => hook.result.current.applySignature("saved"));

    let seen!: boolean;
    act(() => {
      hook.result.current.applySignature("edited");
      // 同じ tick の中。state はまだ描き直されていない。待ちを挟んだ判定
      // （取り込み、ADR 0021）はこの時点の値を読む。
      seen = hook.result.current.isDirty();
    });

    expect(seen).toBe(true);
  });

  it("未保存かどうかを親に渡し、外れるときは下ろす", () => {
    const { hook, onDirtyChange } = setup();
    act(() => hook.result.current.applySignature("saved"));
    act(() => hook.result.current.applySignature("edited"));
    expect(onDirtyChange).toHaveBeenLastCalledWith(true);

    hook.unmount();

    // 残すと、キャンバスがもう無いのに親が止め続ける。
    expect(onDirtyChange).toHaveBeenLastCalledWith(false);
  });

  it("返す関数は、描き直しても同じものを返す", () => {
    const { hook } = setup();
    const { isDirty, applySignature, markSaved } = hook.result.current;

    act(() => hook.result.current.applySignature("saved"));
    act(() => hook.result.current.applySignature("edited"));

    expect(hook.result.current.isDirty).toBe(isDirty);
    expect(hook.result.current.applySignature).toBe(applySignature);
    expect(hook.result.current.markSaved).toBe(markSaved);
  });

  it("入力が変わらずに描き直しても、返す束は同じものを返す", () => {
    // 束は BoardPage が依存に置く。作り直されると、それを依存にしたものまで
    // 描くたびに作り直される（web/CLAUDE.md）。
    const { hook } = setup();
    const first = hook.result.current;

    hook.rerender();

    expect(hook.result.current).toBe(first);
  });
});

describe("useConfirmLeave", () => {
  /** ページを離れようとしたことにする。止めたかどうかを返す。 */
  function leave(): boolean {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  }

  it("止めるあいだだけ、離れる前にブラウザに訊かせる", () => {
    const hook = renderHook(({ active }) => useConfirmLeave(active), {
      initialProps: { active: false },
    });
    expect(leave()).toBe(false);

    hook.rerender({ active: true });
    expect(leave()).toBe(true);

    // 止める理由が無くなったら外す。残すと、保存したあとも訊かれる。
    hook.rerender({ active: false });
    expect(leave()).toBe(false);

    hook.rerender({ active: true });
    hook.unmount();
    expect(leave()).toBe(false);
  });

  // preventDefault だけを見ないブラウザが残っているので、実装は両方を立てる。
  // `returnValue = ""` も `defaultPrevented` を真にするので、上の断言だけだと
  // どちらか片方を消しても通る。1 つずつ控えて見る。
  it("訊かせるときは、preventDefault と returnValue の両方を立てる", () => {
    renderHook(() => useConfirmLeave(true));
    const event = new Event("beforeunload", { cancelable: true });
    const preventDefault = vi.spyOn(event, "preventDefault");
    let returnValue: unknown;
    Object.defineProperty(event, "returnValue", {
      configurable: true,
      get: () => returnValue,
      set: (value: unknown) => {
        returnValue = value;
      },
    });

    window.dispatchEvent(event);

    expect(preventDefault).toHaveBeenCalledTimes(1);
    expect(returnValue).toBe("");
  });
});
