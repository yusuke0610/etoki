import { defineConfig, devices } from "@playwright/test";

import { FAKE_ADDR, STACK_ADDR } from "./e2e-stack/addresses";

/**
 * 実物の etoki と偽の GitHub / LLM に繋いだ E2E の設定（ADR 0097）。
 * `make test-e2e-stack` だけが使う。
 *
 * **モックの E2E（`playwright.config.ts`）とは分ける。** あちらは画面の約束を
 * `page.route` で固め（ADR 0012）、こちらは画面とサーバーが本物どうしで往復
 * できることだけを見る。spec の置き場所も分け、モックを持ち込む口は eslint が
 * 落とす（`eslint.config.js`）。混ぜると、どちらの約束を見ているテストなのかが
 * 読めなくなる。
 *
 * ブラウザは devShell（`playwright-driver.browsers`）が提供する。
 */
const baseURL = `http://${STACK_ADDR}`;

export default defineConfig({
  testDir: "./e2e-stack",
  outputDir: "./e2e-output/stack/results",

  // 1 本の中でボードを作って保存し、絵解いて作るところまで進むので、モックの
  // E2E より長く取る。
  timeout: 60_000,
  expect: { timeout: 10_000 },

  // **spec どうしは同じサーバーと同じ偽の GitHub を共有する。** 偽物の状態は
  // 1 回の実行のあいだ残るので、spec はボードを自分で作り、偽の GitHub の記録は
  // 自分の書いた語で絞って読む（`e2e-stack/stack.ts`）。並べても混ざらない。
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // モックの E2E と同じ理由で CI では 2 に抑える（`playwright.config.ts`）。
  workers: process.env.CI ? 2 : undefined,

  reporter: process.env.CI
    ? [["github"], ["html", { outputFolder: "./e2e-output/stack/report", open: "never" }]]
    : [["list"], ["html", { outputFolder: "./e2e-output/stack/report", open: "never" }]],

  use: {
    baseURL,
    trace: "on-first-retry",
    screenshot: "only-on-failure",
  },

  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],

  webServer: {
    // 起動の手順は `make try-fake` と同じ fake-stack を通す（ADR 0097）。ビルドは
    // `make test-e2e-stack` が先に済ませている。
    command: "make --no-print-directory fake-stack",
    cwd: "..",
    env: { ETOKI_ADDR: STACK_ADDR, FAKE_ADDR },
    // **待つのは自分で立てた 2 つが応答したという行。** URL の応答では待たない
    // （`url` も `reuseExistingServer` も書かない）。書くと、同じポートで別の
    // チェックアウトが動かしている etoki が応答した時点で始まり、そちらの
    // コードを検査する（#383）。行を出す前に何を確かめているかは
    // `scripts/fake-stack.sh`。
    wait: {
      stdout: new RegExp(`fake-stack: ready ${baseURL.replaceAll(".", "\\.")}(\\s|$)`),
    },
    // 立てるのはビルド済みのバイナリだけ（ビルドは `make test-e2e-stack` が済ませる）。
    timeout: 60_000,
    // **止めるときは SIGTERM をプロセスグループへ送る。** 既定の SIGKILL では
    // スクリプトの片付け（一時 DB の削除）が走らない。
    gracefulShutdown: { signal: "SIGTERM", timeout: 10_000 },
    // 既定では webServer の stdout を捨てる。起動しなかったときに理由が残らない。
    stdout: "pipe",
  },
});
