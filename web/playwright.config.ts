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

/**
 * E2E のためだけに立てる dev サーバーのポート（#383）。
 *
 * **`make dev` の 5173 と分け、既に動いているサーバーも使わない。** 使うと、
 * 別のチェックアウト（worktree）で動いている dev サーバーに繋ぎ、そちらの
 * ブランチのコードを黙って検査する。無関係な失敗が並ぶか、壊れた変更が緑で
 * 通る。このポートが埋まっていたら、繋ぎにいかずに起動で落ちる
 * （`vite.config.ts` の `strictPort`）。2 つのチェックアウトで同時に回すときは
 * `E2E_PORT` で別のポートを渡す。
 */
const port = Number(process.env.E2E_PORT ?? "5174");
if (!Number.isInteger(port) || port <= 0) {
  throw new Error(
    `E2E_PORT にはポート番号を渡す（受け取った値: ${process.env.E2E_PORT}）`,
  );
}
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./e2e-output/results",

  timeout: 30_000,
  expect: { timeout: 5_000 },

  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // Excalidraw のキャンバスは描画が重い。CI では並列度を 2 に抑えて、
  // 実装ではなくマシンの都合で落ちるのを避ける。1 にするとランナー（4 vCPU）が
  // 遊んだまま全件を直列に回し、CI の 8 割が E2E 待ちになっていた（#269）。
  // 同じ大きさの手元では、2 にしても 1 のときに無かった失敗や flaky は
  // 出なかった（手元の環境だけで落ちる 1 件は 1 でも同じく落ちる）。4 では
  // flaky が出た。
  // 上げるなら、retries に吸われて緑のまま延びていないかを CI のログで見てから。
  workers: process.env.CI ? 2 : undefined,

  reporter: process.env.CI
    ? [["github"], ["html", { outputFolder: "./e2e-output/report", open: "never" }]]
    : [["list"], ["html", { outputFolder: "./e2e-output/report", open: "never" }]],

  use: {
    baseURL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },

  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],

  webServer: {
    // host は vite.config.ts が 127.0.0.1 に固定している。ポートだけ上書きする。
    command: `bun run dev --port ${port}`,
    // **既に動いているサーバーは使わず、手元でも毎回立てる**（上の `port` の
    // 理由）。立ち上げに数秒かかるが、何を検査したのか分からない速さより、
    // 毎回自分のコードを見るほうを採る。
    //
    // そのために **URL の応答では待たない（`url` も `reuseExistingServer` も
    // 書かない）。** 待つのは、ここで立てた Vite 自身が「Local: <baseURL>」を
    // 出すこと。Playwright は `url` と `wait` の早いほうで待ちをやめるので、
    // `url` を書くと、同じポートで同時に立てた別のチェックアウトのサーバーが
    // 応答した時点で始まり、自分の Vite がポートを取れずに落ちる前にそちらを
    // 検査する。ポートが埋まっていれば自分の Vite が `strictPort` で落ち、
    // Playwright は起動の失敗として止まる。
    wait: { stdout: new RegExp(`Local:\\s+${baseURL.replaceAll(".", "\\.")}/`) },
    timeout: 120_000,
    // 既定では dev サーバーの stdout を捨てる。起動しなかったとき
    // 「120 秒待った」以外の情報が残らず、CI で原因を追えない。
    stdout: "pipe",
  },
});
