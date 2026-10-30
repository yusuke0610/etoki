import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { sceneFileUnreadableFailure, type Failure } from "../api/errorMessage";
import { log } from "../app/logger";
import type { SceneElement } from "../excalidraw/annotation";
import { readSceneFile, type ImportedScene } from "../excalidraw/transfer";
import type { Exclusion } from "./exclusion";
import { useBoardTransfer } from "./useBoardTransfer";

// `loadFromBlob` は Blob の読み取りと Excalidraw の復元が要り、jsdom では動かない
// （`transfer.test.ts`）。読むところだけ差し替え、詰め替えは実物を通す。実物を
// 通した取り込みは E2E（`web/e2e/transfer.spec.ts`）が見る。
vi.mock("../excalidraw/transfer", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../excalidraw/transfer")>()),
  readSceneFile: vi.fn(),
}));

/**
 * 取り込み（ADR 0045、#146 で `BoardPage` から切り出し）。
 *
 * **守りたいのは、確認なしに描いたものを置き換えないこと。** 読んでいるあいだの
 * 描き足しを確認なしで捨てない（読んでから訊く、ADR 0021）。読めなかったら
 * キャンバスに触らない。
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

const imported = (patch: Partial<ImportedScene> = {}): ImportedScene => ({
  elements: [
    { id: "img-1", type: "image", fileId: "file-1" },
    { id: "r1", type: "rectangle" },
  ],
  files: {
    "file-1": {
      id: "file-1",
      mimeType: "image/png",
      dataURL: "data:image/png;base64,AA",
    },
  },
  viewBackgroundColor: "#ffc9c9",
  ...patch,
});

function exclusion(blocked = false): Exclusion {
  return {
    running: null,
    reasonFor: () => null,
    run: async (_op, f) => {
      if (blocked) return false;
      await f();
      return true;
    },
  };
}

function setup(initial: { dirty?: boolean; exclusive?: Exclusion } = {}) {
  const canvas = { dirty: initial.dirty ?? false };
  const addFiles = vi.fn();
  const scrollToContent = vi.fn();
  const api = {
    getAppState: () => ({ viewBackgroundColor: "#ffffff" }),
    getSceneElements: () => [],
    getFiles: () => ({}),
    addFiles,
    scrollToContent,
  } as unknown as ExcalidrawImperativeAPI;
  const isDirty = () => canvas.dirty;
  const updateElements =
    vi.fn<(next: SceneElement[], appState?: Record<string, unknown>) => void>();
  const onError = vi.fn<(failure: Failure) => void>();
  const exclusive = initial.exclusive ?? exclusion();

  const hook = renderHook(() =>
    useBoardTransfer({
      api,
      boardName: "ブレスト",
      exclusive,
      isDirty,
      updateElements,
      onError,
    }),
  );
  return { hook, canvas, addFiles, scrollToContent, updateElements, onError };
}

const file = new File(["{}"], "scene.excalidraw");
let confirm: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  confirm = vi.spyOn(window, "confirm").mockReturnValue(true);
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.mocked(readSceneFile).mockReset();
});

describe("useBoardTransfer の取り込み", () => {
  it("読んでから訊く。読んでいるあいだに描き足されたら、それも訊く", async () => {
    const { hook, canvas, updateElements } = setup({ dirty: false });
    const reading = deferred<ImportedScene>();
    vi.mocked(readSceneFile).mockReturnValueOnce(reading.promise);
    confirm.mockReturnValue(false);

    let importing!: Promise<void>;
    act(() => {
      importing = hook.result.current.importScene(file);
    });
    // 押した時点は保存済み。読んでいるあいだに描き足した。
    expect(confirm).not.toHaveBeenCalled();
    canvas.dirty = true;
    await act(async () => {
      reading.resolve(imported());
      await importing;
    });

    // 押した時点の値で決めると、描き足したぶんを確認なしで捨てる。
    expect(confirm).toHaveBeenCalledTimes(1);
    expect(updateElements).not.toHaveBeenCalled();
  });

  it("未保存で断られたら、置き換えない", async () => {
    const { hook, updateElements, addFiles } = setup({ dirty: true });
    vi.mocked(readSceneFile).mockResolvedValueOnce(imported());
    confirm.mockReturnValue(false);

    await act(() => hook.result.current.importScene(file));

    expect(updateElements).not.toHaveBeenCalled();
    expect(addFiles).not.toHaveBeenCalled();
  });

  it("未保存でなければ訊かずに置き換え、背景色と貼ってあった画像も入れる", async () => {
    const { hook, updateElements, addFiles, scrollToContent } = setup({ dirty: false });
    vi.mocked(readSceneFile).mockResolvedValueOnce(imported());

    await act(() => hook.result.current.importScene(file));

    expect(confirm).not.toHaveBeenCalled();
    expect(updateElements).toHaveBeenCalledTimes(1);
    const [elements, appState] = updateElements.mock.calls[0]!;
    expect(elements.map((e) => e.id)).toEqual(["img-1", "r1"]);
    expect(appState).toEqual({ viewBackgroundColor: "#ffc9c9" });
    // 入れ忘れると、画像の要素だけが空白で置かれる。
    expect(addFiles).toHaveBeenCalledWith([imported().files["file-1"]]);
    // 画面の外に置かれると、押しても何も起きていないように見える。
    expect(scrollToContent).toHaveBeenCalledTimes(1);
  });

  it("背景色の無いファイルは、背景を変えない", async () => {
    const { hook, updateElements } = setup();
    vi.mocked(readSceneFile).mockResolvedValueOnce(
      imported({ viewBackgroundColor: undefined }),
    );

    await act(() => hook.result.current.importScene(file));

    expect(updateElements.mock.calls[0]?.[1]).toBeUndefined();
  });

  it("取り込んだ注釈は、新しい ID に移してから置く", async () => {
    // 同じボードへ戻したファイルでも、以前の作成の記録を引き継がない。
    const { hook, updateElements } = setup();
    vi.mocked(readSceneFile).mockResolvedValueOnce(
      imported({
        elements: [
          {
            id: "annotation-before",
            type: "frame",
            customData: { etoki: { granularity: "epic" } },
          },
          { id: "t1", type: "text", frameId: "annotation-before" },
        ],
        files: {},
      }),
    );

    await act(() => hook.result.current.importScene(file));

    const [elements] = updateElements.mock.calls[0]!;
    const frameId = elements[0]?.id;
    expect(frameId).not.toBe("annotation-before");
    expect((elements[1] as { frameId?: string }).frameId).toBe(frameId);
  });

  it("読めなかったら、確認も出さずキャンバスにも触らない", async () => {
    vi.spyOn(log, "error").mockImplementation(() => {});
    const { hook, updateElements, addFiles, onError } = setup({ dirty: true });
    vi.mocked(readSceneFile).mockRejectedValueOnce(new Error("not a scene"));

    await act(() => hook.result.current.importScene(file));

    expect(onError).toHaveBeenCalledWith(sceneFileUnreadableFailure());
    expect(confirm).not.toHaveBeenCalled();
    expect(updateElements).not.toHaveBeenCalled();
    expect(addFiles).not.toHaveBeenCalled();
  });

  it("排他を取れなければ、読みにもいかない", async () => {
    const { hook, updateElements } = setup({ exclusive: exclusion(true) });

    await act(() => hook.result.current.importScene(file));

    expect(readSceneFile).not.toHaveBeenCalled();
    expect(updateElements).not.toHaveBeenCalled();
  });
});
