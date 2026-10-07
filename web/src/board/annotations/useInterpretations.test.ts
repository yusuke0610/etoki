import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, boardsApi } from "../../api/boards";
import type {
  AnnotationImage,
  AnnotationStatus,
  Interpretation,
  SyncItem,
} from "../../api/types";
import { exportAnnotationImage } from "../../excalidraw/image";
import { useInterpretations } from "./useInterpretations";

// 画像の書き出しは canvas が要るので jsdom では動かない（`web/CLAUDE.md`）。
// ここで見るのは、書き出した画像がそのまま解釈に渡ることだけ。
vi.mock("../../excalidraw/image", () => ({ exportAnnotationImage: vi.fn() }));

/**
 * 注釈の解釈と、引いた解釈の履歴（#146 で `BoardPage` から切り出し）。
 *
 * **守りたいのは 2 つ。** 保存を挟んだら保存前のシーンに対する解釈を出さない
 * こと（`web/CLAUDE.md`「引いた解釈と実行の履歴」）と、引き直しに失敗しても
 * 前の結果を消さないこと。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const annotation = (id: string, granularity: AnnotationStatus["granularity"]) =>
  ({ id, name: id, granularity, state: "uncreated" }) satisfies AnnotationStatus;

const result = (summary: string): Interpretation => ({
  summary,
  contentHash: "h",
  items: [],
});

const image: AnnotationImage = { mediaType: "image/png", data: "AAAA" };
const api = {} as ExcalidrawImperativeAPI;

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(exportAnnotationImage).mockReset();
});

describe("useInterpretations", () => {
  it("解釈すると結果が積まれて選ばれ、実行したときの粒度を控える", async () => {
    vi.mocked(exportAnnotationImage).mockResolvedValue(image);
    const interpret = vi
      .spyOn(boardsApi, "interpret")
      .mockResolvedValue(result("一つ目"));
    const { result: hook } = renderHook(() =>
      useInterpretations({ api, boardId: "b1", annotations: [annotation("a1", "epic")] }),
    );

    await act(() => hook.current.interpret("a1"));

    expect(interpret).toHaveBeenCalledWith("b1", "a1", image);
    const state = hook.current.states.a1;
    expect(state?.running).toBe(false);
    expect(state?.runs).toHaveLength(1);
    expect(state?.runs[0]?.granularity).toBe("epic");
    expect(state?.runs[0]?.result).toEqual(result("一つ目"));
    expect(state?.selectedId).toBe(state?.runs[0]?.id);
  });

  it("失敗しても、前の結果は消さない", async () => {
    const interpret = vi
      .spyOn(boardsApi, "interpret")
      .mockResolvedValueOnce(result("一つ目"))
      .mockRejectedValueOnce(new ApiError(500, "test_unknown", "boom"));
    const { result: hook } = renderHook(() =>
      useInterpretations({
        api: null,
        boardId: "b1",
        annotations: [annotation("a1", "")],
      }),
    );

    await act(() => hook.current.interpret("a1"));
    await act(() => hook.current.interpret("a1"));

    expect(interpret).toHaveBeenCalledTimes(2);
    const state = hook.current.states.a1;
    expect(state?.failure).toEqual({ message: "解釈できませんでした: boom", detail: "" });
    expect(state?.runs.map((r) => r.result.summary)).toEqual(["一つ目"]);
  });

  it("同じ注釈を引き直したら、先に投げた応答は捨てる", async () => {
    const first = deferred<Interpretation>();
    const second = deferred<Interpretation>();
    vi.spyOn(boardsApi, "interpret")
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const { result: hook } = renderHook(() =>
      useInterpretations({
        api: null,
        boardId: "b1",
        annotations: [annotation("a1", "")],
      }),
    );

    let a!: Promise<void>;
    let b!: Promise<void>;
    act(() => {
      a = hook.current.interpret("a1");
      b = hook.current.interpret("a1");
    });
    await act(async () => {
      second.resolve(result("後"));
      first.resolve(result("先"));
      await Promise.all([a, b]);
    });

    expect(hook.current.states.a1?.runs.map((r) => r.result.summary)).toEqual(["後"]);
  });

  it("保存を挟んだら、遅れて届いた応答を入れず、全部消える", async () => {
    const pending = deferred<Interpretation>();
    vi.spyOn(boardsApi, "interpret")
      .mockResolvedValueOnce(result("保存前に引いたもの"))
      .mockReturnValueOnce(pending.promise);
    const { result: hook } = renderHook(() =>
      useInterpretations({
        api: null,
        boardId: "b1",
        annotations: [annotation("a1", "")],
      }),
    );

    await act(() => hook.current.interpret("a1"));
    let running!: Promise<void>;
    act(() => {
      running = hook.current.interpret("a1");
    });
    act(() => hook.current.discardAll());
    expect(hook.current.states).toEqual({});

    await act(async () => {
      pending.resolve(result("遅れて届いた"));
      await running;
    });
    expect(hook.current.states).toEqual({});
  });

  it("作ったものを、その解釈に結びつける。解釈の無い注釈には何もしない", async () => {
    vi.spyOn(boardsApi, "interpret").mockResolvedValue(result("一つ目"));
    const { result: hook } = renderHook(() =>
      useInterpretations({
        api: null,
        boardId: "b1",
        annotations: [annotation("a1", "")],
      }),
    );
    await act(() => hook.current.interpret("a1"));
    const runId = hook.current.states.a1?.runs[0]?.id ?? -1;
    const items: SyncItem[] = [
      {
        itemId: "I_1",
        kind: "issue",
        title: "t",
        body: "",
        localId: "l1",
        action: "created",
        confirmed: true,
      },
    ];

    act(() => hook.current.recordCreated("a1", runId, items));
    act(() => hook.current.recordCreated("a2", 1, items));

    expect(hook.current.states.a1?.runs[0]?.created).toEqual([items]);
    expect(hook.current.states).not.toHaveProperty("a2");
  });

  it("見る解釈の選び直しは画面の中だけで、API を呼ばない", async () => {
    const interpret = vi
      .spyOn(boardsApi, "interpret")
      .mockResolvedValueOnce(result("一つ目"))
      .mockResolvedValueOnce(result("二つ目"));
    const { result: hook } = renderHook(() =>
      useInterpretations({
        api: null,
        boardId: "b1",
        annotations: [annotation("a1", "")],
      }),
    );
    await act(() => hook.current.interpret("a1"));
    await act(() => hook.current.interpret("a1"));
    const older = hook.current.states.a1?.runs.find((r) => r.result.summary === "一つ目");

    act(() => hook.current.select("a1", older?.id ?? -1));

    expect(hook.current.states.a1?.selectedId).toBe(older?.id);
    expect(interpret).toHaveBeenCalledTimes(2);
  });
});
