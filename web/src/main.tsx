import "./index.css";

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import { ErrorBoundary } from "./ErrorBoundary";
import { logUncaught } from "./logger";
import { applyTheme, initialTheme } from "./theme";

const container = document.getElementById("root");
if (!container) {
  throw new Error("#root not found");
}

// 境界が拾わない経路（イベントハンドラの中と Promise の reject）を console に
// 残す。画面には出さない（ADR 0027）。
logUncaught();

// React が描く前に配色を決める。App の中で初めて載せると、ダークの人にも
// 最初の 1 フレームだけ明るい画面が出る。以後の切り替えは App の `useTheme`。
applyTheme(initialTheme());

createRoot(container).render(
  <StrictMode>
    {/*
      ここは最後の受け皿。キャンバスごと落ちたときだけ出るので、未保存のものは
      すでに失われている。**手前で受けられるものは手前で受ける**（BoardPage の
      パネル）。ここまで来ると読み込み直すしか無い（ADR 0027）。
    */}
    <ErrorBoundary name="アプリ全体" recovery="reload">
      <App />
    </ErrorBoundary>
  </StrictMode>,
);
