import { describe, expect, it } from "vitest";

import type { SceneElement } from "./annotation";
import {
  exportAnnotationImage,
  findAnnotationFrame,
  MAX_IMAGE_DIMENSION,
  toAnnotationImage,
  type ExportToBlob,
} from "./image";

const frame = (id: string, extra: Partial<SceneElement> = {}): SceneElement => ({
  id,
  type: "frame",
  customData: { etoki: { granularity: "" } },
  ...extra,
});

describe("findAnnotationFrame", () => {
  it("ID の一致する注釈の frame を返す", () => {
    const elements = [frame("a1"), frame("a2"), { id: "t1", type: "text" }];

    expect(findAnnotationFrame(elements, "a2")?.id).toBe("a2");
  });

  it("注釈でない frame は返さない", () => {
    // ブレスト中にユーザーが自分の用途で使った frame。
    const elements = [{ id: "f1", type: "frame" }];

    expect(findAnnotationFrame(elements, "f1")).toBeUndefined();
  });

  it("削除済みの frame は返さない", () => {
    const elements = [frame("a1", { isDeleted: true })];

    expect(findAnnotationFrame(elements, "a1")).toBeUndefined();
  });
});

describe("toAnnotationImage", () => {
  it("バイト列を base64 にして PNG として詰める", () => {
    const image = toAnnotationImage(new Uint8Array([0x89, 0x50, 0x4e, 0x47]));

    expect(image.mediaType).toBe("image/png");
    expect(image.data).toBe("iVBORw==");
  });

  it("btoa の引数の上限を超える長さでも詰められる", () => {
    const bytes = new Uint8Array(0x8000 * 2 + 5).fill(0x41);

    const image = toAnnotationImage(bytes);

    expect(atob(image.data)).toHaveLength(bytes.length);
  });
});

describe("exportAnnotationImage", () => {
  /** exportToBlob の代わり。呼ばれた引数を残す。 */
  function recordingExport() {
    const calls: Parameters<ExportToBlob>[0][] = [];
    const exportBlob: ExportToBlob = async (opts) => {
      calls.push(opts);
      return new Blob([new Uint8Array([1, 2, 3])]);
    };
    return { calls, exportBlob };
  }

  const api = (elements: readonly SceneElement[]) => ({
    getSceneElements: () => elements,
    getAppState: () => ({}),
    getFiles: () => ({}),
  });

  it("注釈の frame を exportingFrame に渡す", async () => {
    // frame を渡さないとボード全体が写り、囲みの外の話題まで LLM に渡る。
    const elements = [frame("a1"), { id: "t1", type: "text" }];
    const { calls, exportBlob } = recordingExport();

    const image = await exportAnnotationImage(api(elements), "a1", exportBlob);

    expect(calls).toHaveLength(1);
    const [opts] = calls;
    expect((opts?.exportingFrame as unknown as SceneElement | undefined)?.id).toBe("a1");
    expect(opts?.maxWidthOrHeight).toBe(MAX_IMAGE_DIMENSION);
    expect(image?.mediaType).toBe("image/png");
  });

  it("注釈が見つからなければ書き出さない", async () => {
    const { calls, exportBlob } = recordingExport();

    const image = await exportAnnotationImage(api([]), "a1", exportBlob);

    // 画像なしでも解釈は成立する。ここで解釈そのものを止めない。
    expect(image).toBeUndefined();
    expect(calls).toHaveLength(0);
  });
});
