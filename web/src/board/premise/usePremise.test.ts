import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import type { BoardDetail, BoardWithFiles } from "../../api/types";
import { usePremise } from "./usePremise";

/**
 * ボードの背景の編集（ADR 0093）。
 *
 * **書きかけを黙って失わないこと**と、**先に保存した人の背景を黙って上書き
 * しないこと**を固定する。
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
  role: "editor",
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-07T00:00:00Z",
  repositoryOwner: "acme",
  repositoryName: "web",
  projectId: "PVT_1",
  projectNumber: 1,
  projectTitle: "ロードマップ",
  projectUrl: "https://github.com/orgs/acme/projects/1",
  scene: "{}",
  premise: "前の背景",
  targetLocked: false,
  sceneOverLimit: false,
};

const conflict = new ApiError(409, "premise_conflict", "premise changed");

afterEach(() => {
  vi.restoreAllMocks();
});

function setup(onChanged = vi.fn()) {
  return renderHook(() => usePremise({ board, onChanged }));
}

describe("usePremise", () => {
  it("書き換えた背景を、編集を始めた時点の背景と一緒に送り、閉じる", async () => {
    const saved = { ...board, premise: "新しい背景" };
    const setPremise = vi.spyOn(boardsApi, "setPremise").mockResolvedValue(saved);
    const onChanged = vi.fn();
    const { result } = setup(onChanged);

    act(() => result.current.openEditor());
    act(() => result.current.change("新しい背景"));
    await act(() => result.current.save());

    expect(setPremise).toHaveBeenCalledWith("b1", "新しい背景", "前の背景");
    expect(onChanged).toHaveBeenCalledWith(saved);
    expect(result.current.open).toBe(false);
    expect(result.current.edited).toBe(false);
    expect(result.current.failure).toBeNull();
  });

  it("書き換えていなければ送らずに閉じる", async () => {
    const setPremise = vi.spyOn(boardsApi, "setPremise");
    const { result } = setup();

    act(() => result.current.openEditor());
    act(() => result.current.change("前の背景"));
    await act(() => result.current.save());

    expect(setPremise).not.toHaveBeenCalled();
    expect(result.current.open).toBe(false);
  });

  // ダイアログは閉じると中身を外す。書きかけをそちらに持つと、閉じただけで消える。
  it("閉じても書きかけは残り、開き直すと同じ書きかけが出る", () => {
    const { result } = setup();

    act(() => result.current.openEditor());
    act(() => result.current.change("書きかけ"));
    act(() => result.current.closeEditor());
    act(() => result.current.openEditor());

    expect(result.current.text).toBe("書きかけ");
    expect(result.current.edited).toBe(true);
  });

  // 持ち越すと、閉じているあいだに届いた新しい背景ではなく、開いた時点の背景を
  // 出し続ける。
  it("書き換えずに閉じたら、書きかけを持ち越さない", () => {
    const { result, rerender } = setup();

    act(() => result.current.openEditor());
    act(() => result.current.change("書きかけ"));
    act(() => result.current.change("前の背景"));
    act(() => result.current.closeEditor());
    rerender();

    expect(result.current.edited).toBe(false);
    expect(result.current.text).toBe("前の背景");
  });

  it("書きかけを捨てると、いまの背景に戻る", () => {
    const { result } = setup();

    act(() => result.current.change("書きかけ"));
    act(() => result.current.discard());

    expect(result.current.text).toBe("前の背景");
    expect(result.current.edited).toBe(false);
  });

  // 送った値のままのときだけ消す（`.claude/rules/async-ui.md`）。待つあいだの
  // 書き足しを捨てない。
  it("保存を待つあいだに書き足したぶんは残し、基準を保存した背景へ進める", async () => {
    const pending = deferred<BoardDetail>();
    const setPremise = vi
      .spyOn(boardsApi, "setPremise")
      .mockReturnValueOnce(pending.promise)
      .mockResolvedValue({ ...board, premise: "書き足した背景" });
    const { result } = setup();

    act(() => result.current.openEditor());
    act(() => result.current.change("新しい背景"));
    let saving!: Promise<void>;
    act(() => {
      saving = result.current.save();
    });
    act(() => result.current.change("書き足した背景"));
    await act(async () => {
      pending.resolve({ ...board, premise: "新しい背景" });
      await saving;
    });

    expect(result.current.text).toBe("書き足した背景");
    expect(result.current.edited).toBe(true);
    expect(result.current.open).toBe(true);

    await act(() => result.current.save());
    expect(setPremise).toHaveBeenLastCalledWith("b1", "書き足した背景", "新しい背景");
  });

  it("先に書き換えられていたら、いまの背景を取り直して並べ、見比べたうえで押し直せる", async () => {
    const fresh: BoardWithFiles = { ...board, premise: "ほかの人の背景", files: {} };
    const setPremise = vi
      .spyOn(boardsApi, "setPremise")
      .mockRejectedValueOnce(conflict)
      .mockResolvedValue({ ...board, premise: "新しい背景" });
    vi.spyOn(boardsApi, "get").mockResolvedValue(fresh);
    const onChanged = vi.fn();
    const { result } = setup(onChanged);

    act(() => result.current.openEditor());
    act(() => result.current.change("新しい背景"));
    await act(() => result.current.save());

    expect(result.current.failure?.message).toMatch(
      /ほかの人が先にボードの背景を書き換えました/,
    );
    expect(result.current.latest).toBe("ほかの人の背景");
    // 書きかけは捨てない。閉じもしない。
    expect(result.current.text).toBe("新しい背景");
    expect(result.current.open).toBe(true);
    // 手元のボードもいまの背景に差し替える。
    expect(onChanged).toHaveBeenCalledWith(fresh);

    await act(() => result.current.save());
    expect(setPremise).toHaveBeenLastCalledWith("b1", "新しい背景", "ほかの人の背景");
    expect(result.current.latest).toBeNull();
    expect(result.current.failure).toBeNull();
  });

  // 取り直せないのに基準だけ進めると、見ていない背景を黙って上書きする。
  it("いまの背景を取り直せなければ、基準を据え置く", async () => {
    const setPremise = vi.spyOn(boardsApi, "setPremise").mockRejectedValue(conflict);
    vi.spyOn(boardsApi, "get").mockRejectedValue(new TypeError("Failed to fetch"));
    const { result } = setup();

    act(() => result.current.change("新しい背景"));
    await act(() => result.current.save());
    await act(() => result.current.save());

    expect(result.current.latest).toBeNull();
    expect(setPremise).toHaveBeenLastCalledWith("b1", "新しい背景", "前の背景");
  });

  it("長すぎて断られたら理由を出し、書きかけを残す", async () => {
    vi.spyOn(boardsApi, "setPremise").mockRejectedValue(
      new ApiError(413, "premise_too_large", "premise has 4001 characters"),
    );
    const get = vi.spyOn(boardsApi, "get");
    const { result } = setup();

    act(() => result.current.openEditor());
    act(() => result.current.change("長い背景"));
    await act(() => result.current.save());

    expect(result.current.failure?.message).toMatch(/長すぎて保存できません/);
    expect(result.current.text).toBe("長い背景");
    expect(result.current.open).toBe(true);
    // 409 でなければ取り直さない。
    expect(get).not.toHaveBeenCalled();
  });

  it("続けて押しても 1 回だけ送る", async () => {
    const pending = deferred<BoardDetail>();
    const setPremise = vi.spyOn(boardsApi, "setPremise").mockReturnValue(pending.promise);
    const { result } = setup();

    act(() => result.current.change("新しい背景"));
    let first!: Promise<void>;
    let second!: Promise<void>;
    act(() => {
      first = result.current.save();
      second = result.current.save();
    });
    await act(async () => {
      pending.resolve({ ...board, premise: "新しい背景" });
      await Promise.all([first, second]);
    });

    expect(setPremise).toHaveBeenCalledTimes(1);
  });
});
