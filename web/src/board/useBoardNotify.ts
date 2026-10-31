import { useCallback, useEffect, useMemo, useRef } from "react";

import type { Failure } from "../api/errorMessage";
import { useNotify } from "../notification/NotificationProvider";
import type { NotifyOptions } from "../notification/types";

/** ボードの画面から失敗を通知に出す口。 */
export type BoardErrorHandler = (
  failure: Failure,
  options?: Pick<NotifyOptions, "action" | "key">,
) => void;

export type BoardNotify = {
  /** 失敗を通知に出す。画面から外れたあとに呼ばれたら何もしない。 */
  onError: BoardErrorHandler;
  /** key の通知を下げる。画面から外れたあとに呼ばれたら何もしない。 */
  dismissHere: (key: string) => void;
  /**
   * key の通知を下げる。**印を見ない。** ボードを離れる時点の片付けに使う
   * （`BoardPage` の `board.id` の cleanup）。
   */
  dismissKey: (key: string) => void;
};

/**
 * ボードの画面が通知を出し入れする口（ADR 0058）。
 *
 * 画面全体に出す失敗は通知へ。**保存の衝突・解釈や作成の失敗はここを通さない。**
 * 消えてよい失敗ではなく、残して読ませる状態だから。
 *
 * 返す束は `useMemo` で同一性を保つ。中の関数を依存に置く `useCallback` が
 * 作り直されないようにするため（web/CLAUDE.md の「関心をフックに切り出すとき」）。
 */
export function useBoardNotify(): BoardNotify {
  const { notify, dismissKey } = useNotify();
  // **画面から外れたあとに返った失敗は通知しない。** 通知はキャンバスより上に
  // 生きているので、ボードを離れる前に投げた保存が離れたあとで失敗すると、
  // 別のボードの画面に「保存できませんでした」が出る。その「再試行」が呼ぶのは
  // 離れたボードの save で、捨てると決めたシーンを前のボードへ保存しにいく。
  // 離れる時点で出ている通知は下げてある（SAVE_FAILED の effect）ので、ここで
  // 塞ぐのは離れる時点でまだ応答待ちだったぶん。
  //
  // 印は effect で立てる。StrictMode は effect を 2 回走らせ、そのあいだに
  // cleanup を挟むので、初期値で true にすると 2 回目以降が false のまま残る。
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  // **下げる側も同じ印を見る。** 通知はボードより上（`NotificationProvider`）に
  // 生きていて key で消すので、**離れたあとに届いた成功で下げると、いま開いて
  // いるボードに出ている同じ key の通知が消える。** 出す側（`onError`）だけを
  // 塞いでも、消える側からボードを跨いでしまう。
  //
  // **離れる時点で下げるのは別の話**なので、そちらは `dismissKey` を直に呼ぶ
  // （`BoardPage` の `board.id` の cleanup）。あれは「離れたから下げる」であって、
  // 「離れたのに下げる」ではない。
  const dismissHere = useCallback(
    (key: string) => {
      if (!mounted.current) return;
      dismissKey(key);
    },
    [dismissKey],
  );

  const onError = useCallback<BoardErrorHandler>(
    (failure, options = {}) => {
      if (!mounted.current) return;
      notify({
        kind: "error",
        message: failure.message,
        detail: failure.detail,
        ...options,
      });
    },
    [notify],
  );

  return useMemo(
    () => ({ onError, dismissHere, dismissKey }),
    [onError, dismissHere, dismissKey],
  );
}
