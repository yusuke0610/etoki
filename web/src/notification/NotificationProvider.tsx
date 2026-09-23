import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useReducer,
  useRef,
  type ReactNode,
} from "react";

import { notificationsReducer } from "./reducer";
import type { Notification, NotifyOptions } from "./types";

/** 通知を出す側が使う口。参照は Provider の生存中ずっと変わらない。 */
export type NotifyApi = {
  /** 通知を出し、その id を返す。 */
  notify: (options: NotifyOptions) => number;
  dismiss: (id: number) => void;
  /** その key の通知を消す。直ったことが分かったときに、古い失敗を下げる。 */
  dismissKey: (key: string) => void;
};

/*
 * 出す口と、並びそのものを別の Context に分ける。**出す側は並びの変化で
 * 再描画しない。** 1 つにまとめると、通知が 1 件出るたびに useNotify() を
 * 呼んでいる画面（キャンバスを抱える BoardPage を含む）が描き直される。
 */
const NotifyContext = createContext<NotifyApi | null>(null);
const NotificationsContext = createContext<Notification[] | null>(null);

/**
 * 通知の状態を持つ。アプリに 1 つだけ置く。
 *
 * **状態管理の基盤を足さない。** etoki の状態はコンポーネントの `useState` と
 * props で降ろす形で、グローバルな store は無い。通知だけは深い位置
 * （BoardPage の保存・パネル）から出すので、React 標準の Context を 1 つ
 * 置き、持たせるのは通知の並びと出し入れの口だけにする（ADR 0058）。
 */
export function NotificationProvider({ children }: { children: ReactNode }) {
  const [items, dispatch] = useReducer(notificationsReducer, []);
  // id はここで振る。reducer を純関数のままにするため。
  const nextId = useRef(1);

  const notify = useCallback((options: NotifyOptions) => {
    const id = nextId.current++;
    dispatch({ type: "add", id, options });
    return id;
  }, []);
  const dismiss = useCallback((id: number) => dispatch({ type: "dismiss", id }), []);
  const dismissKey = useCallback(
    (key: string) => dispatch({ type: "dismissKey", key }),
    [],
  );

  const api = useMemo(
    () => ({ notify, dismiss, dismissKey }),
    [notify, dismiss, dismissKey],
  );

  return (
    <NotifyContext.Provider value={api}>
      <NotificationsContext.Provider value={items}>
        {children}
      </NotificationsContext.Provider>
    </NotifyContext.Provider>
  );
}

/**
 * 通知を出す口。
 *
 * **Provider の外で呼んだら投げる。** 黙って何もしない既定値を置くと、配線を
 * 忘れた画面の失敗がどこにも出ないまま消える。
 */
export function useNotify(): NotifyApi {
  const api = useContext(NotifyContext);
  if (api === null) {
    throw new Error("useNotify は NotificationProvider の内側で呼ぶ");
  }
  return api;
}

/** 画面に出ている通知の並び。描画する `Notifications` だけが使う。 */
export function useNotificationList(): Notification[] {
  const items = useContext(NotificationsContext);
  if (items === null) {
    throw new Error("useNotificationList は NotificationProvider の内側で呼ぶ");
  }
  return items;
}
