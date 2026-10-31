import { useEffect, useMemo } from "react";

import { sceneUnreadableFailure } from "../api/errorMessage";
import type { BoardWithFiles } from "../api/types";
import { openedFiles } from "../excalidraw/files";
import type { BoardErrorHandler } from "./useBoardNotify";

/**
 * 開いたときに effect から出る失敗の通知。
 *
 * **effect から出すものには key を付ける。** 開発時の StrictMode では effect が
 * 2 回走るので、畳まないと同じ失敗が 2 件並ぶ。注釈の状態のぶん
 * （`ANNOTATIONS_FAILED`）は `useAnnotationStates` にある。
 */
export const SCENE_UNREADABLE = "scene-unreadable";

export type InitialScene = {
  /** キャンバスに渡す最初のシーン。渡すのはマウントの 1 回きり。 */
  initialData: unknown;
  /** 開いた時点でサーバーが持っていた画像。保存はこれに無い画像だけを送る。 */
  openedFileIds: string[];
};

/**
 * 開いたボードから、キャンバスに渡す最初のシーンを組み立てる。
 *
 * 読めなかったら空で開き、そのことを通知する。
 */
export function useInitialScene(
  board: Pick<BoardWithFiles, "scene" | "files">,
  onError: BoardErrorHandler,
): InitialScene {
  const initialScene = useMemo(() => {
    try {
      const scene = JSON.parse(board.scene) as { elements?: unknown; appState?: unknown };
      // **開いたら中身が見えている位置から始める。** シーンの原点はキャンバスの
      // 左上に来るので、原点あたりに描かれたものはツールバーの下に隠れる。
      // ひな形から作ったボードは必ずそこから始まる（`excalidraw/template.ts`）
      // ので、見出しが隠れた状態が最初の 1 画面になる。
      //
      // **要素は動かさない。** 動かすと座標の変更として未保存になり、開いた
      // だけで保存を促すことになる。動かすのは見ている位置のほう。
      //
      // 貼った画像はシーンとは別に届く（ADR 0074）。渡さないと、画像の要素だけが
      // 空白で置かれる。
      return {
        data: { ...scene, files: openedFiles(board.files), scrollToContent: true },
        unreadable: false,
      };
    } catch {
      // 保存時に検証しているのでここには来ないはずだが、来たら空で開く。
      return { data: { elements: [], appState: {} }, unreadable: true };
    }
  }, [board.scene, board.files]);

  const openedFileIds = useMemo(() => Object.keys(board.files), [board.files]);

  // 読めなかったことの通知は描画の外で出す。useMemo の中で出すと、描画中に
  // 別のコンポーネント（通知）の状態を書き換えることになる。
  useEffect(() => {
    if (initialScene.unreadable)
      onError(sceneUnreadableFailure(), { key: SCENE_UNREADABLE });
  }, [initialScene, onError]);

  const initialData = initialScene.data;
  return useMemo(() => ({ initialData, openedFileIds }), [initialData, openedFileIds]);
}
