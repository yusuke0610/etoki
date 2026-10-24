import { defineConfig, devices } from "@playwright/test";

/**
 * 使い方の動画を録る設定（ADR 0081）。`make tutorial-capture` だけが使う。
 *
 * **E2E の設定と分ける。** 録るのはテストではなく生成器で、ffmpeg が要り、結果は
 * コミットする動画になる。`make test-e2e` に混ぜると、テストを回すたびに動画が
 * 書き換わる。
 *
 * 大きさは 1280 × 720 で固定する。録画と画面の大きさを揃えないと、縮めて
 * 録られて文字が潰れる。
 */
export default defineConfig({
  testDir: "./e2e",
  testMatch: /tutorial\.capture\.ts$/,
  outputDir: "./e2e-output/tutorial/results",
  timeout: 120_000,
  // 2・3 ページ目は 1 ページ目が保存したシーンを読むので、順に走らせる。
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: "http://127.0.0.1:5173",
    viewport: { width: 1280, height: 720 },
    video: { mode: "on", size: { width: 1280, height: 720 } },
  },
  // テーマは OS に追従する（ADR 0055）ので、両方を録る。
  projects: [
    { name: "light", use: { ...devices["Desktop Chrome"], colorScheme: "light" } },
    { name: "dark", use: { ...devices["Desktop Chrome"], colorScheme: "dark" } },
  ],
  webServer: {
    command: "bun run dev",
    url: "http://127.0.0.1:5173",
    reuseExistingServer: true,
    timeout: 120_000,
  },
});
