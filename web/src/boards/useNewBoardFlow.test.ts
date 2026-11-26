import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../api/boards";
import { describeFailure } from "../api/errorMessage";
import type { BoardDetail } from "../api/types";
import { BLANK_TEMPLATE, templateScene } from "../excalidraw/template";
import { useNewBoardFlow } from "./useNewBoardFlow";

/**
 * 新しいボードを作るまでの流れ（#230 で `App` から切り出し）。
 *
 * ダイアログで名前とひな形を決め、その場で作る。**作成先は選ばない**
 * （ADR 0100）。守りたいのは、失敗したら入力を残すことと、やめた入力を次に
 * 持ち越さないこと。失敗で入力を消すと、直して押し直す元が無くなる。
 */

const created: BoardDetail = {
  id: "b-new",
  name: "新しいボード",
  role: "owner",
  createdAt: "2026-10-07T00:00:00Z",
  updatedAt: "2026-10-07T00:00:00Z",
  repositoryOwner: "",
  repositoryName: "",
  projectId: "",
  projectNumber: 0,
  projectTitle: "",
  projectUrl: "",
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
  it("前後の空白を落とした名前と選んだひな形で作り、ダイアログを閉じて入力を既定に戻す", async () => {
    const create = vi.spyOn(boardsApi, "create").mockResolvedValue(created);
    const { result } = typed("  新しいボード  ");
    act(() => result.current.setTemplate("sequence"));

    let board: BoardDetail | null = null;
    await act(async () => {
      board = await result.current.create(() => true);
    });

    // ひな形の要素は組み立てるたびに ID が振り直されるので、形で比べる。
    // **作成先は引数に無い**（`boardsApi.create` が送らない、ADR 0100）。
    const [sentName, scene] = create.mock.calls[0] ?? [];
    expect(sentName).toBe("新しいボード");
    const types = (s: string | undefined) =>
      (JSON.parse(s ?? "{}") as { elements: { type: string }[] }).elements.map(
        (e) => e.type,
      );
    expect(types(scene)).toEqual(types(templateScene("sequence")));
    expect(types(scene).length).toBeGreaterThan(0);
    expect(board).toEqual(created);
    expect(result.current.dialogOpen).toBe(false);
    expect(result.current.name).toBe("");
    expect(result.current.template).toBe(BLANK_TEMPLATE);
    expect(result.current.submitting).toBe(false);
  });

  // 空のシーンを持っているのはサーバー（`web/CLAUDE.md` のひな形の節）。
  it("ひな形が空白なら、シーンを送らない", async () => {
    const create = vi.spyOn(boardsApi, "create").mockResolvedValue(created);
    const { result } = typed("新しいボード");

    await act(() => result.current.create(() => true));

    expect(create).toHaveBeenCalledWith("新しいボード", undefined);
  });

  it("名前が空白だけなら作らず、未保存の確認も訊かない", async () => {
    const create = vi.spyOn(boardsApi, "create");
    const { result } = typed("   ");
    const confirm = vi.fn(() => true);

    let board: BoardDetail | null = created;
    await act(async () => {
      board = await result.current.create(confirm);
    });

    expect(board).toBeNull();
    expect(confirm).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
    expect(result.current.dialogOpen).toBe(true);
  });

  it("未保存の変更を捨てないと言われたら作らず、入力もダイアログもそのまま", async () => {
    const create = vi.spyOn(boardsApi, "create");
    const { result } = typed("新しいボード");

    let board: BoardDetail | null = created;
    await act(async () => {
      board = await result.current.create(() => false);
    });

    expect(board).toBeNull();
    expect(create).not.toHaveBeenCalled();
    expect(result.current.dialogOpen).toBe(true);
    expect(result.current.name).toBe("新しいボード");
  });

  it("ダイアログをやめたら、名前とひな形を既定に戻す", () => {
    const { result } = typed("新しいボード");
    act(() => result.current.setTemplate("sequence"));

    act(() => result.current.cancel());

    expect(result.current.dialogOpen).toBe(false);
    expect(result.current.name).toBe("");
    expect(result.current.template).toBe(BLANK_TEMPLATE);
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
      board = await result.current.create(() => true);
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
      await result.current.create(() => true);
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
      retried = result.current.create(() => true);
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
      await result.current.create(() => true);
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
    // **弾いた 2 回目は確認も呼ばない。** 呼ぶ側（`useBoardNavigation`）は確認の
    // 中で送る前の世代を控えるので、呼ぶと作っている最中の作成を古いものにする
    // （#481 の full review）。
    const secondConfirm = vi.fn(() => true);
    await act(async () => {
      const first = result.current.create(() => true);
      const second = result.current.create(secondConfirm);
      resolve(created);
      boards = await Promise.all([first, second]);
    });

    expect(create).toHaveBeenCalledTimes(1);
    expect(boards).toEqual([created, null]);
    expect(secondConfirm).not.toHaveBeenCalled();
  });
});
