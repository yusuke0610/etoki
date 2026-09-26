/**
 * 画面全体に出す通知（ADR 0058）。
 *
 * **通知は「過ぎ去ってよい失敗」の器。** 残す必要がある状態（保存の衝突、
 * 注釈ごとの解釈・作成の失敗、部分失敗）は通知にしない。消えた瞬間に読め
 * なくなる。何を通知に出すかの線引きは `web/CLAUDE.md` にある。
 *
 * 画面の中だけの概念なので、`api/openapi.yaml` にも `api/types.ts` にも置かない。
 */

/**
 * 通知の種別。見た目と読み上げの割り込み方が変わる。
 *
 * **今回の呼び出し元は `error` だけ。** 成功を通知に重ねると、同じ事実が
 * 状態表示（「未保存」の消灯、作成結果の一覧）と 2 箇所に出て、どちらが状態で
 * どちらが履歴か読めなくなる。型として持つのは、後から足すときに器を
 * 作り直さないため。
 */
export type NotificationKind = "error" | "warning" | "success" | "info";

/** 通知から直接押せる次の一手。1 件に 0 個か 1 個。 */
export type NotificationAction = {
  label: string;
  run: () => void;
};

/** `notify()` に渡すもの。 */
export type NotifyOptions = {
  kind: NotificationKind;
  /** 読むべき文（次に何をすればよいか）。 */
  message: string;
  /** サーバーが返した本文などの手掛かり。既定で畳む。 */
  detail?: string;
  action?: NotificationAction;
  /**
   * 自動で消えるまでのミリ秒。`null` は消さない。省けば種別ごとの既定。
   * **`action` を持つ通知は、ここに何を渡しても消さない。** 消える瞬間に
   * 押そうとした手が空振りする。
   */
  duration?: number | null;
  /** 同じ key の通知は積まずに置き換える。同じ失敗の繰り返しで埋めない。 */
  key?: string;
};

/** 画面に出ている通知 1 件。 */
export type Notification = {
  id: number;
  kind: NotificationKind;
  message: string;
  detail: string;
  action?: NotificationAction;
  /** 自動で消えるまでのミリ秒。`null` は消さない。 */
  duration: number | null;
  key?: string;
};
