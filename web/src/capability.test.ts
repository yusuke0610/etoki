import { describe, expect, it } from "vitest";

import { ERROR_MESSAGES } from "./api/errorMessage";
import type { Capabilities } from "./api/types";
import { unavailableReason } from "./capability";

const all: Capabilities = { interpretation: true, creation: true, sharing: true };

describe("unavailableReason", () => {
  it("使える機能には理由を出さない", () => {
    expect(unavailableReason(all, "interpretation")).toBeNull();
  });

  // 押した後に 503 で返る理由と、押す前に出す文言を別に持たない（ADR 0030）。
  it("使えない機能は、503 と同じ文言を返す", () => {
    expect(unavailableReason({ ...all, interpretation: false }, "interpretation")).toBe(
      ERROR_MESSAGES.llm_not_configured,
    );
    expect(unavailableReason({ ...all, creation: false }, "creation")).toBe(
      ERROR_MESSAGES.github_not_configured,
    );
    expect(unavailableReason({ ...all, sharing: false }, "sharing")).toBe(
      ERROR_MESSAGES.sharing_not_configured,
    );
  });

  // 設定するものが違うので畳まない。畳むと「何を設定すればよいか」を言えない。
  it("機能ごとに違う文言を出す", () => {
    const messages = (["interpretation", "creation", "sharing"] as const).map((f) =>
      unavailableReason({ interpretation: false, creation: false, sharing: false }, f),
    );

    expect(new Set(messages).size).toBe(messages.length);
  });

  // まだ引けていない状態を「使えない」に倒すと、確かめていないことを確かめた
  // ように見せることになる（中核思想 3）。押させておけば 503 が理由を返す。
  it("まだ確かめていないうちは止めない", () => {
    expect(unavailableReason(null, "interpretation")).toBeNull();
    expect(unavailableReason(null, "creation")).toBeNull();
    expect(unavailableReason(null, "sharing")).toBeNull();
  });
});
