/**
 * 通しの E2E（ADR 0097）が立てる etoki と偽の上流のアドレス。
 *
 * 設定（`playwright.stack.config.ts`）と spec の両方が読むので、ここ 1 か所で
 * 決める。**`make dev`（8080 / 5173）・`make try-fake`（8080 / 8090）・
 * `make test-e2e`（5174）と分けてある。** それらを手元で動かしたまま回せるように
 * するため。埋まっていたら繋ぎにいかずに起動で落ちる（`scripts/fake-stack.sh`）。
 * 2 つのチェックアウトで同時に回すときは `E2E_STACK_PORT` / `E2E_STACK_FAKE_PORT` で
 * 別のポートを渡す。
 */

function portFromEnv(name: string, fallback: number): number {
  const raw = process.env[name];
  if (raw === undefined) return fallback;
  const port = Number(raw);
  if (!Number.isInteger(port) || port <= 0 || port > 65535) {
    throw new Error(`${name} にはポート番号を渡す（受け取った値: ${raw}）`);
  }
  return port;
}

/** etoki を立てる先。ビルド済みの画面もここから配る。 */
export const STACK_ADDR = `127.0.0.1:${portFromEnv("E2E_STACK_PORT", 8181)}`;

/** 偽の GitHub / LLM を立てる先。 */
export const FAKE_ADDR = `127.0.0.1:${portFromEnv("E2E_STACK_FAKE_PORT", 8191)}`;
