import "@testing-library/jest-dom/vitest";

// jsdom は HTMLCanvasElement.getContext を実装しておらず、既定では null を返す。
// @excalidraw/excalidraw は import 時に 2D コンテキストの機能検出を行うため、
// null が返ると読み込み自体が失敗する。
//
// 本物の canvas（native モジュール）を入れると Nix 環境でのビルドが重くなる
// うえ、etoki のテストが検証したいのは描画結果ではなくデータの往復である。
// 最小限のスタブで足りる。描画結果を検証したくなったら、そのときに
// ブラウザ実行のテストを別途用意する。
const stub2DContext = {
  filter: "none",
  font: "",
  measureText: () => ({ width: 0 }),
  fillRect: () => {},
  clearRect: () => {},
  drawImage: () => {},
  getImageData: () => ({ data: new Uint8ClampedArray(4) }),
  putImageData: () => {},
  save: () => {},
  restore: () => {},
  scale: () => {},
  translate: () => {},
  setTransform: () => {},
  beginPath: () => {},
  closePath: () => {},
  fill: () => {},
  stroke: () => {},
};

HTMLCanvasElement.prototype.getContext = function getContext(contextID: string): unknown {
  return contextID === "2d" ? stub2DContext : null;
} as typeof HTMLCanvasElement.prototype.getContext;
