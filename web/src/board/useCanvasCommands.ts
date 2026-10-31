import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";
import { useCallback, useMemo } from "react";

import { createTable, tableCenter } from "../excalidraw/table";
import type { CanvasAnnotations } from "./useCanvasAnnotations";

type Options = {
  api: ExcalidrawImperativeAPI | null;
  currentElements: CanvasAnnotations["currentElements"];
  updateElements: CanvasAnnotations["updateElements"];
  /** スマホで全面に開いたパネルを閉じる（#199）。 */
  closePhonePanel: () => void;
};

export type CanvasCommands = {
  addTable: () => void;
  focusFrame: (frameId: string) => void;
};

/**
 * キャンバスに対して etoki のメニューやパネルから行う操作。
 *
 * 返す束は `useMemo` で同一性を保つ。`addTable` はキャンバスのメニュー
 * （`useMemo` で持つ、ADR 0065）の依存に入る。
 */
export function useCanvasCommands({
  api,
  currentElements,
  updateElements,
  closePhonePanel,
}: Options): CanvasCommands {
  /**
   * 表を 1 つ置く（ADR 0069）。
   *
   * **ダイアログを挟まない。** 描いている最中の操作なので、手数の少なさが
   * そのまま値打ちになる。大きさは固定で、足りなければ複製やセルの追加で
   * 広げる。置くだけで保存はしない。確定させるのは人間の保存操作だけ。
   *
   * 置いた表は group ごと選んでおく。そのまま動かしたり、消したりできる。
   */
  const addTable = useCallback(() => {
    if (!api) return;

    const { scrollX, scrollY, zoom, width, height } = api.getAppState();
    const table = createTable(
      tableCenter({ scrollX, scrollY, zoom: zoom.value, width, height }),
    );
    updateElements([...currentElements(), ...table]);

    const groupId = table[0]?.groupIds?.[0];
    if (groupId !== undefined) {
      // 選択を渡す `updateScene` はツールを切り替えない。矩形ツールのまま置くと、
      // 次のポインター操作で選択が外れて新しい矩形を描いてしまい、置いた直後に
      // 表を動かせない。
      api.setActiveTool({ type: "selection" });
      api.updateScene({
        appState: {
          selectedElementIds: Object.fromEntries(table.map((el) => [el.id, true])),
          selectedGroupIds: { [groupId]: true },
        },
      } as never);
    }
  }, [api, currentElements, updateElements]);

  /**
   * キャンバスをそのフレームへ寄せて選択する。
   *
   * パネルの項目とキャンバスのフレームを結ぶ唯一の手段（ADR 0022）。
   * `sceneSignature` が appState から見るのは背景色だけで、選択とスクロールは
   * 見ないので、これで未保存にはならない。
   */
  const focusFrame = useCallback(
    (frameId: string) => {
      if (!api) return;
      const frame = currentElements().find((el) => el.id === frameId);
      if (!frame) return;

      // スマホでは全面のパネルがキャンバスを隠しているので閉じる（#199）。
      // 詳細を閉じてから寄せる（`AnnotationDetail`）のと同じ理由。
      closePhonePanel();
      api.updateScene({ appState: { selectedElementIds: { [frameId]: true } } } as never);
      api.scrollToContent(frame as never, { fitToContent: true, animate: true });
    },
    [api, currentElements, closePhonePanel],
  );

  return useMemo(() => ({ addTable, focusFrame }), [addTable, focusFrame]);
}
