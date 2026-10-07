import { useCallback, useEffect, useMemo, useState } from "react";

import { authApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import type { SessionStatus } from "../api/types";
import type { NotifyOptions } from "../notification/types";

/**
 * ログイン状態の取得失敗の通知。
 *
 * 取りにいくのは起動時の effect で、開発時の StrictMode では 2 回走る。key で
 * 畳まないと、同じ失敗が 2 件並ぶ（1 本の state だった頃は上書きで隠れていた）。
 */
const SESSION_FAILED = "session-failed";

export type Session = {
  /** ログイン状態。null は問い合わせ中。 */
  session: SessionStatus | null;
  /**
   * ログインが済んでいるか、要らない構成か。
   *
   * ログインが要る構成では、済むまで読みにいかない。先に叩くと 401 が
   * エラー表示に出て、ログイン画面の上に無関係な失敗が重なる。
   */
  signedIn: boolean;
  /** ログイン状態を読み直す。読めなければ、起動時と同じ側に倒して知らせる。 */
  reread: () => Promise<void>;
  /** サーバーから読んだ状態で置き換える（ログアウトの後）。 */
  replace: (session: SessionStatus) => void;
};

/** ログイン状態（#230 で `App` から切り出し）。 */
export function useSession(
  showFailure: (failure: Failure, options: Pick<NotifyOptions, "key">) => void,
): Session {
  const [session, setSession] = useState<SessionStatus | null>(null);

  const reread = useCallback(async () => {
    try {
      setSession(await authApi.session());
    } catch (e) {
      // 状態が分からないなら、ログインを求めない側に倒す。求める側に倒すと、
      // 認証を設定していない構成が API の一時的な失敗で使えなくなる。
      showFailure(describeFailure("ログイン状態を取得できませんでした", e), {
        key: SESSION_FAILED,
      });
      setSession({ authRequired: false, authenticated: false });
    }
  }, [showFailure]);

  useEffect(() => {
    // 読みにいくのは await の後で state を置く非同期関数なので描画の連鎖は
    // 起きないが、規則が見ているのは effect から setState を含む関数を呼ぶこと
    // 自体なので、ここは外す（`App` の一覧の読み込みと同じ）。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void reread();
  }, [reread]);

  const signedIn = session !== null && (!session.authRequired || session.authenticated);

  return useMemo(
    () => ({ session, signedIn, reread, replace: setSession }),
    [session, signedIn, reread],
  );
}
