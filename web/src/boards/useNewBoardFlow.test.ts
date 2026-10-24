import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../api/boards";
import { describeFailure } from "../api/errorMessage";
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
  premise: "",
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

/**
 * 作成先を選ばずに作る口（GitHub が未設定の構成、ADR 0090）。
 *
 * **守りたいのは、作成先を送らないことと、失敗したら入力を残すこと。** 送って
 * しまうと GitHub の無いサーバーでも選んだ体の値が残り、失敗で入力を消すと
 * 直して押し直す元が無くなる。
 */
describe("useNewBoardFlow の createWithoutTarget", () => {
  it("作成先を送らずに作り、ダイアログを閉じて入力を既定に戻す", async () => {
    const create = vi.spyOn(boardsApi, "create").mockResolvedValue(created);
    const { result } = typed("  新しいボード  ");
    act(() => result.current.setTemplate("sequence"));

    let board: BoardDetail | null = null;
    await act(async () => {
      board = await result.current.createWithoutTarget(() => true);
    });

    const [sentName, sentTarget, scene] = create.mock.calls[0] ?? [];
    expect(sentName).toBe("新しいボード");
    expect(sentTarget).toBeNull();
    // ひな形は選択画面を通る流れと同じく添える。
    expect(JSON.parse(scene ?? "{}").elements.length).toBeGreaterThan(0);
    expect(board).toEqual(created);
    expect(result.current.dialogOpen).toBe(false);
    expect(result.current.name).toBe("");
    expect(result.current.template).toBe(BLANK_TEMPLATE);
    // 選択画面には入らない。
    expect(result.current.creating).toBeNull();
    expect(result.current.submitting).toBe(false);
  });

  it("名前が空白だけなら作らず、未保存の確認も訊かない", async () => {
    const create = vi.spyOn(boardsApi, "create");
    const { result } = typed("   ");
    const confirm = vi.fn(() => true);

    let board: BoardDetail | null = created;
    await act(async () => {
      board = await result.current.createWithoutTarget(confirm);
    });

    expect(board).toBeNull();
    expect(confirm).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
  });

  it("未保存の変更を捨てないと言われたら作らない", async () => {
    const create = vi.spyOn(boardsApi, "create");
    const { result } = typed("新しいボード");

    let board: BoardDetail | null = created;
    await act(async () => {
      board = await result.current.createWithoutTarget(() => false);
    });

    expect(board).toBeNull();
    expect(create).not.toHaveBeenCalled();
    expect(result.current.dialogOpen).toBe(true);
  });

  // 理由はダイアログの中に出す（`failure`）。モーダルのダイアログの外に出す
  // 通知は、ダイアログの下に隠れて読めない。
  it("作れなかったら理由を持って null を返し、ダイアログと入力を残す", async () => {
    const error = new ApiError(500, "test_unknown", "boom");
    vi.spyOn(boardsApi, "create").mockRejectedValue(error);
    const { result } = typed("新しいボード");
    act(() => result.current.setTemplate("sequence"));

    let board: BoardDetail | null = created;
    await act(async () => {
      board = await result.current.createWithoutTarget(() => true);
    });

    expect(board).toBeNull();
    expect(result.current.failure).toEqual(
      describeFailure("ボードを作成できませんでした", error),
    );
    expect(result.current.dialogOpen).toBe(true);
    expect(result.current.name).toBe("新しいボード");
    expect(result.current.template).toBe("sequence");
    expect(result.current.submitting).toBe(false);
  });

  it("押し直したら、作っているあいだは前の理由を出さない", async () => {
    const create = vi
      .spyOn(boardsApi, "create")
      .mockRejectedValueOnce(new ApiError(500, "test_unknown", "boom"));
    const { result } = typed("新しいボード");
    await act(async () => {
      await result.current.createWithoutTarget(() => true);
    });
    expect(result.current.failure).not.toBeNull();

    let resolve: (b: BoardDetail) => void = () => {};
    create.mockReturnValueOnce(
      new Promise<BoardDetail>((r) => {
        resolve = r;
      }),
    );
    let retried: Promise<BoardDetail | null> = Promise.resolve(null);
    act(() => {
      retried = result.current.createWithoutTarget(() => true);
    });

    expect(result.current.submitting).toBe(true);
    expect(result.current.failure).toBeNull();
    await act(async () => {
      resolve(created);
      await retried;
    });
  });

  it("やめたら理由を消し、次に開いたときに出さない", async () => {
    vi.spyOn(boardsApi, "create").mockRejectedValue(
      new ApiError(500, "test_unknown", "boom"),
    );
    const { result } = typed("新しいボード");
    await act(async () => {
      await result.current.createWithoutTarget(() => true);
    });
    expect(result.current.failure).not.toBeNull();

    act(() => result.current.cancel());
    act(() => result.current.openDialog());

    expect(result.current.failure).toBeNull();
  });

  // 押した時点で弾く。state だと同じ tick の 2 回目がまだ false を読み、同じ
  // 名前のボードが 2 枚できる。
  it("作っているあいだにもう一度押しても、2 本目を投げない", async () => {
    let resolve: (b: BoardDetail) => void = () => {};
    const create = vi.spyOn(boardsApi, "create").mockReturnValue(
      new Promise<BoardDetail>((r) => {
        resolve = r;
      }),
    );
    const { result } = typed("新しいボード");

    let boards: (BoardDetail | null)[] = [];
    await act(async () => {
      const first = result.current.createWithoutTarget(() => true);
      const second = result.current.createWithoutTarget(() => true);
      resolve(created);
      boards = await Promise.all([first, second]);
    });

    expect(create).toHaveBeenCalledTimes(1);
    expect(boards).toEqual([created, null]);
  });
});
