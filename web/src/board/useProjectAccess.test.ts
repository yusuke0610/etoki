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

  // 切れると: 前に同じ作成先で確かめた答え（denied）が、ID の照合をまた通る。
  // 戻すまでのあいだに相手の権限が変わっていても、訊き直しが届く前に古い答えで
  // 止める（#481 の full review）。
  it("前と同じ作成先に戻しても、訊き直しが届くまでは前の答えを出さない", async () => {
    const third = deferred<BoardAccess>();
    vi.spyOn(boardsApi, "access")
      .mockResolvedValueOnce(access("denied"))
      .mockReturnValueOnce(new Promise<BoardAccess>(() => {}))
      .mockReturnValueOnce(third.promise);

    const { result, rerender } = renderHook(
      ({ projectId }: { projectId: string }) => useProjectAccess("b1", projectId),
      { initialProps: { projectId: "PVT_1" } },
    );
    await waitFor(() => expect(result.current).toBe("denied"));

    rerender({ projectId: "PVT_2" });
    rerender({ projectId: "PVT_1" });

    expect(result.current).toBe("unknown");

    await act(async () => {
      third.resolve(access("allowed"));
      await third.promise;
    });
    expect(result.current).toBe("allowed");
  });

  // 切れると: 戻したあとの訊き直しが失敗したとき、前の denied が読み込み直すまで
  // 残り、作れるかもしれない Project に作らせない。あいだの作成先の確認も失敗して
  // いると、答えを上書きするものが無いので、前の答えがそのまま残る。
  it("前と同じ作成先に戻して訊き直しが失敗したら、前の答えではなく unknown", async () => {
    const get = vi
      .spyOn(boardsApi, "access")
      .mockResolvedValueOnce(access("denied"))
      .mockRejectedValueOnce(new Error("boom"))
      .mockRejectedValueOnce(new Error("boom"));

    const { result, rerender } = renderHook(
      ({ projectId }: { projectId: string }) => useProjectAccess("b1", projectId),
      { initialProps: { projectId: "PVT_1" } },
    );
    await waitFor(() => expect(result.current).toBe("denied"));
    rerender({ projectId: "PVT_2" });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
    await act(async () => {});

    rerender({ projectId: "PVT_1" });
    await waitFor(() => expect(get).toHaveBeenCalledTimes(3));
    await act(async () => {});

    expect(result.current).toBe("unknown");
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
