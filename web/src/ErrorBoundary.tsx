import { Component, type ErrorInfo, type ReactNode } from "react";

import { log } from "./logger";

/**
 * 落ちたあとに差し出す出口。**巻き込んだ範囲が違うので、言うことも変わる。**
 *
 * - `remount`: 子だけを作り直す。キャンバスの外側で落ちたときに使う。
 *   キャンバスは載ったままなので、描いたものはまだ画面にある。
 * - `reload`: ページごと読み込み直す。ツリーが外れたあとなので、未保存の
 *   ものはもう戻らない。**戻らないことを先に言う。**
 */
export type Recovery = "remount" | "reload";

type Props = {
  /**
   * どこが落ちたのかを表す名前。**console にだけ出す。**
   *
   * 画面に出すと、利用者には手掛かりにならないものが増えるだけになる。
   */
  name: string;
  recovery: Recovery;
  children: ReactNode;
};

type State = { failed: boolean };

const MESSAGE: Record<Recovery, string> = {
  remount:
    "この部分を表示できませんでした。キャンバスに描いたものはそのまま残っています。続ける前に保存してください。",
  reload:
    "画面を表示できませんでした。保存していないブレストは失われています。読み込み直すと、最後に保存した状態から再開します。",
};

const ACTION: Record<Recovery, string> = {
  remount: "再表示",
  reload: "読み込み直す",
};

/**
 * レンダリング中の例外を受け止め、真っ白にせず理由を出す。
 *
 * React は境界が無いとツリー全体を unmount する。`#root` が空になり、
 * **そこまでのブレストが画面から消える。** シーンは保存するまで Excalidraw の
 * メモリにしかないので、消えた時点で取り返せない。
 *
 * 境界が拾うのはレンダリング中の例外だけで、イベントハンドラの中と Promise の
 * reject は素通りする。そちらは `logUncaught`（`logger.ts`）が console に残す。
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false };

  static getDerivedStateFromError(): State {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // 例外は文字列化せずに渡す。console がスタックを展開する（`logger.ts`）。
    // componentStack は React が組み立てた文字列で、どの木で落ちたかが読める。
    log.error(`${this.props.name}を表示できませんでした`, error, info.componentStack);
  }

  private readonly recover = (): void => {
    if (this.props.recovery === "reload") {
      window.location.reload();
      return;
    }
    // 子は unmount 済みなので、state を戻せばそのまま作り直される。
    this.setState({ failed: false });
  };

  render(): ReactNode {
    if (!this.state.failed) return this.props.children;

    const { recovery } = this.props;

    return (
      <div className="error-boundary" role="alert">
        {/*
          例外の中身はそのまま出さない。利用者に読めないものを見せることになり、
          出せる手も変わらない。行き先は console に固定して、そこを教える。
        */}
        <p>{MESSAGE[recovery]}</p>
        <p className="hint">くわしい内容はブラウザのコンソールに出しています。</p>
        <button type="button" onClick={this.recover}>
          {ACTION[recovery]}
        </button>
      </div>
    );
  }
}
