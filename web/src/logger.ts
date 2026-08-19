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

/**
 * 誰も捕まえなかった失敗を残す。登録を外す関数を返す。
 *
 * `ErrorBoundary` が拾うのはレンダリング中の例外だけで、イベントハンドラの中と
 * Promise の reject は素通りする。**そちらは画面に出さない。** 出す手がある
 * のはレンダリングを止めた側だけで、この経路には差し出せる出口（再表示・
 * 読み込み直し）が無い。害の無い reject まで画面を塞ぐことになる。
 *
 * 残らないことだけを直す。ログの行が `[etoki]` で始まるので、ブラウザ拡張や
 * excalidraw の出力に紛れても自前の失敗だと分かる。
 *
 * **開発ビルドでは、境界が受け止めた例外もここを通る。** React は DEV でだけ、
 * スタックを保つために例外を一度 window へ投げ直す（`StrictMode` の二重描画も
 * あるので複数行になる）。同じ失敗が「捕まえていない例外」としても出るのは
 * その都合で、本番ビルドでは通らない。**投げ直されたぶんだけを見分ける手が
 * 無いので、ここでは間引かない。** 間引くつもりで捨てると、本当に誰も
 * 捕まえていない例外まで消える。
 */
export function logUncaught(target: Window = window): () => void {
  // 例外そのものを渡す。message だけにするとスタックが落ちる。ErrorEvent に
  // error が載らない経路（クロスオリジンのスクリプト）だけ文字列に落とす。
  const onError = (e: ErrorEvent) =>
    log.error("捕まえていない例外", (e.error as unknown) ?? e.message);
  const onRejection = (e: PromiseRejectionEvent) =>
    log.error("拾われていない Promise の reject", e.reason);

  target.addEventListener("error", onError);
  target.addEventListener("unhandledrejection", onRejection);

  return () => {
    target.removeEventListener("error", onError);
    target.removeEventListener("unhandledrejection", onRejection);
  };
}
