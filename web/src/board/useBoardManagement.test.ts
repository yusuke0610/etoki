import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi, githubApi } from "../api/boards";
import { targetProjectMissingFailure } from "../api/errorMessage";
import type { BoardDeletion, BoardDetail, Project } from "../api/types";
import { useBoardDeletion, useRename, useTargetRefresh } from "./useBoardManagement";

/**
 * ボードそのものの管理（改名・削除・作成先の名前の取り直し）。#146 で
 * `BoardPage` から切り出し。
 *
 * **削除は取り消せない**（ADR 0042）。失われるものを見せる前に確認を出さない
 * こと、確認を経ずに消さないことを固定する。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const board: BoardDetail = {
  id: "b1",
  name: "ロードマップ",
  role: "owner",
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-07T00:00:00Z",
  repositoryOwner: "acme",
  repositoryName: "web",
  projectId: "PVT_1",
  projectNumber: 1,
  projectTitle: "古い名前",
  projectUrl: "https://github.com/orgs/acme/projects/1",
  scene: "{}",
  targetLocked: true,
  sceneOverLimit: false,
};

const failure = new ApiError(500, "test_unknown", "boom");

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useRename", () => {
  it("前後の空白を落として送り、閉じる", async () => {
    const renamed = { ...board, name: "新しい名前" };
    const rename = vi.spyOn(boardsApi, "rename").mockResolvedValue(renamed);
    const onRenamed = vi.fn();
    const { result } = renderHook(() =>
      useRename({ board, onRenamed, onError: vi.fn() }),
    );

    act(() => result.current.setDraft("  新しい名前  "));
    await act(() => result.current.rename());

    expect(rename).toHaveBeenCalledWith("b1", "新しい名前");
    expect(onRenamed).toHaveBeenCalledWith(renamed);
    expect(result.current.draft).toBeNull();
    expect(result.current.renaming).toBe(false);
  });

  it.each([
    ["空白だけ", "   "],
    ["今と同じ名前", " ロードマップ "],
  ])("%s なら送らずに閉じる", async (_, draft) => {
    const rename = vi.spyOn(boardsApi, "rename");
    const { result } = renderHook(() =>
      useRename({ board, onRenamed: vi.fn(), onError: vi.fn() }),
    );

    act(() => result.current.setDraft(draft));
    await act(() => result.current.rename());

    expect(rename).not.toHaveBeenCalled();
    expect(result.current.draft).toBeNull();
  });

  it("失敗したら下書きを残して知らせる", async () => {
    vi.spyOn(boardsApi, "rename").mockRejectedValue(failure);
    const onRenamed = vi.fn();
    const onError = vi.fn();
    const { result } = renderHook(() => useRename({ board, onRenamed, onError }));

    act(() => result.current.setDraft("新しい名前"));
    await act(() => result.current.rename());

    expect(result.current.draft).toBe("新しい名前");
    expect(onRenamed).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith({
      message: "名前を変更できませんでした: boom",
      detail: "",
    });
  });
});

describe("useBoardDeletion", () => {
  const losing: BoardDeletion = { recordedItemCount: 3 };

  it("失われるものを引き終わるまで、確認を出さない", async () => {
    const pending = deferred<BoardDeletion>();
    vi.spyOn(boardsApi, "deletion").mockReturnValue(pending.promise);
    const { result } = renderHook(() =>
      useBoardDeletion({ boardId: "b1", onDeleted: vi.fn(), onError: vi.fn() }),
    );

    let asking!: Promise<void>;
    act(() => {
      asking = result.current.ask();
    });
    expect(result.current.state).toEqual({ status: "loading" });

    await act(async () => {
      pending.resolve(losing);
      await asking;
    });
    expect(result.current.state).toEqual({ status: "confirming", losing });
  });

  it("失われるものを引けなかったら、確認を出さずに閉じて知らせる", async () => {
    vi.spyOn(boardsApi, "deletion").mockRejectedValue(failure);
    const onError = vi.fn();
    const { result } = renderHook(() =>
      useBoardDeletion({ boardId: "b1", onDeleted: vi.fn(), onError }),
    );

    await act(() => result.current.ask());

    expect(result.current.state).toBeNull();
    expect(onError).toHaveBeenCalledWith({
      message: "削除で失われるものを確かめられませんでした: boom",
      detail: "",
    });
  });

  it("確認を出していなければ消さない", async () => {
    const remove = vi.spyOn(boardsApi, "delete");
    const { result } = renderHook(() =>
      useBoardDeletion({ boardId: "b1", onDeleted: vi.fn(), onError: vi.fn() }),
    );

    await act(() => result.current.confirm());

    expect(remove).not.toHaveBeenCalled();
  });

  it("消したら、消えた ID を渡す", async () => {
    vi.spyOn(boardsApi, "deletion").mockResolvedValue(losing);
    const remove = vi.spyOn(boardsApi, "delete").mockResolvedValue(undefined);
    const onDeleted = vi.fn();
    const { result } = renderHook(() =>
      useBoardDeletion({ boardId: "b1", onDeleted, onError: vi.fn() }),
    );

    await act(() => result.current.ask());
    await act(() => result.current.confirm());

    expect(remove).toHaveBeenCalledWith("b1");
    expect(onDeleted).toHaveBeenCalledWith("b1");
  });

  it("消せなかったら、確認に戻して知らせる", async () => {
    vi.spyOn(boardsApi, "deletion").mockResolvedValue(losing);
    vi.spyOn(boardsApi, "delete").mockRejectedValue(failure);
    const onDeleted = vi.fn();
    const onError = vi.fn();
    const { result } = renderHook(() =>
      useBoardDeletion({ boardId: "b1", onDeleted, onError }),
    );

    await act(() => result.current.ask());
    await act(() => result.current.confirm());

    expect(result.current.state).toEqual({ status: "confirming", losing });
    expect(onDeleted).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith({
      message: "ボードを削除できませんでした: boom",
      detail: "",
    });
  });

  it("取り消したら確認を閉じる", async () => {
    vi.spyOn(boardsApi, "deletion").mockResolvedValue(losing);
    const { result } = renderHook(() =>
      useBoardDeletion({ boardId: "b1", onDeleted: vi.fn(), onError: vi.fn() }),
    );

    await act(() => result.current.ask());
    act(() => result.current.cancel());

    expect(result.current.state).toBeNull();
  });
});

describe("useTargetRefresh", () => {
  const project: Project = {
    id: "PVT_1",
    number: 12,
    title: "新しい名前",
    url: "https://github.com/orgs/acme/projects/12",
  };

  it("GitHub から受け取った番号・名前・URL をそのまま送る", async () => {
    const projects = vi
      .spyOn(githubApi, "projects")
      .mockResolvedValue([{ ...project, id: "PVT_other", number: 2 }, project]);
    const refreshed = { ...board, projectTitle: "新しい名前" };
    const write = vi
      .spyOn(boardsApi, "refreshTargetDisplay")
      .mockResolvedValue(refreshed);
    const onTargetRefreshed = vi.fn();
    const { result } = renderHook(() =>
      useTargetRefresh({ board, onTargetRefreshed, onError: vi.fn() }),
    );

    await act(() => result.current.refresh());

    expect(projects).toHaveBeenCalledWith("acme", "web");
    expect(write).toHaveBeenCalledWith("b1", {
      projectId: "PVT_1",
      projectNumber: 12,
      projectTitle: "新しい名前",
      projectUrl: "https://github.com/orgs/acme/projects/12",
    });
    expect(onTargetRefreshed).toHaveBeenCalledWith(refreshed);
    expect(result.current.refreshing).toBe(false);
  });

  it("Project が見つからなければ、書かずに知らせる", async () => {
    vi.spyOn(githubApi, "projects").mockResolvedValue([{ ...project, id: "PVT_other" }]);
    const write = vi.spyOn(boardsApi, "refreshTargetDisplay");
    const onError = vi.fn();
    const { result } = renderHook(() =>
      useTargetRefresh({ board, onTargetRefreshed: vi.fn(), onError }),
    );

    await act(() => result.current.refresh());

    expect(write).not.toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith(targetProjectMissingFailure());
    expect(result.current.refreshing).toBe(false);
  });

  it("取り直せなかったら知らせ、取り直し中の印を下ろす", async () => {
    const pending = deferred<Project[]>();
    vi.spyOn(githubApi, "projects").mockReturnValue(pending.promise);
    const write = vi.spyOn(boardsApi, "refreshTargetDisplay").mockRejectedValue(failure);
    const onError = vi.fn();
    const { result } = renderHook(() =>
      useTargetRefresh({ board, onTargetRefreshed: vi.fn(), onError }),
    );

    let refreshing!: Promise<void>;
    act(() => {
      refreshing = result.current.refresh();
    });
    expect(result.current.refreshing).toBe(true);

    await act(async () => {
      pending.resolve([project]);
      await refreshing;
    });
    expect(write).toHaveBeenCalled();
    expect(onError).toHaveBeenCalledWith({
      message: "作成先の名前を取り直せませんでした: boom",
      detail: "",
    });
    expect(result.current.refreshing).toBe(false);
  });
});
