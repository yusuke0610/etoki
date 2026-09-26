import type { Notification, NotificationKind, NotifyOptions } from "./types";

/** 同時に出す通知の上限。超えたら古いものから落とす。 */
export const MAX_NOTIFICATIONS = 3;

/**
 * 種別ごとの既定の表示時間。`null` は自動で消さない。
 *
 * **`error` と `warning` は消さない。** etoki で警告に当たるのは「未保存」
 * 「取り残しが出る」のような状態が多く、消えると読めなくなる。
 */
const DEFAULT_DURATION: Record<NotificationKind, number | null> = {
  error: null,
  warning: null,
  success: 6000,
  info: 6000,
};

export type NotificationsEvent =
  | { type: "add"; id: number; options: NotifyOptions }
  | { type: "dismiss"; id: number }
  | { type: "dismissKey"; key: string };

/**
 * 通知の並びを更新する純関数。新しいものが先頭。
 *
 * 上限・置き換え・順序は React 抜きで決まるので、ここに閉じてテストで固定する
 * （`grouping.ts` などと同じく、判定は純関数に置く）。
 */
export function notificationsReducer(
  state: Notification[],
  event: NotificationsEvent,
): Notification[] {
  switch (event.type) {
    case "add": {
      const next = toNotification(event.id, event.options);
      // 同じ key は置き換える。先頭に出し直すのは、新しく起きたことを
      // 読み上げに届けるため（位置だけ据え置くと、同じ文の繰り返しに見える）。
      const rest =
        next.key === undefined ? state : state.filter((n) => n.key !== next.key);
      return [next, ...rest].slice(0, MAX_NOTIFICATIONS);
    }
    case "dismiss":
      return state.filter((n) => n.id !== event.id);
    case "dismissKey":
      return state.filter((n) => n.key !== event.key);
  }
}

function toNotification(id: number, options: NotifyOptions): Notification {
  const duration =
    options.action !== undefined
      ? null
      : options.duration !== undefined
        ? options.duration
        : DEFAULT_DURATION[options.kind];

  return {
    id,
    kind: options.kind,
    message: options.message,
    detail: options.detail ?? "",
    action: options.action,
    duration,
    key: options.key,
  };
}
