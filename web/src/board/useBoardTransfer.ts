import { useCallback, useMemo, useRef, type MutableRefObject } from "react";

import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";

import { sceneFileUnreadableFailure, type Failure } from "../api/errorMessage";
import type { SceneElement } from "../excalidraw/annotation";
import {
  exportFileName,
  readSceneFile,
  remapImportedAnnotationIds,
  remapImportedFileIds,
  sceneJSON,
  type ImportedScene,
} from "../excalidraw/transfer";
import { log } from "../logger";
import type { Exclusion } from "./exclusion";

type Options = {
  api: ExcalidrawImperativeAPI | null;
  /** 書き出すファイルの名前の元。 */
  boardName: string;
  exclusive: Exclusion;
  /** 未保存かどうか。**待ちを挟んで読むので ref 側から**（`useDirtyScene`）。 */
  isDirty: () => boolean;
  updateElements: (next: SceneElement[], appState?: Record<string, unknown>) => void;
  onError: (failure: Failure) => void;
};

export type BoardTransfer = {
  /** 取り込むファイルを選ばせる隠し `<input>`。押す口はボタン 1 つ。 */
  fileInput: MutableRefObject<HTMLInputElement | null>;
  exportScene: () => void;
  importScene: (file: File) => Promise<void>;
};

/**
 * ボードの持ち出しと取り込み（ADR 0045、#146 で `BoardPage` から切り出し）。
 *
 * **口は etoki のヘッダー 1 つに寄せる。** ライブラリのメニュー側は閉じてある
 * （`BoardPage` の `UI_OPTIONS`）。
 */
export function useBoardTransfer({
  api,
  boardName,
  exclusive,
  isDirty,
  updateElements,
  onError,
}: Options): BoardTransfer {
  /**
   * いまのキャンバスを `.excalidraw` として書き出す。
   *
   * **保存済みシーンではなくキャンバスから出す。** 保存済みから出すと、未保存の
   * 描き足しが黙って落ちる（中核思想 3）。**未保存でも押させる。** 入力は 1 つ
   * しかないので、解釈のように揃うまで待たせる理由が無い（ADR 0018 との違い）。
   *
   * viewer にも出す。見えているものを出すだけで、共有した相手にはすでに全部
   * 見えている（ADR 0017）。
   */
  const exportScene = useCallback(() => {
    if (!api) return;

    // 保存が送るのと同じバイト列。ヘッダーに出ている大きさが、そのまま
    // 書き出したファイルの大きさになる。
    const url = URL.createObjectURL(
      new Blob([sceneJSON(api)], { type: "application/json" }),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = exportFileName(boardName);
    // DOM に入っていない a のクリックを無視するブラウザがあるので、一度入れる。
    document.body.appendChild(link);
    link.click();
    link.remove();
    // 押した直後に外すと、ダウンロードが始まる前に URL が消えることがある。
    window.setTimeout(() => URL.revokeObjectURL(url), 0);
  }, [api, boardName]);

  const fileInput = useRef<HTMLInputElement | null>(null);

  /**
   * `.excalidraw` ファイルをキャンバスに取り込む。
   *
   * **サーバーには何も送らない。** 載せるだけで、確定させるのは人間の保存操作
   * だけ（中核思想 3）。取り込んだシーンの検証・版の照合・大きさの上限は、
   * その保存の経路のまま効く。
   *
   * **引いた解釈は捨てない。** これはキャンバスの編集であって保存ではない。
   * 捨てるのは保存したときだけで、それまでは未保存なので解釈は押せない
   * （ADR 0018）。
   */
  const importScene = useCallback(
    async (file: File) => {
      if (!api) return;

      // **排他は入口で取る。** `disabled` は表示の約束でしかないので、
      // 直接呼ばれても保存・作成と並走しないことはここで決める
      // （`.claude/rules/async-ui.md`）。取れなければ何もしない。
      await exclusive.run("importing", async () => {
        let imported: ImportedScene;
        try {
          // **読んでから訊く**（`App.open` と同じ形、ADR 0021）。訊いてから
          // 読むと、読んでいるあいだの描き足しを確認なしで捨てる。読めなかった
          // ときに捨ててよいかを訊いてしまうことも無くなる。
          imported = await readSceneFile(file, api);
        } catch (e) {
          // 読めなかった。**キャンバスには触らない。** 例外の中身は画面に
          // 出さず console に残す（`web/CLAUDE.md`）。
          log.error("シーンファイルを読み込めませんでした", e);
          onError(sceneFileUnreadableFailure());
          return;
        }

        // **ここから先に待ちは無い。** 挟むと、待っているあいだの描き足しを
        // 確認なしで捨てる。
        if (
          isDirty() &&
          !window.confirm(
            "取り込むと、いまキャンバスにある内容は置き換わります。未保存の変更は失われます。",
          )
        ) {
          return;
        }

        // 同じボードへ戻したファイルでも、以前の sync_runs / sync_items を
        // 引き継がないよう、注釈とそれを指す要素を一緒に新しい ID へ移す。
        imported = remapImportedAnnotationIds(imported);
        // addFiles は同じ ID のデータを上書きしない。別の画像がすでに同じ ID を
        // 使っていたら、取り込む画像とそれを指す要素を一緒に新しい ID へ移す。
        imported = remapImportedFileIds(imported, api.getFiles());
        updateElements(
          imported.elements,
          imported.viewBackgroundColor === undefined
            ? undefined
            : { viewBackgroundColor: imported.viewBackgroundColor },
        );
        // 貼ってあった画像。**入れ忘れると画像の要素だけが空白で置かれる。**
        api.addFiles(Object.values(imported.files) as never);

        // 取り込んだ絵が画面の外にあると、押しても何も起きていないように見える
        // （ADR 0040 で図のドラフトに対して決めたのと同じ形）。空のシーンには
        // 寄せる先が無い。
        if (imported.elements.length > 0) {
          api.scrollToContent(imported.elements as never, { fitToContent: true });
        }
      });
    },
    [api, exclusive, isDirty, onError, updateElements],
  );

  return useMemo(
    () => ({ fileInput, exportScene, importScene }),
    [exportScene, importScene],
  );
}
