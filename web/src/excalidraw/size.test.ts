import { serializeAsJSON } from "@excalidraw/excalidraw";
import type { ExcalidrawElement } from "@excalidraw/excalidraw/element/types";
import { describe, expect, it } from "vitest";

import { formatSceneSize, sceneBytes } from "./size";

describe("sceneBytes", () => {
  it("空文字は 0 バイト", () => {
    expect(sceneBytes("")).toBe(0);
  });

  // 文字数で数えると、日本語だけのボードで実際に送る量の 1/3 しか出ない。
  // サーバーが見るのもバイト数なので、そちらに揃っていることを固定する。
  it("文字数ではなく UTF-8 のバイト数で数える", () => {
    expect(sceneBytes("あ")).toBe(3);
    expect(sceneBytes("abc")).toBe(3);
  });

  /**
   * 画像を貼るとシーンが大きくなることを固定する。
   *
   * **これがこの表示の存在理由。** `serializeAsJSON` は `getFiles()` ごと
   * 直列化するので、貼った画像が base64 でシーンに乗る（ADR 0038）。ここが
   * 切れると、いちばん効く要因を数えていないことに気づけない。
   */
  it("貼った画像のぶんだけ増える", () => {
    const elements = [image()] as unknown as readonly ExcalidrawElement[];
    const dataURL = `data:image/png;base64,${"A".repeat(4096)}`;

    const withoutFile = serializeAsJSON(elements, {}, {}, "local");
    const withFile = serializeAsJSON(
      elements,
      {},
      {
        "file-1": {
          mimeType: "image/png",
          id: "file-1",
          dataURL,
          created: 1,
          lastRetrieved: 1,
        },
      } as never,
      "local",
    );

    expect(sceneBytes(withFile)).toBeGreaterThan(sceneBytes(withoutFile) + 4096);
  });
});

describe("formatSceneSize", () => {
  it.each([
    [0, "0 B"],
    [1023, "1023 B"],
    [1024, "1 KiB"],
    [1536, "1.5 KiB"],
    [1024 * 1024, "1 MiB"],
    [(8 << 20) + 1, "8 MiB"],
  ])("%i バイトは %s", (bytes, want) => {
    expect(formatSceneSize(bytes)).toBe(want);
  });
});

/** 画像要素 1 つ。ファイルを参照するところだけが要る。 */
function image() {
  return {
    id: "img-1",
    type: "image",
    x: 0,
    y: 0,
    width: 100,
    height: 100,
    angle: 0,
    strokeColor: "#1e1e1e",
    backgroundColor: "transparent",
    fillStyle: "solid",
    strokeWidth: 1,
    strokeStyle: "solid",
    roughness: 1,
    opacity: 100,
    groupIds: [],
    frameId: null,
    roundness: null,
    seed: 1,
    version: 1,
    versionNonce: 1,
    isDeleted: false,
    boundElements: null,
    updated: 1,
    link: null,
    locked: false,
    index: null,
    fileId: "file-1",
    status: "saved",
    scale: [1, 1],
  };
}
