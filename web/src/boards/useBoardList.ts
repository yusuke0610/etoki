import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { ApiError, boardsApi } from "../api/boards";
import { describeFailure, type Failure } from "../api/errorMessage";
import { createGenerations } from "../board/generation";
import type { NotifyOptions } from "../notification/types";
import type { BoardList } from "./BoardListPage";

/** ボード一覧の取得失敗の通知。続けて失敗しても 1 件に畳み、読めたら下げる。 */
const BOARD_LIST_FAILED = "board-list-failed";

/**
 * 一覧を読み込む要求の世代のキー（`.claude/rules/async-ui.md`）。
 *
 * 一覧へ戻るたびに読み直す（#200）ので、改名や削除の引き直しと並走しうる。
 * 古い応答が新しい一覧を上書きすると、件数が巻き戻る。
 */
const LISTING = "list";

type Options = {
  showFailure: (failure: Failure, options: Pick<NotifyOptions, "action" | "key">) => void;
  dismissKey: (key: string) => void;
  /**
   * ログインが要ると返った。ログイン状態を読み直す（`useSession`）。
   *
   * 使っている最中の失効はここで初めて分かる。エラーだけ出すと、画面は
   * ログイン済みのまま何も操作できず、リロードするまで戻れない。状態を読み直せば
   * ログイン画面に落ちる。
   */
  onLoginRequired: () => Promise<void>;
};

export type BoardListing = {
  /** ボードの一覧。**null はまだ読み込んでいない。** 0 件（空の配列）と分ける。 */
  boards: BoardList | null;
  reload: () => Promise<void>;
  /**
   * 消えたボードを手元から外す。
   *
   * **引き直しを待たずに外す。** 引き直しが失敗しても一覧は前の値のまま残るので、
   * 引き直しだけに任せると開くと 404 になる行が並び続ける。消えたことはサーバーの
   * 応答で確かめてあるので、推測ではない。
   */
  remove: (id: string) => void;
  /**
   * 一覧を捨て、走っている読み込みも無効にする（ログアウト）。
   *
   * React state を消しても進行中のリクエストは止まらない。遅れて着いた応答が、
   * 次にログインした人の画面に前の人の一覧を出す。
   */
  clear: () => void;
};

/** ボードの一覧（#230 で `App` から切り出し）。 */
export function useBoardList({
  showFailure,
  dismissKey,
  onLoginRequired,
}: Options): BoardListing {
  const [boards, setBoards] = useState<BoardList | null>(null);
  // 一覧を読み込む要求の世代（`LISTING`）。ボードを開く要求とは別に持つ。
  // ボードを開いたときに一覧の読み込みまで捨てる理由は無い。
  const [listings] = useState(createGenerations);

  // 通知の「再読み込み」から呼ぶ。通知は失敗した時点で作られるので、押された
  // 時点の reload を呼ぶよう ref を介す（reload 自身の中からは自分を指せない）。
  const reloadRef = useRef<() => Promise<void>>(async () => {});
  const reload = useCallback(async () => {
    const generation = listings.start(LISTING);
    try {
      const entries = await boardsApi.list();
      // 追い越されていたら捨てる。あとから始めた読み込みのほうが新しい。
      if (!listings.isCurrent(LISTING, generation)) return;
      setBoards({ entries, fetchedAt: new Date() });
      // 前に読めなかったことの通知は、もう当てはまらない。
      dismissKey(BOARD_LIST_FAILED);
    } catch (e) {
      // 追い越された読み込みの失敗は出さない。新しいほうが答えを持っている。
      if (!listings.isCurrent(LISTING, generation)) return;
      if (e instanceof ApiError && e.code === "login_required") {
        // 読み直しの失敗は `useSession` が自分で倒して知らせる。ここで投げると、
        // 呼び出し側は void reload() なので誰も受けず、画面はログイン済みの
        // ままボード一覧だけが空という、戻れない状態で止まる。
        await onLoginRequired();
        return;
      }
      // その場から読み直せるようにする。一覧が空のまま残ると、リロード以外に
      // 戻る手が画面に無い。
      showFailure(describeFailure("ボード一覧を取得できませんでした", e), {
        key: BOARD_LIST_FAILED,
        action: { label: "再読み込み", run: () => void reloadRef.current() },
      });
    }
  }, [dismissKey, listings, onLoginRequired, showFailure]);
  useEffect(() => {
    reloadRef.current = reload;
  }, [reload]);

  const remove = useCallback((id: string) => {
    setBoards(
      (listed) =>
        listed && { ...listed, entries: listed.entries.filter((b) => b.id !== id) },
    );
  }, []);

  const clear = useCallback(() => {
    setBoards(null);
    listings.invalidateAll();
  }, [listings]);

  return useMemo(
    () => ({ boards, reload, remove, clear }),
    [boards, reload, remove, clear],
  );
}
