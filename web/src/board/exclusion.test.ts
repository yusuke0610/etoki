import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import {
  blockedReason,
  useExclusion,
  useReentryGuard,
  type Operation,
  type RunningOperation,
} from "./exclusion";

/**
 * 排他の表を固定する（#146）。
 *
 * **守りたいのは「どの組が並走しないか」と「そのとき何と言うか」の 2 つ。**
 * 以前はこれが 5 箇所（ref 2 つ・state 2 つ・導出 1 つ）に分かれていて、
 * 1 マス変えたことに気づく手段が E2E しか無かった。
 *
 * **期待値は「非空」ではなく文字列そのもの。** 全部に同じ理由を返す実装でも
 * 通ってしまう（`.claude/rules/test-effectiveness.md`）。
 */

const RUNNING: RunningOperation[] = ["saving", "importing", "creating"];
const OPERATIONS: Operation[] = [...RUNNING, "changeTarget"];

describe("blockedReason", () => {
  // 何も走っていなければ、どの操作にも理由は出ない。
  it("何も走っていなければ null", () => {
    for (const op of OPERATIONS) {
      expect(blockedReason(op, null), op).toBeNull();
    }
  });

  // **表そのもの。** 1 マスでも書き換えたら落ちる。
  it.each([
    ["saving", "importing", "取り込みが終わるまで保存できません"],
    ["saving", "creating", "作成が終わるまで保存できません"],
    ["importing", "saving", "保存が終わるまで取り込めません"],
    ["importing", "creating", "作成が終わるまで取り込めません"],
    ["creating", "saving", "保存が終わるまで作成できません。"],
    ["creating", "importing", "取り込みが終わるまで作成できません。"],
    ["changeTarget", "saving", "保存が終わるまで作成先を変更できません"],
  ] as [Operation, RunningOperation, string][])(
    "%s は %s の最中なら「%s」",
    (op, running, reason) => {
      expect(blockedReason(op, running)).toBe(reason);
    },
  );

  // **対角は理由を出さない。** ボタン自身が「保存中…」と名乗っているため。
  // 出さないことと通すことは別で、弾くのは useExclusion の仕事。
  it.each(RUNNING)("%s の最中に同じ操作の理由は出さない", (op) => {
    expect(blockedReason(op, op)).toBeNull();
  });

  // いまは止めていない組。**現状を写したものなので、止めると決めた日に
  // ここが落ちる。** 黙って変わらないための固定。
  it.each([
    ["changeTarget", "importing"],
    ["changeTarget", "creating"],
  ] as [Operation, RunningOperation][])(
    "%s は %s の最中でも止めていない",
    (op, running) => {
      expect(blockedReason(op, running)).toBeNull();
    },
  );
});

describe("useExclusion", () => {
  /** 決着を手元で握れる Promise。 */
  function pending(): { promise: Promise<void>; settle: () => void } {
    let settle!: () => void;
    const promise = new Promise<void>((resolve) => {
      settle = resolve;
    });
    return { promise, settle };
  }

  it("走っているあいだは running に出て、終わると戻る", async () => {
    const { result } = renderHook(() => useExclusion());
    const first = pending();

    expect(result.current.running).toBeNull();

    let done!: Promise<boolean>;
    await act(async () => {
      done = result.current.run("saving", () => first.promise);
    });
    expect(result.current.running).toBe("saving");

    await act(async () => {
      first.settle();
      await done;
    });
    expect(result.current.running).toBeNull();
    expect(await done).toBe(true);
  });

  // **押した時点の値で弾く。** 同じ tick に 2 回呼ぶ形にしてあるのは、間に
  // 再描画を挟むと state だけを見る実装でも通ってしまうため（#108）。
  it("同じ tick に届いた 2 回目は f を呼ばずに false", async () => {
    const { result } = renderHook(() => useExclusion());
    const first = pending();
    const second = vi.fn(() => Promise.resolve());

    let started!: Promise<boolean>;
    let refused!: Promise<boolean>;
    await act(async () => {
      started = result.current.run("saving", () => first.promise);
      refused = result.current.run("creating", second);
    });

    expect(await refused).toBe(false);
    expect(second).not.toHaveBeenCalled();

    await act(async () => {
      first.settle();
      await started;
    });
  });

  // 走っている操作が変わると、押せない理由も変わる。
  it("reasonFor が走っている操作を反映する", async () => {
    const { result } = renderHook(() => useExclusion());
    const first = pending();

    expect(result.current.reasonFor("creating")).toBeNull();

    let done!: Promise<boolean>;
    await act(async () => {
      done = result.current.run("saving", () => first.promise);
    });
    expect(result.current.reasonFor("creating")).toBe("保存が終わるまで作成できません。");
    expect(result.current.reasonFor("importing")).toBe("保存が終わるまで取り込めません");

    await act(async () => {
      first.settle();
      await done;
    });
    expect(result.current.reasonFor("creating")).toBeNull();
  });

  // **投げても排他を離す。** 離さないと、1 度失敗しただけで以後すべての
  // 保存・取り込み・作成が黙って弾かれる。
  it("f が投げても排他を離す", async () => {
    const { result } = renderHook(() => useExclusion());

    await act(async () => {
      await expect(
        result.current.run("importing", () => Promise.reject(new Error("読めない"))),
      ).rejects.toThrow("読めない");
    });

    expect(result.current.running).toBeNull();
    let taken!: boolean;
    await act(async () => {
      taken = await result.current.run("saving", () => Promise.resolve());
    });
    expect(taken).toBe(true);
  });
});

describe("useReentryGuard", () => {
  it("同じ対象の 2 回目は入れない", () => {
    const { result } = renderHook(() => useReentryGuard());

    expect(result.current.enter("a")).toBe(true);
    expect(result.current.enter("a")).toBe(false);

    result.current.leave("a");
    expect(result.current.enter("a")).toBe(true);
  });

  // **対象ごとに独立している。** 注釈 A の履歴を読んでいるあいだも、注釈 B の
  // 履歴は読める。1 つの真偽値にすると、ここが黙って直列になる。
  it("対象が違えば止めない", () => {
    const { result } = renderHook(() => useReentryGuard());

    expect(result.current.enter("a")).toBe(true);
    expect(result.current.enter("b")).toBe(true);
  });

  // 対象が 1 つしか無い操作（図を置く）は key を省く。
  it("key を省いても同じように弾く", () => {
    const { result } = renderHook(() => useReentryGuard());

    expect(result.current.enter()).toBe(true);
    expect(result.current.enter()).toBe(false);

    result.current.leave();
    expect(result.current.enter()).toBe(true);
  });
});
