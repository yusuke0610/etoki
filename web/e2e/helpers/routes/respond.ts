import type { Route } from "@playwright/test";

import type { ErrorResponse } from "../../../src/api/types";

/**
 * owner 以外に閉じている操作の応答。
 *
 * **404 ではなく 403。** メンバーはボードの存在をすでに知っているので、何が
 * 足りないのかを隠す理由が無い（ADR 0017）。非メンバーの 404 と混ぜない。
 */
export async function ownerOnly(route: Route): Promise<void> {
  await json(route, 403, {
    code: "forbidden_role",
    error: "owner only",
  } satisfies ErrorResponse);
}

export async function json(route: Route, status: number, body: unknown): Promise<void> {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

export function boardIdOf(route: Route): string {
  const segments = new URL(route.request().url()).pathname.split("/");
  // /api/boards/<id>/... の 4 番目が ID。
  return segments[3] ?? "";
}
