import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import type { CreatedRun, Interpretation, SyncItem } from "../../api/types";
import { useExclusion } from "../exclusion";
import { useCreation } from "./useCreation";

/**
 * 解釈結果から draft issue を作る（#146 で `BoardPage` から切り出し）。
 *
 * **作成は取り消せない**（ADR 0009）。守りたいのは次の 3 つ。
 *
 * - 保存・取り込み・別の作成と並走させない（`exclusion.ts`）。並走すると、
 *   作った内容と記録される `content_hash` が食い違ったり、作ったのに結果が
 *   消えて押し直され、draft issue が重複したりする。
 * - 注釈の状態を取り直すまで「実行中」を保つ。先に終えると、作ったばかりの
 *   item が畳み込みに入る前の隙間で押し直せる（`.claude/rules/async-ui.md`）。
 * - 保存を挟んだら、遅れて届いた結果を出さない。保存前の解釈に対するものを
 *   いまの内容に対して作られたと誤読させない。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const interpretation: Interpretation = { summary: "s", contentHash: "h", items: [] };

const item: SyncItem = {
  itemId: "I_1",
  kind: "issue",
  title: "t",
  body: "",
  localId: "l1",
  action: "created",
  confirmed: true,
};

const created: CreatedRun = {
  runId: 7,
  createdAt: "2026-10-07T00:00:00Z",
  items: [item],
};

function setup() {
  const recordCreated = vi.fn();
  const discardRuns = vi.fn();
  const refreshAnnotations = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);
  const hook = renderHook(() => {
    const exclusive = useExclusion();
    const creation = useCreation({
      boardId: "b1",
      exclusive,
      recordCreated,
      discardRuns,
      refreshAnnotations,
    });
    return { exclusive, creation };
  });
  return { hook, recordCreated, discardRuns, refreshAnnotations };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useCreation", () => {
  it("作れたら、解釈に結びつけ、履歴を捨て、注釈の状態を取り直してから終える", async () => {
    const createItems = vi.spyOn(boardsApi, "createItems").mockResolvedValue(created);
    const { hook, recordCreated, discardRuns, refreshAnnotations } = setup();

    await act(() => hook.result.current.creation.create("a1", 3, interpretation));

    expect(createItems).toHaveBeenCalledWith("b1", "a1", interpretation);
    expect(recordCreated).toHaveBeenCalledWith("a1", 3, [item]);
    expect(discardRuns).toHaveBeenCalledWith("a1");
    expect(refreshAnnotations).toHaveBeenCalledTimes(1);
    expect(hook.result.current.creation.states.a1).toEqual({
      status: "done",
      run: created,
    });
    expect(hook.result.current.exclusive.running).toBeNull();
  });

  it("注釈の状態を取り直すあいだは実行中のままにする", async () => {
    vi.spyOn(boardsApi, "createItems").mockResolvedValue(created);
    const refreshing = deferred<void>();
    const { hook, refreshAnnotations } = setup();
    refreshAnnotations.mockReturnValue(refreshing.promise);

    let creating!: Promise<void>;
    act(() => {
      creating = hook.result.current.creation.create("a1", 3, interpretation);
    });
    // 作成の応答は届き、取り直しを待っている。
    await act(async () => {
      await vi.waitFor(() => expect(refreshAnnotations).toHaveBeenCalled());
    });
    expect(hook.result.current.creation.states.a1).toEqual({ status: "running" });
    expect(hook.result.current.exclusive.running).toBe("creating");

    await act(async () => {
      refreshing.resolve();
      await creating;
    });
    expect(hook.result.current.creation.states.a1).toEqual({
      status: "done",
      run: created,
    });
  });

  it("失敗したら失敗を出し、解釈への結びつけも履歴の破棄も取り直しもしない", async () => {
    vi.spyOn(boardsApi, "createItems").mockRejectedValue(
      new ApiError(502, "test_unknown", "boom"),
    );
    const { hook, recordCreated, discardRuns, refreshAnnotations } = setup();

    await act(() => hook.result.current.creation.create("a1", 3, interpretation));

    expect(hook.result.current.creation.states.a1).toEqual({
      status: "error",
      failure: { message: "作成できませんでした: boom", detail: "" },
    });
    expect(recordCreated).not.toHaveBeenCalled();
    expect(discardRuns).not.toHaveBeenCalled();
    expect(refreshAnnotations).not.toHaveBeenCalled();
  });

  it("保存を挟んだら、遅れて届いた結果を出さず、解釈にも結びつけない", async () => {
    const pending = deferred<CreatedRun>();
    vi.spyOn(boardsApi, "createItems").mockReturnValue(pending.promise);
    const { hook, recordCreated, discardRuns } = setup();

    let creating!: Promise<void>;
    act(() => {
      creating = hook.result.current.creation.create("a1", 3, interpretation);
    });
    act(() => hook.result.current.creation.discardAll());
    expect(hook.result.current.creation.states).toEqual({});

    await act(async () => {
      pending.resolve(created);
      await creating;
    });
    expect(hook.result.current.creation.states).toEqual({});
    expect(recordCreated).not.toHaveBeenCalled();
    expect(discardRuns).not.toHaveBeenCalled();
  });

  it.each(["saving", "importing"] as const)("%s の最中は API を呼ばない", async (op) => {
    const createItems = vi.spyOn(boardsApi, "createItems").mockResolvedValue(created);
    const { hook } = setup();
    const other = deferred<void>();

    let running!: Promise<boolean>;
    act(() => {
      running = hook.result.current.exclusive.run(op, () => other.promise);
    });
    await act(() => hook.result.current.creation.create("a1", 3, interpretation));

    expect(createItems).not.toHaveBeenCalled();
    expect(hook.result.current.creation.states).toEqual({});

    await act(async () => {
      other.resolve();
      await running;
    });
  });

  it("作成中にもう 1 件押しても、API は 1 回しか呼ばない", async () => {
    const pending = deferred<CreatedRun>();
    const createItems = vi
      .spyOn(boardsApi, "createItems")
      .mockReturnValue(pending.promise);
    const { hook } = setup();

    let first!: Promise<void>;
    act(() => {
      first = hook.result.current.creation.create("a1", 3, interpretation);
      void hook.result.current.creation.create("a2", 1, interpretation);
    });
    expect(createItems).toHaveBeenCalledTimes(1);
    expect(hook.result.current.creation.states).not.toHaveProperty("a2");

    await act(async () => {
      pending.resolve(created);
      await first;
    });
  });
});
