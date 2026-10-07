import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../api/boards";
import type { BoardListEntry } from "../api/types";
import { useBoardList } from "./useBoardList";

/**
 * ボードの一覧（#230 で `App` から切り出し）。
 *
 * **守りたいのは、古い応答で新しい一覧を上書きしないこと。** 一覧へ戻るたびに
 * 読み直す（#200）ので、改名や削除の引き直しと並走する。上書きすると件数が
 * 巻き戻る。
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

const entry = (id: string): BoardListEntry => ({
  id,
  name: id,
  role: "owner",
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-07T00:00:00Z",
  repositoryOwner: "acme",
  repositoryName: "web",
  projectId: "PVT_1",
  projectNumber: 1,
  projectTitle: "ロードマップ",
  projectUrl: "https://github.com/orgs/acme/projects/1",
  annotationCounts: null,
});

const failure = new ApiError(500, "test_unknown", "boom");

function setup() {
  const showFailure = vi.fn();
  const dismissKey = vi.fn();
  const onLoginRequired = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);
  const hook = renderHook(() =>
    useBoardList({ showFailure, dismissKey, onLoginRequired }),
  );
  return { hook, showFailure, dismissKey, onLoginRequired };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useBoardList", () => {
  it("読めたら一覧と取得時刻が入り、前の失敗の通知を下げる", async () => {
    vi.spyOn(boardsApi, "list").mockResolvedValue([entry("b1"), entry("b2")]);
    const { hook, dismissKey } = setup();

    expect(hook.result.current.boards).toBeNull();
    await act(() => hook.result.current.reload());

    expect(hook.result.current.boards?.entries).toEqual([entry("b1"), entry("b2")]);
    expect(hook.result.current.boards?.fetchedAt).toBeInstanceOf(Date);
    expect(dismissKey).toHaveBeenCalledWith("board-list-failed");
  });

  it("追い越された読み込みは、成功も失敗も反映しない", async () => {
    const first = deferred<BoardListEntry[]>();
    const second = deferred<BoardListEntry[]>();
    vi.spyOn(boardsApi, "list")
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const { hook, showFailure } = setup();

    let a!: Promise<void>;
    let b!: Promise<void>;
    act(() => {
      a = hook.result.current.reload();
      b = hook.result.current.reload();
    });
    await act(async () => {
      second.resolve([entry("新しい")]);
      first.resolve([entry("古い")]);
      await Promise.all([a, b]);
    });
    expect(hook.result.current.boards?.entries).toEqual([entry("新しい")]);

    // 先に始めた読み込みが失敗しても、後から始めたほうが答えを持っている。
    const older = deferred<BoardListEntry[]>();
    const newer = deferred<BoardListEntry[]>();
    vi.mocked(boardsApi.list).mockReset();
    vi.mocked(boardsApi.list)
      .mockReturnValueOnce(older.promise)
      .mockReturnValueOnce(newer.promise);
    act(() => {
      a = hook.result.current.reload();
      b = hook.result.current.reload();
    });
    await act(async () => {
      newer.resolve([entry("後")]);
      older.reject(failure);
      await Promise.all([a, b]);
    });
    expect(showFailure).not.toHaveBeenCalled();
    expect(hook.result.current.boards?.entries).toEqual([entry("後")]);
  });

  it("失敗したら「再読み込み」つきで知らせ、押すとその時点の読み込みを走らせる", async () => {
    const list = vi
      .spyOn(boardsApi, "list")
      .mockRejectedValueOnce(failure)
      .mockResolvedValueOnce([entry("b1")]);
    const { hook, showFailure } = setup();

    await act(() => hook.result.current.reload());

    expect(showFailure).toHaveBeenCalledTimes(1);
    const [shown, options] = showFailure.mock.calls[0] ?? [];
    expect(shown).toEqual({
      message: "ボード一覧を取得できませんでした: boom",
      detail: "",
    });
    expect(options.key).toBe("board-list-failed");
    expect(options.action.label).toBe("再読み込み");

    act(() => options.action.run());
    await waitFor(() => expect(hook.result.current.boards).not.toBeNull());
    expect(list).toHaveBeenCalledTimes(2);
    expect(hook.result.current.boards?.entries).toEqual([entry("b1")]);
  });

  it("ログインが要ると返ったら、一覧の失敗として出さずにログイン状態を読み直す", async () => {
    vi.spyOn(boardsApi, "list").mockRejectedValue(
      new ApiError(401, "login_required", "login required"),
    );
    const { hook, showFailure, onLoginRequired } = setup();

    await act(() => hook.result.current.reload());

    expect(onLoginRequired).toHaveBeenCalledTimes(1);
    expect(showFailure).not.toHaveBeenCalled();
  });

  it("消えたボードは、引き直しを待たずに手元から外す", async () => {
    vi.spyOn(boardsApi, "list").mockResolvedValue([entry("b1"), entry("b2")]);
    const { hook } = setup();
    await act(() => hook.result.current.reload());

    act(() => hook.result.current.remove("b1"));

    expect(hook.result.current.boards?.entries).toEqual([entry("b2")]);
  });

  it("捨てたら一覧を消し、走っている読み込みの応答も入れない", async () => {
    const pending = deferred<BoardListEntry[]>();
    vi.spyOn(boardsApi, "list")
      .mockResolvedValueOnce([entry("前の人の")])
      .mockReturnValueOnce(pending.promise);
    const { hook } = setup();
    await act(() => hook.result.current.reload());

    let loading!: Promise<void>;
    act(() => {
      loading = hook.result.current.reload();
    });
    act(() => hook.result.current.clear());
    expect(hook.result.current.boards).toBeNull();

    await act(async () => {
      pending.resolve([entry("遅れて届いた")]);
      await loading;
    });
    expect(hook.result.current.boards).toBeNull();
  });
});
