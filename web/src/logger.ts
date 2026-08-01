/**
 * ブラウザ側で起きた失敗を残すための最小限のロガー。
 *
 * バックエンドは slog に寄せてあるが、フロントで完結する失敗（シーンの
 * パース失敗、fetch 自体の失敗）はどこにも残らない。単一ユーザーがローカルで
 * 動かす前提なので送信先は console だけでよく、収集基盤は持たない。
 */

/** ログの重要度。数値が大きいほど重い。 */
const severity = { debug: 10, info: 20, warn: 30, error: 40 } as const;

export type LogLevel = keyof typeof severity;

// 開発中は debug まで、本番ビルドでは info から出す。
let threshold: LogLevel = import.meta.env.DEV ? "debug" : "info";

/** 出力するしきい値を変える。既定はビルドモード任せなので、変えたいときだけ呼ぶ。 */
export function setLogLevel(level: LogLevel): void {
  threshold = level;
}

function emit(level: LogLevel, message: string, details: unknown[]): void {
  if (severity[level] < severity[threshold]) return;

  // 例外は文字列化せずに渡す。console がスタックを展開してくれるため。
  console[level](`[etoki] ${message}`, ...details);
}

export const log = {
  debug: (message: string, ...details: unknown[]) => emit("debug", message, details),
  info: (message: string, ...details: unknown[]) => emit("info", message, details),
  warn: (message: string, ...details: unknown[]) => emit("warn", message, details),
  error: (message: string, ...details: unknown[]) => emit("error", message, details),
};
