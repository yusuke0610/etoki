import { describe, expect, it } from "vitest";

import { exportFileName, readSceneFile, type LoadFromBlob } from "./transfer";
import type { SceneSource } from "./image";

/**
 * 書き出しのファイル名と、取り込んだものの詰め替え（ADR 0042）。
 *
 * **ライブラリの実物は掛けられない。** `loadFromBlob` は Blob の読み取りと
 * Excalidraw の復元が要り jsdom では動かない。ここで確かめるのは、返ってきた
 * ものをどう詰め替えるかまで（`image.ts` の `exportBlob` と同じ形）。実物を
 * 通した取り込みは E2E（`web/e2e/transfer.spec.ts`）が見る。
 */

describe("exportFileName", () => {
  it("ボード名をそのまま使う", () => {
    expect(exportFileName("認証まわりのブレスト")).toBe(
      "認証まわりのブレスト.excalidraw",
    );
  });

  // 区切り文字が残ると、ファイル名がパスとして読まれる。
  it("パスの区切りを置き換える", () => {
    expect(exportFileName("acme/web の設計")).toBe("acme_web の設計.excalidraw");
    expect(exportFileName("a\\b")).toBe("a_b.excalidraw");
  });

  it("OS が嫌う文字と制御文字を置き換える", () => {
    expect(exportFileName('a<b>c:d"e|f?g*h')).toBe("a_b_c_d_e_f_g_h.excalidraw");
    expect(exportFileName("a\u0000b\u001fc\u007fd")).toBe("a_b_c_d.excalidraw");
  });

  // 空文字にすると拡張子だけの隠しファイルになる。
  it("名前が残らないときは代わりの名前にする", () => {
    expect(exportFileName("")).toBe("board.excalidraw");
    expect(exportFileName("   ")).toBe("board.excalidraw");
    expect(exportFileName("..")).toBe("board.excalidraw");
    // **置き換えた結果が残るなら、そちらを使う。** 代わりの名前に倒すのは
    // 名前が 1 文字も残らなかったときだけ。
    expect(exportFileName("\u0001")).toBe("_.excalidraw");
  });

  it("末尾の空白とドットを落とす", () => {
    expect(exportFileName("設計メモ...")).toBe("設計メモ.excalidraw");
    expect(exportFileName("  設計メモ  ")).toBe("設計メモ.excalidraw");
  });

  it("長い名前を切り詰める", () => {
    const name = exportFileName("あ".repeat(200));
    expect(name).toBe(`${"あ".repeat(80)}.excalidraw`);
  });
});

/** Excalidraw の代わり。取り込みが今の画面から読むものだけを持つ。 */
function sceneSource(viewBackgroundColor = "#ffffff"): SceneSource {
  return {
    getSceneElements: () => [{ id: "existing", type: "rectangle" }],
    getAppState: () => ({ viewBackgroundColor }),
    getFiles: () => ({}),
  };
}

describe("readSceneFile", () => {
  const blob = new Blob(["{}"]);

  it("要素と画像と背景色を取り出す", async () => {
    const load: LoadFromBlob = async () => ({
      elements: [{ id: "imported", type: "frame" }],
      appState: { viewBackgroundColor: "#f5faff" },
      files: { "file-1": { id: "file-1" }, "file-2": { id: "file-2" } },
    });

    const imported = await readSceneFile(blob, sceneSource(), load);

    expect(imported.elements).toEqual([{ id: "imported", type: "frame" }]);
    // 表ではなく配列。`addFiles` が配列でしか受け取らない。
    expect(imported.files).toEqual([{ id: "file-1" }, { id: "file-2" }]);
    expect(imported.viewBackgroundColor).toBe("#f5faff");
  });

  // 画像を貼っていないファイルでも `addFiles` に渡せる形で返す。
  it("画像が無ければ空の配列にする", async () => {
    const load: LoadFromBlob = async () => ({
      elements: [],
      appState: { viewBackgroundColor: "#ffffff" },
    });

    expect((await readSceneFile(blob, sceneSource(), load)).files).toEqual([]);
  });

  // 既定色をここで決めると、Excalidraw が既定を変えた日に取り込みだけ色が変わる。
  it("ファイルに背景色が無ければ今の色を保つ", async () => {
    const load: LoadFromBlob = async () => ({
      elements: [],
      appState: { viewBackgroundColor: null },
    });

    expect(await readSceneFile(blob, sceneSource("#123456"), load)).toMatchObject({
      viewBackgroundColor: "#123456",
    });
  });

  // 今の画面の状態を渡さないと、復元が既定値で埋めてしまう。
  it("今の画面の状態と要素をライブラリに渡す", async () => {
    let received: unknown[] = [];
    const load: LoadFromBlob = async (_blob, appState, elements) => {
      received = [appState, elements];
      return { elements: [], appState: {} };
    };

    await readSceneFile(blob, sceneSource("#abcdef"), load);

    expect(received).toEqual([
      { viewBackgroundColor: "#abcdef" },
      [{ id: "existing", type: "rectangle" }],
    ]);
  });

  // 読めないファイルはここで投げる。呼び出し側がキャンバスに触らない判断を
  // できるよう、握りつぶさない。
  it("ライブラリが投げたらそのまま投げる", async () => {
    const load: LoadFromBlob = () => Promise.reject(new Error("Invalid file"));

    await expect(readSceneFile(blob, sceneSource(), load)).rejects.toThrow(
      "Invalid file",
    );
  });
});
