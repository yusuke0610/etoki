import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { ExcalidrawImperativeAPI } from "@excalidraw/excalidraw/types";

import { sceneBytes } from "../excalidraw/size";
import { sceneJSON } from "../excalidraw/transfer";

/**
 * シーンの大きさを数え直すまでの待ち時間（ミリ秒）。
 *
 * 描くたびに数えると、画像を貼った大きいボードほど重くなる。手が止まってから
 * 数えれば、表示が少し遅れるだけで済む。
 */
const MEASURE_DELAY_MS = 500;

export type SceneSize = {
  /** 保存に送るシーンのバイト数。null は「まだ数えていない」。 */
  bytes: number | null;
  /** 変化を知らせる。**数えるのは手が止まってから 1 度だけ。** */
  schedule: () => void;
};

/**
 * いまのシーンの大きさを数える（#146 で `BoardPage` から切り出し）。
 *
 * 数えるには保存と同じ直列化が要る（画像は `getFiles()` ごと乗る）ので、
 * 描くたびに数えると、いちばん数えたい大きいボードでいちばん重くなる。
 * 変化が止まってから 1 度だけ数える。
 *
 * **上限は持たない。** 判定はサーバーだけが持つ（ADR 0018 / 0038）ので、
 * ここが出すのは「いまどれくらいか」という状態にとどめる。
 */
export function useSceneSize(api: ExcalidrawImperativeAPI | null): SceneSize {
  const [bytes, setBytes] = useState<number | null>(null);

  const measure = useCallback(() => {
    if (!api) return;
    setBytes(sceneBytes(sceneJSON(api)));
  }, [api]);

  const timer = useRef<number | null>(null);

  const schedule = useCallback(() => {
    if (timer.current !== null) window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => {
      timer.current = null;
      measure();
    }, MEASURE_DELAY_MS);
  }, [measure]);

  /*
    開いた直後に 1 度数える。押す前に見せるための表示なので、**何か描くまで
    空欄というのでは遅い。** デバウンスを通すと 0.5 秒遅れて出るようになり、
    ヘッダーのバッジが開いた直後に無い状態ができる（スクリーンショットで
    見つけた）。

    `set-state-in-effect` は「props や state から導ける値を effect で作るな」
    という規則で、ここはそれに当たらない。**数えるには Excalidraw から
    読み出すしかなく、読み出せるのはマウントしたあと。** 1 回ぶんの追加描画は
    避けられないもので、避ける形にすると表示が遅れる。
  */
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    measure();
  }, [measure]);

  useEffect(
    () => () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    },
    [],
  );

  // **同一性を保つ。** 呼ぶ側は `sceneSize.schedule` を useCallback の依存に
  // 置くので、毎レンダーで束を作り直すと、そこに乗っているコールバックまで
  // 作り直される（Excalidraw の `onChange` がそれで毎回差し替わる）。
  return useMemo(() => ({ bytes, schedule }), [bytes, schedule]);
}
