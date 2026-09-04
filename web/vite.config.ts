import { createReadStream } from "node:fs";
import { cp } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

// バックエンドの既定リッスンアドレス。make dev は両者を同時に起動するため、
// dev サーバーからは同一オリジンに見えるようプロキシしておく。
const API_TARGET = "http://127.0.0.1:8080";

// Excalidraw が同梱している手書きフォントの実体。
const excalidrawFonts = fileURLToPath(
  new URL("./node_modules/@excalidraw/excalidraw/dist/prod/fonts", import.meta.url),
);

// キャンバスのフォントを配る URL。オリジン相対にする。
//
// **この値を持つのはここだけ。** 配る側（dev の middleware とビルドの写し先）と
// 向ける側（index.html に差し込む window.EXCALIDRAW_ASSET_PATH）が同じ定数から
// 出るようにしてある。アプリ側のモジュールに書き写すと、片方を変えた日に
// もう片方が静かに CDN へ戻る。
const EXCALIDRAW_ASSET_PATH = "/excalidraw-assets/";

/**
 * Excalidraw の手書きフォントを etoki 自身の配信元から配る。
 *
 * **指定しないと実行時に esm.sh へ取りに行く。** etoki は既定で 127.0.0.1 に
 * しかバインドしないローカルツールなので（ADR 0004）、キャンバスを開くたびに
 * 第三者のホストへ出ていくのは前提と食い違う。ネットワークの無い環境では毎回
 * 失敗し、**注釈範囲の画像は見た目そのものを LLM に渡している**（ADR 0018）
 * ので、フォントが読めた環境と読めなかった環境では同じシーンから別の画像が
 * 渡ることになる。
 *
 * **`make dev` と `make start` の両方で同じ URL を配る。** dev では Vite が
 * web/ の下しか配らずフォントの実体は node_modules にあるので、読む側だけを
 * middleware で繋ぐ。ビルドでは成果物へ写し、etoki が `ETOKI_WEB_DIR` から
 * 配る。実体の置き場所は違うが、キャンバスが組み立てる URL は同じになる。
 *
 * **向け先は index.html に差し込む。** モジュールの中で代入すると、
 * 「Excalidraw を読み込むより先に走るか」に依存する。
 *
 * **丸ごと写す。14 MB ある。** 使うものだけに絞れば小さくなるが、Excalidraw が
 * 既定のフォントを変えた日に、絞った前提が崩れたことに気づけない。指定した先が
 * 404 になると必ず esm.sh へ落ちるので、絞り漏れは画面のどこにも出ない。
 * 減ることより気づけることを取る。
 */
function excalidrawFontAssets(): Plugin {
  return {
    name: "etoki:excalidraw-font-assets",

    transformIndexHtml() {
      return [
        {
          tag: "script",
          // フォントを取りに行くのは描画のときなので head で足りるが、
          // アプリより先に確定させておく。
          injectTo: "head-prepend",
          children: `window.EXCALIDRAW_ASSET_PATH=${JSON.stringify(EXCALIDRAW_ASSET_PATH)};`,
        },
      ];
    },

    configureServer(server) {
      // connect は一致した接頭辞を req.url から外して渡す。
      server.middlewares.use(`${EXCALIDRAW_ASSET_PATH}fonts`, (req, res) => {
        // 読めなかったら next() ではなく 404 を返す。**dev サーバーは当たら
        // なかったパスに index.html を 200 で返す。** 素通しにすると、写し
        // 漏れたフォントが「取れた」ことになり、Excalidraw は HTML を woff2
        // として読む。CDN への fallback にすら落ちないので、壊れたことが
        // どこにも出ない。
        const notFound = (): void => {
          res.statusCode = 404;
          res.end();
        };

        // 不正な percent-encoding は decodeURIComponent が例外を投げる。
        // 例外のまま抜けると notFound() を経ずに落ちるので、ここで拾う。
        let rel: string;
        try {
          rel = decodeURIComponent((req.url ?? "").split("?")[0] ?? "");
        } catch {
          notFound();
          return;
        }
        const file = path.join(excalidrawFonts, rel);

        // フォントの外に出るパスは配らない。dev サーバーは開発者の手元の
        // ファイルを読める位置にいるので、辿らせない。
        if (!file.startsWith(excalidrawFonts + path.sep)) {
          notFound();
          return;
        }

        createReadStream(file)
          .on("error", notFound)
          .once("open", () => res.setHeader("Content-Type", "font/woff2"))
          .pipe(res);
      });
    },

    // writeBundle は出力先を引数で受け取る。outDir を別に読み直すと、設定を
    // 変えたときにここだけ古い場所へ写す。
    async writeBundle(options) {
      if (!options.dir) return;
      await cp(excalidrawFonts, path.join(options.dir, EXCALIDRAW_ASSET_PATH, "fonts"), {
        recursive: true,
      });
    },
  };
}

// @excalidraw/excalidraw の exports は development / production で実体が分かれる。
// vitest は development 側を引き当てるが、そちらはバンドルサイズが大きく
// テストには不要なので prod 側を直接指す。alias は test スコープに閉じており、
// アプリのビルドと dev サーバーは通常の解決のままになる。
const excalidrawProd = fileURLToPath(
  new URL("./node_modules/@excalidraw/excalidraw/dist/prod/index.js", import.meta.url),
);

export default defineConfig({
  plugins: [react(), excalidrawFontAssets()],
  server: {
    // 既定の localhost にせず IPv4 ループバックを明示する。localhost の解決は
    // 環境まかせで、IPv6 のある環境では ::1 に寄って 127.0.0.1 では届かなく
    // なる。バックエンドの既定も 127.0.0.1 なので、そちらに揃える。
    host: "127.0.0.1",
    port: 5173,
    // ポートが埋まっていたら黙って隣にずらさず落とす。ずらされると
    // Playwright が 5173 を待ち続け、原因の分からないタイムアウトになる。
    strictPort: true,
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
