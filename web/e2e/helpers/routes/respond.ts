import type { Route } from "@playwright/test";

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
