import { defineConfig, devices } from "@playwright/test";

/**
 * E2E テストの設定。
 *
 * バックエンドは起動しない。すべての API 呼び出しは `e2e/helpers/api.ts` が
 * `page.route` で差し替える。LLM と GitHub は外部サービスであり、実物に繋ぐと
 * テストが鍵とネットワークに依存する。ここで確かめたいのは「開発者が押した
 * ときだけ動く」という画面側の約束であって、外部連携そのものではない。
 *
 * ブラウザは devShell（`playwright-driver.browsers`）が提供する。
 * `PLAYWRIGHT_BROWSERS_PATH` は shellHook で設定済みなので、ここでは触らない。
 */
export default defineConfig({
  testDir: "./e2e",
  outputDir: "./e2e-output/results",

  timeout: 30_000,
  expect: { timeout: 5_000 },

  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // Excalidraw のキャンバスは描画が重い。CI では並列度を落として、
  // 実装ではなくマシンの都合で落ちるのを避ける。
  workers: process.env.CI ? 1 : undefined,

  reporter: process.env.CI
    ? [["github"], ["html", { outputFolder: "./e2e-output/report", open: "never" }]]
    : [["list"], ["html", { outputFolder: "./e2e-output/report", open: "never" }]],

  use: {
    baseURL: "http://127.0.0.1:5173",
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },

  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],

  webServer: {
    command: "bun run dev",
    // vite.config.ts が host と port を固定しているので、ここと必ず一致する。
    url: "http://127.0.0.1:5173",
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
    // 既定では dev サーバーの stdout を捨てる。起動しなかったとき
    // 「120 秒待った」以外の情報が残らず、CI で原因を追えない。
    stdout: "pipe",
  },
});
