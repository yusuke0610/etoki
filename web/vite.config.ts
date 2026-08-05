import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// バックエンドの既定リッスンアドレス。make dev は両者を同時に起動するため、
// dev サーバーからは同一オリジンに見えるようプロキシしておく。
const API_TARGET = "http://127.0.0.1:8080";

// @excalidraw/excalidraw の exports は development / production で実体が分かれる。
// vitest は development 側を引き当てるが、そちらはバンドルサイズが大きく
// テストには不要なので prod 側を直接指す。alias は test スコープに閉じており、
// アプリのビルドと dev サーバーは通常の解決のままになる。
const excalidrawProd = fileURLToPath(
  new URL("./node_modules/@excalidraw/excalidraw/dist/prod/index.js", import.meta.url),
);

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: API_TARGET },
      "/healthz": { target: API_TARGET },
    },
  },
  test: {
    // e2e/ の spec は Playwright が実行する。vitest の既定の include は
    // *.spec.ts も拾うため、明示的に src 配下だけに絞る。
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
    alias: {
      "@excalidraw/excalidraw": excalidrawProd,
    },
    server: {
      deps: {
        // excalidraw は open-color を import するが、その実体は .json であり
        // Node の ESM 解決では import attribute なしに読めない。inline に
        // 指定して Vite の変換を通すことで解決させる。
        inline: [/@excalidraw\/excalidraw/, /open-color/],
      },
    },
  },
});
