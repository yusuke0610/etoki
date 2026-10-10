import { describe, expect, it } from "vitest";

import { clientDisplayName, clientOrigin, UNNAMED_CLIENT } from "./clientLabel";

describe("clientOrigin", () => {
  it("URL で名乗ったクライアントはホストを返す", () => {
    expect(clientOrigin("https://client.example/oauth/meta.json")).toBe("client.example");
    expect(clientOrigin("https://client.example:8443/meta")).toBe("client.example:8443");
  });

  // 動的登録の client_id は乱数。出どころを表さないので出さない。
  it("URL でなければ null", () => {
    expect(clientOrigin("3q2-random-id")).toBeNull();
    expect(clientOrigin("http://client.example/meta")).toBeNull();
    expect(clientOrigin("https://")).toBeNull();
  });
});

describe("clientDisplayName", () => {
  it("名乗らなかったクライアントはそうと出す", () => {
    expect(clientDisplayName("")).toBe(UNNAMED_CLIENT);
    expect(clientDisplayName("  ")).toBe(UNNAMED_CLIENT);
    expect(clientDisplayName("Claude Code")).toBe("Claude Code");
  });
});
