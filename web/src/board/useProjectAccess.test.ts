import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { boardsApi } from "../api/boards";
import type { BoardAccess } from "../api/types";
import { useProjectAccess } from "./useProjectAccess";

/**
 * 作成先の Project に書けるかどうか（`useProjectAccess`）。
 *
 * **守りたいのは、作成先が変わったら訊き直すこと**（ADR 0100）。作成先はボードを
 * 開いたまま選ぶので、開いたときの答えが残ると、選んだ Project に書けないのに
 * 「GitHub に作成する」を押させる（逆に、書けるのに止める）。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const access = (projectAccess: BoardAccess["projectAccess"]): BoardAccess => ({
  role: "owner",
  projectAccess,
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useProjectAccess", () => {
  it("作成先が変わったら訊き直し、届くまでは前の作成先の答えを出さない", async () => {
    const second = deferred<BoardAccess>();
    const get = vi
      .spyOn(boardsApi, "access")
      .mockResolvedValueOnce(access("allowed"))
      .mockReturnValueOnce(second.promise);

    const { result, rerender } = renderHook(
      ({ projectId }: { projectId: string }) => useProjectAccess("b1", projectId),
      { initialProps: { projectId: "PVT_1" } },
    );
    await waitFor(() => expect(result.current).toBe("allowed"));

    rerender({ projectId: "PVT_2" });

    // 前の作成先について確かめた答えを、いまの作成先の答えとして出さない。
    expect(result.current).toBe("unknown");
    expect(get).toHaveBeenCalledTimes(2);
    expect(get).toHaveBeenLastCalledWith("b1");

    await act(async () => {
      second.resolve(access("denied"));
      await second.promise;
    });
    expect(result.current).toBe("denied");
  });

  it("作成先が変わらなければ、描き直しても訊き直さない", async () => {
    const get = vi.spyOn(boardsApi, "access").mockResolvedValue(access("allowed"));

    const { result, rerender } = renderHook(
      ({ projectId }: { projectId: string }) => useProjectAccess("b1", projectId),
      { initialProps: { projectId: "PVT_1" } },
    );
    await waitFor(() => expect(result.current).toBe("allowed"));

    rerender({ projectId: "PVT_1" });

    expect(result.current).toBe("allowed");
    expect(get).toHaveBeenCalledTimes(1);
  });

  // 確かめられなかったことを、書ける / 書けないのどちらにも倒さない（ADR 0017）。
  it("訊けなければ unknown のまま", async () => {
    const get = vi.spyOn(boardsApi, "access").mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useProjectAccess("b1", "PVT_1"));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(1));
    await act(async () => {});

    expect(result.current).toBe("unknown");
  });
});
