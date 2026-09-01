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

// jsdom は CSS Font Loading API を実装していない。@excalidraw/excalidraw は
// 文字を測るときにフォントの登録簿を組み立てるので、FontFace が無いと
// ラベル付きの要素を作った時点で落ちる（convertToExcalidrawElements）。
//
// canvas と同じで、ここで要るのは「読み込めること」だけ。**測った幅は本物では
// ない。** 文字の寸法に依存する検証はブラウザ側（E2E）に置く。
if (typeof globalThis.FontFace === "undefined") {
  class StubFontFace {
    readonly family: string;
    readonly status = "loaded";
    constructor(family: string) {
      this.family = family;
    }
    load(): Promise<StubFontFace> {
      return Promise.resolve(this);
    }
  }

  globalThis.FontFace = StubFontFace as unknown as typeof FontFace;
}

// jsdom の Blob は arrayBuffer を実装していない。ブラウザにはあるので、無い側に
// 合わせて FileReader で書き直すのではなく、ここで補う。
if (typeof Blob.prototype.arrayBuffer !== "function") {
  Blob.prototype.arrayBuffer = function arrayBuffer(this: Blob): Promise<ArrayBuffer> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result as ArrayBuffer);
      reader.onerror = () => reject(reader.error);
      reader.readAsArrayBuffer(this);
    });
  };
}
