import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../api/boards";
import type { BoardDetail, BoardTarget } from "../api/types";
import { BLANK_TEMPLATE, templateScene } from "../excalidraw/template";
import { useNewBoardFlow } from "./useNewBoardFlow";

/**
 * 新しいボードを作るまでの流れ（#230 で `App` から切り出し）。
 *
 * ダイアログで名前とひな形を決め、作成先を選んでから作る（#200、ADR 0017）。
 * **守りたいのは、選び直すために戻った人に打ち直させないことと、やめた入力を
 * 次に持ち越さないこと。**
 */

const target: BoardTarget = {
  repositoryOwner: "acme",
  repositoryName: "web",
  projectId: "PVT_1",
  projectNumber: 1,
  projectTitle: "ロードマップ",
  projectUrl: "https://github.com/orgs/acme/projects/1",
};

const created: BoardDetail = {
  id: "b-new",
  name: "新しいボード",
  role: "owner",
  createdAt: "2026-10-07T00:00:00Z",
  updatedAt: "2026-10-07T00:00:00Z",
  ...target,
  projectNumber: 1,
  projectTitle: "ロードマップ",
  projectUrl: "https://github.com/orgs/acme/projects/1",
  scene: "{}",
  targetLocked: false,
  sceneOverLimit: false,
};

afterEach(() => {
  vi.restoreAllMocks();
});

/** ダイアログを開いて名前を打った状態から始める。 */
function typed(name: string) {
  const hook = renderHook(() => useNewBoardFlow());
  act(() => {
    hook.result.current.openDialog();
    hook.result.current.setName(name);
  });
  return hook;
}

describe("useNewBoardFlow", () => {
  it("名前が空白だけなら進まず、未保存の確認も訊かない", () => {
    const { result } = typed("   ");
    const confirm = vi.fn(() => true);

    let started = true;
    act(() => {
      started = result.current.start(confirm);
    });

    expect(started).toBe(false);
    expect(confirm).not.toHaveBeenCalled();
    expect(result.current.dialogOpen).toBe(true);
    expect(result.current.creating).toBeNull();
  });

  it("未保存の変更を捨てないと言われたら進まず、入力もダイアログもそのまま", () => {
    const { result } = typed("新しいボード");

    let started = true;
    act(() => {
      started = result.current.start(() => false);
    });

    expect(started).toBe(false);
    expect(result.current.dialogOpen).toBe(true);
    expect(result.current.name).toBe("新しいボード");
    expect(result.current.creating).toBeNull();
  });

  it("進むとダイアログを閉じ、前後の空白を落とした名前で作成先の選択に入る。入力は残す", () => {
    const { result } = typed("  新しいボード  ");

    let started = false;
    act(() => {
      started = result.current.start(() => true);
    });

    expect(started).toBe(true);
    expect(result.current.dialogOpen).toBe(false);
    expect(result.current.creating).toBe("新しいボード");
    expect(result.current.name).toBe("  新しいボード  ");
  });

  it("作成先の選択をやめたら、入力を残したままダイアログを開き直す", () => {
    const { result } = typed("新しいボード");
    act(() => {
      result.current.setTemplate("sequence");
      result.current.start(() => true);
    });

    act(() => result.current.backToDialog());

    expect(result.current.creating).toBeNull();
    expect(result.current.dialogOpen).toBe(true);
    expect(result.current.name).toBe("新しいボード");
    expect(result.current.template).toBe("sequence");
  });

  it("ダイアログをやめたら、名前とひな形を既定に戻す", () => {
    const { result } = typed("新しいボード");
    act(() => result.current.setTemplate("sequence"));

    act(() => result.current.cancel());

    expect(result.current.dialogOpen).toBe(false);
    expect(result.current.name).toBe("");
    expect(result.current.template).toBe(BLANK_TEMPLATE);
  });

  it("作成先が決まったら、選んだひな形のシーンを添えて作り、入力を既定に戻す", async () => {
    const create = vi.spyOn(boardsApi, "create").mockResolvedValue(created);
    const { result } = typed("新しいボード");
    act(() => {
      result.current.setTemplate("sequence");
      result.current.start(() => true);
    });

    let board: BoardDetail | null = null;
    await act(async () => {
      board = await result.current.create(target);
    });

    // ひな形の要素は組み立てるたびに ID が振り直されるので、形で比べる。
    const [sentName, sentTarget, scene] = create.mock.calls[0] ?? [];
    expect(sentName).toBe("新しいボード");
    expect(sentTarget).toEqual(target);
    const types = (s: string | undefined) =>
      (JSON.parse(s ?? "{}") as { elements: { type: string }[] }).elements.map(
        (e) => e.type,
      );
    expect(types(scene)).toEqual(types(templateScene("sequence")));
    expect(types(scene).length).toBeGreaterThan(0);
    expect(board).toEqual(created);
    expect(result.current.creating).toBeNull();
    expect(result.current.name).toBe("");
    expect(result.current.template).toBe(BLANK_TEMPLATE);
  });

  // 空のシーンを持っているのはサーバー（`web/CLAUDE.md` のひな形の節）。
  it("ひな形が空白なら、シーンを送らない", async () => {
    const create = vi.spyOn(boardsApi, "create").mockResolvedValue(created);
    const { result } = typed("新しいボード");
    act(() => {
      result.current.start(() => true);
    });

    await act(() => result.current.create(target));

    expect(create).toHaveBeenCalledWith("新しいボード", target, undefined);
  });

  it("作れなかったら投げ、入力は残す（作成先の選択が失敗を出す）", async () => {
    vi.spyOn(boardsApi, "create").mockRejectedValue(
      new ApiError(500, "test_unknown", "boom"),
    );
    const { result } = typed("新しいボード");
    act(() => {
      result.current.setTemplate("sequence");
      result.current.start(() => true);
    });

    await act(async () => {
      await expect(result.current.create(target)).rejects.toThrow("boom");
    });

    expect(result.current.creating).toBe("新しいボード");
    expect(result.current.name).toBe("新しいボード");
    expect(result.current.template).toBe("sequence");
  });

  it("作成先を選んでいないときは作らない", async () => {
    const create = vi.spyOn(boardsApi, "create");
    const { result } = renderHook(() => useNewBoardFlow());

    let board: BoardDetail | null = created;
    await act(async () => {
      board = await result.current.create(target);
    });

    expect(board).toBeNull();
    expect(create).not.toHaveBeenCalled();
  });
});
