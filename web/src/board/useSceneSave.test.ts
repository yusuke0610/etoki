import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";

import { ApiError, boardsApi } from "../api/boards";
import type { Failure } from "../api/errorMessage";
import type { SaveSceneResponse } from "../api/types";
import type { SceneElement } from "../excalidraw/annotation";
import { sceneSignature } from "../excalidraw/dirty";
import type { NotifyOptions } from "../notification/types";
import type { Exclusion } from "./exclusion";
import { SAVE_FAILED, useSceneSave } from "./useSceneSave";

/**
 * シーンの保存（#146 で `BoardPage` から切り出し）。
 *
 * **守りたいのは「何の上に描いたか」と「サーバーが何を持っているか」の持ち回り。**
 * 基準の版（ADR 0020 / 0092）を取り違えると、2 回目の保存が必ず 409 になるか、
 * 読み込んでいない相手の保存を黙って上書きする。持っている画像（ADR 0074）を
 * 取り違えると、毎回送り直すか、送らなかった画像が欠ける。どれも画面の見た目
 * では気づけない。
 */

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

const image = (id: string, fileId: string) => ({
  id,
  type: "image",
  fileId,
  x: 0,
  y: 0,
  width: 10,
  height: 10,
  isDeleted: false,
});

const file = (id: string) => ({
  id,
  mimeType: "image/png",
  dataURL: `data:image/png;base64,${id}`,
  created: 1,
});

const conflict = new ApiError(409, "scene_conflict", "stale");
const tooLarge = new ApiError(413, "scene_too_large", "too large");
const boom = new ApiError(500, "test_unknown", "boom");

const saved = (updatedAt: string, fileIds: string[] = []): SaveSceneResponse => ({
  updatedAt,
  fileIds,
});

/** 排他。既定は取れる。`blocked` なら `f` を呼ばずに false を返す。 */
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

type Props = { updatedAt: string };

function setup(
  initial: {
    fileIds?: string[];
    sceneOverLimit?: boolean;
    exclusive?: Exclusion;
    withApi?: boolean;
  } = {},
) {
  // キャンバスの中身。保存のあいだに書き換えて、送った内容との違いを作る。
  const canvas = {
    elements: [image("img-1", "f1"), image("img-2", "f2")] as unknown[],
    files: { f1: file("f1"), f2: file("f2") } as Record<string, unknown>,
    background: "#ffffff",
  };
  const api = {
    getSceneElements: () => canvas.elements,
    getAppState: () => ({ viewBackgroundColor: canvas.background }),
    getFiles: () => canvas.files,
  } as unknown as ExcalidrawImperativeAPI;

  const markSaved = vi.fn<(signature: string) => void>();
  const onSaved = vi.fn<() => Promise<void>>().mockResolvedValue(undefined);
  const onError =
    vi.fn<(failure: Failure, options?: Pick<NotifyOptions, "action" | "key">) => void>();
  const dismiss = vi.fn<(key: string) => void>();
  const exclusive = initial.exclusive ?? exclusion();
  const currentBackground = () => canvas.background;
  const fileIds = initial.fileIds ?? [];

  const hook = renderHook(
    ({ updatedAt }: Props) =>
      useSceneSave({
        api: initial.withApi === false ? null : api,
        boardId: "b1",
        updatedAt,
        fileIds,
        sceneOverLimit: initial.sceneOverLimit ?? false,
        exclusive,
        currentBackground,
        markSaved,
        onSaved,
        onError,
        dismiss,
      }),
    { initialProps: { updatedAt: "v1" } },
  );
  return { hook, canvas, markSaved, onSaved, onError, dismiss };
}

/** 送った基準の版。 */
const sentBase = (call: number) => vi.mocked(boardsApi.saveScene).mock.calls[call]?.[3];
/** 送った画像。 */
const sentFiles = (call: number) =>
  Object.keys(vi.mocked(boardsApi.saveScene).mock.calls[call]?.[2] ?? {}).sort();

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useSceneSave", () => {
  describe("基準の版（ADR 0020 / 0092）", () => {
    it("返った版を、次の保存の基準にする", async () => {
      vi.spyOn(boardsApi, "saveScene")
        .mockResolvedValueOnce(saved("v2"))
        .mockResolvedValueOnce(saved("v3"));
      const { hook } = setup();

      await act(() => hook.result.current.save());
      await act(() => hook.result.current.save());

      // 捨てると 2 回目の保存が必ず衝突する。
      expect(sentBase(0)).toBe("v1");
      expect(sentBase(1)).toBe("v2");
    });

    it("渡された版が変わったら（ボードを読み直した）、それを基準にする", async () => {
      vi.spyOn(boardsApi, "saveScene").mockResolvedValue(saved("v10"));
      const { hook } = setup();

      hook.rerender({ updatedAt: "v9" });
      await act(() => hook.result.current.save());

      expect(sentBase(0)).toBe("v9");
    });
  });

  describe("持っている画像（ADR 0074）", () => {
    it("サーバーがまだ持っていない画像だけを送り、返った ID を次の把握にする", async () => {
      vi.spyOn(boardsApi, "saveScene")
        .mockResolvedValueOnce(saved("v2", ["f1", "f2"]))
        .mockResolvedValueOnce(saved("v3", ["f1", "f2"]));
      const { hook } = setup({ fileIds: ["f1"] });

      await act(() => hook.result.current.save());
      await act(() => hook.result.current.save());

      // 開いた時点で持っていた f1 は送らない。2 回目は何も送らない。
      expect(sentFiles(0)).toEqual(["f2"]);
      expect(sentFiles(1)).toEqual([]);
    });

    it("衝突や失敗では把握を動かさず、次の保存でも同じ画像を送る", async () => {
      vi.spyOn(boardsApi, "saveScene")
        .mockRejectedValueOnce(boom)
        .mockRejectedValueOnce(conflict)
        .mockResolvedValueOnce(saved("v2", ["f1", "f2"]));
      const { hook } = setup({ fileIds: ["f1"] });

      await act(() => hook.result.current.save());
      await act(() => hook.result.current.save());
      await act(() => hook.result.current.save());

      // 書けていないので、サーバーの持ち物も変わっていない。
      expect(sentFiles(0)).toEqual(["f2"]);
      expect(sentFiles(1)).toEqual(["f2"]);
      expect(sentFiles(2)).toEqual(["f2"]);
    });
  });

  describe("衝突（409）", () => {
    it("失敗ではなく状態として持ち、未保存の印は下ろさない", async () => {
      vi.spyOn(boardsApi, "saveScene").mockRejectedValueOnce(conflict);
      const { hook, markSaved, onSaved, onError } = setup();

      let written!: boolean;
      await act(async () => {
        written = await hook.result.current.save();
      });

      expect(written).toBe(false);
      expect(hook.result.current.conflicted).toBe(true);
      // 捨てて読み直すと、消えるのは相手ではなくこちらの作業になる。
      expect(markSaved).not.toHaveBeenCalled();
      expect(onSaved).not.toHaveBeenCalled();
      // 帯が言うので、通知には出さない。
      expect(onError).not.toHaveBeenCalled();
    });

    it("ボードを読み直して版が変わったら、衝突の知らせは消える", async () => {
      vi.spyOn(boardsApi, "saveScene").mockRejectedValueOnce(conflict);
      const { hook } = setup();
      await act(() => hook.result.current.save());
      expect(hook.result.current.conflicted).toBe(true);

      hook.rerender({ updatedAt: "v2" });

      expect(hook.result.current.conflicted).toBe(false);
    });
  });

  describe("失敗", () => {
    it.each([
      {
        name: "上限超え（413）は何度押しても同じなので、再試行を付けない",
        error: tooLarge,
        retry: false,
      },
      { name: "それ以外は、その場から押し直せる", error: boom, retry: true },
    ])("$name", async ({ error, retry }) => {
      vi.spyOn(boardsApi, "saveScene").mockRejectedValueOnce(error);
      const { hook, onError, markSaved } = setup();

      let written!: boolean;
      await act(async () => {
        written = await hook.result.current.save();
      });

      expect(written).toBe(false);
      expect(markSaved).not.toHaveBeenCalled();
      expect(onError).toHaveBeenCalledTimes(1);
      const [failure, options] = onError.mock.calls[0]!;
      expect(failure.message).toMatch(/^保存できませんでした/);
      // 失敗が続いても同じ 1 件を差し替えるだけにする。
      expect(options?.key).toBe(SAVE_FAILED);
      expect(options?.action?.label).toBe(retry ? "再試行" : undefined);
    });

    it("再試行は、その場で保存し直す", async () => {
      vi.spyOn(boardsApi, "saveScene")
        .mockRejectedValueOnce(boom)
        .mockResolvedValueOnce(saved("v2"));
      const { hook, onError, onSaved } = setup();
      await act(() => hook.result.current.save());

      await act(async () => {
        onError.mock.calls[0]![1]!.action!.run();
        await Promise.resolve();
      });

      expect(boardsApi.saveScene).toHaveBeenCalledTimes(2);
      await vi.waitFor(() => expect(onSaved).toHaveBeenCalledTimes(1));
    });
  });

  describe("書けたとき", () => {
    it("前の失敗の通知を下げ、上限超えの印を倒し、true を返す", async () => {
      vi.spyOn(boardsApi, "saveScene").mockResolvedValueOnce(saved("v2"));
      const { hook, dismiss, onSaved } = setup({ sceneOverLimit: true });
      expect(hook.result.current.overLimit).toBe(true);

      let written!: boolean;
      await act(async () => {
        written = await hook.result.current.save();
      });

      expect(written).toBe(true);
      expect(dismiss).toHaveBeenCalledWith(SAVE_FAILED);
      // 保存が成功した = いまのシーンはサーバーの上限を満たしている（ADR 0048）。
      expect(hook.result.current.overLimit).toBe(false);
      expect(onSaved).toHaveBeenCalledTimes(1);
    });

    it("基準にするのは送った内容の署名で、待つあいだの描き足しは含めない", async () => {
      const responding = deferred<SaveSceneResponse>();
      vi.spyOn(boardsApi, "saveScene").mockReturnValueOnce(responding.promise);
      const { hook, canvas, markSaved } = setup();
      const sent = sceneSignature(canvas.elements as SceneElement[], canvas.background);

      let saving!: Promise<boolean>;
      act(() => {
        saving = hook.result.current.save();
      });
      // 応答を待つあいだに描き足した。これは未保存のまま残らなければならない。
      canvas.elements = [...canvas.elements, image("img-3", "f1")];
      await act(async () => {
        responding.resolve(saved("v2"));
        await saving;
      });

      expect(markSaved).toHaveBeenCalledWith(sent);
      expect(markSaved).not.toHaveBeenCalledWith(
        sceneSignature(canvas.elements as SceneElement[], canvas.background),
      );
    });
  });

  describe("送らないとき", () => {
    it("排他を取れなければ送らず、false を返す", async () => {
      const saveScene = vi.spyOn(boardsApi, "saveScene");
      const { hook } = setup({ exclusive: exclusion(true) });

      let written!: boolean;
      await act(async () => {
        written = await hook.result.current.save();
      });

      expect(written).toBe(false);
      expect(saveScene).not.toHaveBeenCalled();
    });

    it("キャンバスがまだ組み上がっていなければ送らず、false を返す", async () => {
      const saveScene = vi.spyOn(boardsApi, "saveScene");
      const { hook } = setup({ withApi: false });

      let written!: boolean;
      await act(async () => {
        written = await hook.result.current.save();
      });

      expect(written).toBe(false);
      expect(saveScene).not.toHaveBeenCalled();
    });
  });
});
