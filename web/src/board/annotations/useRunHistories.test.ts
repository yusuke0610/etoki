import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import type { SyncRun } from "../../api/types";
import { useRunHistories } from "./useRunHistories";

/**
 * 実行の履歴を押したときだけ引く（#146 で `BoardPage` から切り出し）。
 *
 * **守りたいのは、古い応答で履歴を書かないこと。** 作成すると履歴は 1 件
 * 増えるので、そのとき走っていた読み込みの応答は 1 件足りない。入れると、
 * 作ったばかりの run が抜けた履歴が残る。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const run = (id: number): SyncRun => ({
  id,
  createdAt: "2026-10-07T00:00:00Z",
  outcome: "complete",
  items: [],
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useRunHistories", () => {
  it("押すと読み込み中になり、引いた履歴がそのまま入る", async () => {
    const pending = deferred<SyncRun[]>();
    const runs = vi.spyOn(boardsApi, "runs").mockReturnValue(pending.promise);
    const { result } = renderHook(() => useRunHistories("b1"));

    let loading!: Promise<void>;
    act(() => {
      loading = result.current.load("a1");
    });
    expect(runs).toHaveBeenCalledWith("b1", "a1");
    expect(result.current.states.a1).toEqual({ status: "loading" });

    await act(async () => {
      pending.resolve([run(2), run(1)]);
      await loading;
    });
    expect(result.current.states.a1).toEqual({ status: "done", runs: [run(2), run(1)] });
  });

  it("失敗したら、その注釈の履歴を失敗にする", async () => {
    vi.spyOn(boardsApi, "runs").mockRejectedValue(
      new ApiError(500, "test_unknown", "boom"),
    );
    const { result } = renderHook(() => useRunHistories("b1"));

    await act(() => result.current.load("a1"));
    expect(result.current.states.a1).toEqual({
      status: "error",
      failure: { message: "履歴を読み込めませんでした: boom", detail: "" },
    });
  });

  // 二重押しは注釈ごとに弾く。**押した時点の値で弾く**ので、同じ tick の 2 回目も
  // 通らない（`exclusion.ts` の `useReentryGuard`）。
  it("同じ注釈の二重押しは 1 回しか引かない。別の注釈は並んで引ける", async () => {
    const pending = deferred<SyncRun[]>();
    const runs = vi.spyOn(boardsApi, "runs").mockReturnValue(pending.promise);
    const { result } = renderHook(() => useRunHistories("b1"));

    act(() => {
      void result.current.load("a1");
      void result.current.load("a1");
      void result.current.load("a2");
    });
    expect(runs.mock.calls).toEqual([
      ["b1", "a1"],
      ["b1", "a2"],
    ]);

    await act(async () => {
      pending.resolve([]);
      await pending.promise;
    });
    // 終わったら、また押せる。
    act(() => {
      void result.current.load("a1");
    });
    expect(runs).toHaveBeenCalledTimes(3);
  });

  it("読み込み中に捨てたら、遅れて届いた応答を入れない", async () => {
    const pending = deferred<SyncRun[]>();
    vi.spyOn(boardsApi, "runs").mockReturnValue(pending.promise);
    const { result } = renderHook(() => useRunHistories("b1"));

    let loading!: Promise<void>;
    act(() => {
      loading = result.current.load("a1");
    });
    act(() => result.current.discard("a1"));
    expect(result.current.states).not.toHaveProperty("a1");

    await act(async () => {
      pending.resolve([run(1)]);
      await loading;
    });
    expect(result.current.states).not.toHaveProperty("a1");
  });

  it("捨てても、他の注釈の履歴は残る", async () => {
    vi.spyOn(boardsApi, "runs").mockResolvedValue([run(1)]);
    const { result } = renderHook(() => useRunHistories("b1"));

    await act(() => result.current.load("a1"));
    await act(() => result.current.load("a2"));
    act(() => result.current.discard("a1"));

    expect(result.current.states).toEqual({ a2: { status: "done", runs: [run(1)] } });
  });
});
