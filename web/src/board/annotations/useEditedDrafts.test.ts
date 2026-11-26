import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useEditedDrafts } from "./useEditedDrafts";

describe("useEditedDrafts", () => {
  it("どれか 1 つでも手直しがあれば上げ、全部下りたら下ろす", () => {
    const onChange = vi.fn();
    const { result } = renderHook(() => useEditedDrafts(onChange));

    act(() => result.current.report("a", true));
    act(() => result.current.report("b", true));
    expect(result.current.any).toBe(true);
    expect(onChange).toHaveBeenLastCalledWith(true);

    // 片方が下りても、もう片方が残っている。
    act(() => result.current.report("a", false));
    expect(result.current.any).toBe(true);

    act(() => result.current.report("b", false));
    expect(result.current.any).toBe(false);
    expect(onChange).toHaveBeenLastCalledWith(false);
  });

  // 保存の前の確認は押した時点で読む。state だと描き直すまで古い。
  it("知らされたその場で、描き直しを待たずに手直しありと答える", () => {
    const { result } = renderHook(() => useEditedDrafts(vi.fn()));

    let seen = false;
    act(() => {
      result.current.report("a", true);
      seen = result.current.hasEdited();
    });

    expect(seen).toBe(true);
  });

  // ボードを離れたら、親（App の導線）に残さない。残すと、次に開いたボードで
  // 失うものが無いのに「手直しを失う」と訊く。
  it("外れるときは、手直しが残っていても下ろす", () => {
    const onChange = vi.fn();
    const { result, unmount } = renderHook(() => useEditedDrafts(onChange));
    act(() => result.current.report("a", true));

    unmount();

    expect(onChange).toHaveBeenLastCalledWith(false);
  });

  // 下書きは開いた注釈の数だけあり、どれも最初に「手直しなし」を知らせる。その
  // たびにボードの画面（キャンバスごと）を描き直さない。
  it("変わらない知らせでは描き直さない", () => {
    let renders = 0;
    const { result } = renderHook(() => {
      renders += 1;
      return useEditedDrafts(vi.fn());
    });
    const before = renders;

    act(() => result.current.report("a", false));

    expect(renders).toBe(before);
  });

  it("受け取る関数と読む関数は、描き直しても同じものを渡す", () => {
    const { result } = renderHook(() => useEditedDrafts(vi.fn()));
    const { report, hasEdited } = result.current;

    act(() => result.current.report("a", true));

    expect(result.current.report).toBe(report);
    expect(result.current.hasEdited).toBe(hasEdited);
  });

  // 切れると: 束を受け取る `BoardPage` の依存が描き直しのたびに変わり、束から
  // 作るもの（確認の登録など）を作り直す（web/CLAUDE.md「束は、入力が変わらない
  // 描き直しで同じものを返す」、#485 の full review）。**`onChange` は外で作って
  // 同じものを渡す。** 描くたびに作ると、入力が変わったことになる。
  it("入力が変わらない描き直しでは、同じ束を返す", () => {
    const onChange = vi.fn();
    const { result, rerender } = renderHook(() => useEditedDrafts(onChange));
    const before = result.current;

    rerender();

    expect(result.current).toBe(before);
  });
});
