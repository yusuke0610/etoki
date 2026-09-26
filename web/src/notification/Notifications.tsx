import { useEffect, useRef, useState, type FocusEvent } from "react";

import { useNotificationList, useNotify } from "./NotificationProvider";
import type { Notification, NotificationKind } from "./types";

/** 色だけで種別を伝えない。見出しとして文字でも出す。 */
const KIND_LABEL: Record<NotificationKind, string> = {
  error: "エラー",
  warning: "注意",
  success: "完了",
  info: "お知らせ",
};

/**
 * 通知を描く。アプリに 1 つだけ置く（ADR 0058）。
 *
 * **キャンバスのレイアウトを動かさない。** 帯としてフローに入れると、出た
 * 瞬間にキャンバスの高さが削られて Excalidraw が描き直され、ブレストの最中に
 * 描画位置が動く。`position: fixed` でフローの外に出す（`index.css`）。
 *
 * **入れ物は 0 件のときも DOM に置いたままにする。** 通知と一緒に入れ物ごと
 * 差し込むと、読み上げソフトが変化として拾わないことがある。
 */
export function Notifications() {
  const items = useNotificationList();

  return (
    <div className="notifications" aria-live="polite">
      {items.map((n) => (
        <NotificationItem key={n.id} notification={n} />
      ))}
    </div>
  );
}

function NotificationItem({ notification: n }: { notification: Notification }) {
  const { dismiss } = useNotify();
  const paused = usePausable(n.duration, () => dismiss(n.id));

  // error / warning は割り込んで読ませ、それ以外は区切りのよいところで読ませる。
  // **フォーカスは奪わない。** 出た瞬間にフォーカスが飛ぶと、描いている手が止まる。
  const assertive = n.kind === "error" || n.kind === "warning";

  return (
    <div
      className={`notification notification-${n.kind}`}
      role={assertive ? "alert" : "status"}
      {...paused}
    >
      <div className="error-body">
        <p className="notification-message">
          <span className="notification-kind">{KIND_LABEL[n.kind]}</span>
          {n.message}
        </p>
        {n.detail !== "" && (
          <details className="error-detail">
            <summary>詳細</summary>
            <pre>{n.detail}</pre>
          </details>
        )}
      </div>
      <div className="notification-actions">
        {n.action && (
          <button
            type="button"
            onClick={() => {
              n.action?.run();
              dismiss(n.id);
            }}
          >
            {n.action.label}
          </button>
        )}
        {/* 通知は最大 3 件並ぶ。どれを閉じるのかを読み上げで区別できるよう、
            名前に本文を含める（見える文言も名前に残す）。 */}
        <button
          type="button"
          aria-label={`「${n.message}」を閉じる`}
          onClick={() => dismiss(n.id)}
        >
          閉じる
        </button>
      </div>
    </div>
  );
}

/**
 * `duration` ミリ秒で `onExpire` を呼ぶ。ホバー中とフォーカスが中にある
 * あいだは止め、離れたら残りから再開する。`null` なら何もしない。
 *
 * 読んでいる途中や、ボタンに手を伸ばしている途中で消さないため。
 */
function usePausable(duration: number | null, onExpire: () => void) {
  const [hovered, setHovered] = useState(false);
  const [focused, setFocused] = useState(false);
  const remaining = useRef(duration);
  const expire = useRef(onExpire);
  useEffect(() => {
    expire.current = onExpire;
  }, [onExpire]);

  const paused = hovered || focused;

  useEffect(() => {
    if (paused || remaining.current === null) return;

    const startedAt = Date.now();
    const timer = setTimeout(() => expire.current(), remaining.current);
    return () => {
      clearTimeout(timer);
      if (remaining.current !== null) {
        remaining.current = Math.max(0, remaining.current - (Date.now() - startedAt));
      }
    };
  }, [paused]);

  return {
    onMouseEnter: () => setHovered(true),
    onMouseLeave: () => setHovered(false),
    onFocus: () => setFocused(true),
    onBlur: (e: FocusEvent<HTMLDivElement>) => {
      // 中のボタンどうしでフォーカスが移るあいだは止めたまま。
      if (!e.currentTarget.contains(e.relatedTarget)) setFocused(false);
    },
  };
}
