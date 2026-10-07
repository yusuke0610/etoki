import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, authApi } from "../api/boards";
import type { SessionStatus } from "../api/types";
import { useSession } from "./useSession";

/**
 * ログイン状態（#230 で `App` から切り出し）。
 *
 * **守りたいのは、読めなかったときにログインを求めない側に倒すこと。**
 * 求める側に倒すと、認証を設定していない構成が API の一時的な失敗で使えなく
 * なる。
 */

const failure = new ApiError(500, "test_unknown", "boom");
const FALLBACK: SessionStatus = { authRequired: false, authenticated: false };

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useSession", () => {
  it("読むまでは null で、起動時に読んだ状態を持つ", async () => {
    const status: SessionStatus = { authRequired: true, authenticated: true };
    vi.spyOn(authApi, "session").mockResolvedValue(status);
    const { result } = renderHook(() => useSession(vi.fn()));

    expect(result.current.session).toBeNull();
    await waitFor(() => expect(result.current.session).toEqual(status));
  });

  // key を付けるのは、開発時の StrictMode が起動時の effect を 2 回走らせるため。
  // 畳まないと同じ失敗が 2 件並ぶ。
  it("読めなかったら、ログインを求めない側に倒し、key つきで知らせる", async () => {
    vi.spyOn(authApi, "session").mockRejectedValue(failure);
    const showFailure = vi.fn();
    const { result } = renderHook(() => useSession(showFailure));

    await waitFor(() => expect(result.current.session).toEqual(FALLBACK));
    expect(showFailure).toHaveBeenCalledWith(
      { message: "ログイン状態を取得できませんでした: boom", detail: "" },
      { key: "session-failed" },
    );
  });

  it.each([
    ["認証が要らないなら signedIn", { authRequired: false, authenticated: false }, true],
    [
      "要るがログイン済みなら signedIn",
      { authRequired: true, authenticated: true },
      true,
    ],
    [
      "要るがまだログインしていないなら signedIn にしない",
      { authRequired: true, authenticated: false },
      false,
    ],
  ] as [string, SessionStatus, boolean][])("%s", async (_, status, want) => {
    vi.spyOn(authApi, "session").mockResolvedValue(status);
    const { result } = renderHook(() => useSession(vi.fn()));

    await waitFor(() => expect(result.current.session).toEqual(status));
    expect(result.current.signedIn).toBe(want);
  });

  it("問い合わせ中は signedIn にしない", () => {
    vi.spyOn(authApi, "session").mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(() => useSession(vi.fn()));

    expect(result.current.signedIn).toBe(false);
  });

  it("読み直しに失敗したときも、同じ側に倒して知らせる", async () => {
    const session = vi
      .spyOn(authApi, "session")
      .mockResolvedValueOnce({ authRequired: true, authenticated: true })
      .mockRejectedValueOnce(failure);
    const showFailure = vi.fn();
    const { result } = renderHook(() => useSession(showFailure));
    await waitFor(() => expect(result.current.signedIn).toBe(true));

    await act(() => result.current.reread());

    expect(session).toHaveBeenCalledTimes(2);
    expect(result.current.session).toEqual(FALLBACK);
    expect(showFailure).toHaveBeenCalledWith(
      { message: "ログイン状態を取得できませんでした: boom", detail: "" },
      { key: "session-failed" },
    );
  });
});
