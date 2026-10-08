import { describe, expect, it } from "vitest";

import { filesToSave, openedFiles, referencedFileIds } from "./files";
import type { SceneSource } from "./image";

/** 画像データ。中身で見分けられるように dataURL に印を入れる。 */
function file(id: string, mark: string) {
  return {
    id,
    mimeType: "image/png",
    dataURL: `data:image/png;base64,${mark}`,
    created: 1,
  };
}

/** キャンバスの代わり。要素と画像の実体だけを返す。 */
function canvas(elements: unknown[], files: Record<string, unknown>): SceneSource {
  return {
    getSceneElements: () => elements,
    getAppState: () => ({}),
    getFiles: () => files,
  };
}

describe("referencedFileIds", () => {
  // 同じ画像を貼った要素が 2 つあっても 1 回だけ。重ねて返すと、同じ画像を
  // 2 回送ることになる。
  it("重複を除き、要素の順に返す", () => {
    expect(
      referencedFileIds([{ fileId: "f-2" }, { fileId: "f-1" }, { fileId: "f-2" }]),
    ).toEqual(["f-2", "f-1"]);
  });
});

describe("filesToSave", () => {
  // **送るのはサーバーがまだ持っていない画像だけ**（ADR 0074）。持っている画像を
  // 送り直すと、図形を動かしただけの保存で画像を丸ごと送ることになる。
  it("サーバーが持っている画像は送らない", () => {
    const api = canvas(
      [
        { id: "a", type: "image", fileId: "held" },
        { id: "b", type: "image", fileId: "new" },
      ],
      { held: file("held", "HELD"), new: file("new", "NEW") },
    );

    const sent = filesToSave(api, new Set(["held"]));

    expect(Object.keys(sent)).toEqual(["new"]);
    expect(JSON.parse(sent.new ?? "")).toEqual(file("new", "NEW"));
  });

  // Excalidraw は消した要素の画像もキャンバスに持ち続ける（元に戻すため）。
  // 送るとサーバーが「参照されていない」で 400 を返す。
  it("参照していない画像は、キャンバスにあっても送らない", () => {
    const api = canvas([{ id: "a", type: "image", fileId: "gone", isDeleted: true }], {
      gone: file("gone", "GONE"),
      loose: file("loose", "LOOSE"),
    });

    expect(filesToSave(api, new Set())).toEqual({});
  });

  // 貼っている途中の要素は実体が遅れて届く。送れないものは送らず、`held` に
  // 入らないまま次の保存へ持ち越す。
  it("キャンバスに実体の無い画像は送らない", () => {
    const api = canvas([{ id: "a", type: "image", fileId: "pending" }], {});

    expect(filesToSave(api, new Set())).toEqual({});
  });
});

describe("openedFiles", () => {
  it("開く口が返した JSON を、キャンバスに渡せる画像にする", () => {
    expect(openedFiles({ f: JSON.stringify(file("f", "F")) })).toEqual({
      f: file("f", "F"),
    });
  });

  // 読めない画像は落とす。Excalidraw が空白の画像として描くので、見えなくは
  // ならない。サーバーは持ったままなので、保存しても失われない。
  it("読めない画像は落とし、読めるものは残す", () => {
    expect(
      openedFiles({
        broken: "{",
        text: '"not an object"',
        list: "[]",
        ok: JSON.stringify(file("ok", "OK")),
      }),
    ).toEqual({ ok: file("ok", "OK") });
  });
});
