import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("App", () => {
  it("バックエンドが応答すれば ok を表示する", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(new Response(null, { status: 200 })),
    );

    render(<App />);

    expect(await screen.findByText("ok")).toBeInTheDocument();
  });

  it("バックエンドに到達できなければ unreachable を表示する", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("connection refused")));

    render(<App />);

    expect(await screen.findByText("unreachable")).toBeInTheDocument();
  });
});
