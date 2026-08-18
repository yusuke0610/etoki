import { describe, expect, it } from "vitest";

import { ApiError } from "./boards";
import { describeFailure, ERROR_MESSAGES } from "./errorMessage";

describe("describeFailure", () => {
  it("既知の code は表の文言を出し、サーバーの本文は畳んだ側に回す", () => {
    const e = new ApiError(409, "scene_conflict", "etoki: board scene was updated");

    expect(describeFailure("保存できませんでした", e)).toEqual({
      message: `保存できませんでした: ${ERROR_MESSAGES.scene_conflict}`,
      detail: "etoki: board scene was updated",
    });
  });

  // 同じ 409 でも打ち手が違う。ここが畳まれると、画面は「衝突しました」としか
  // 言えなくなる（#86 の出発点）。
  it("同じステータスでも code が違えば違う打ち手を出す", () => {
    const conflict = describeFailure(
      "保存できませんでした",
      new ApiError(409, "scene_conflict", "hint"),
    );
    const locked = describeFailure(
      "設定できませんでした",
      new ApiError(409, "target_locked", "hint"),
    );

    expect(conflict.message).not.toBe(locked.message);
  });

  // サーバーのほうが新しいと、まだ知らない code が来る。畳んで隠すと画面は
  // 何も言えなくなるので、そのときだけ本文を前に出す。
  it("知らない code は本文をそのまま見せる", () => {
    const e = new ApiError(418, "brand_new_code", "サーバーからの説明");

    expect(describeFailure("できませんでした", e)).toEqual({
      message: "できませんでした: サーバーからの説明",
      detail: "",
    });
  });

  it("通信そのものの失敗は、操作の名前だけを見せて中身は畳む", () => {
    const e = new TypeError("Failed to fetch");

    expect(describeFailure("保存できませんでした", e)).toEqual({
      message: "保存できませんでした",
      detail: "TypeError: Failed to fetch",
    });
  });
});

describe("ERROR_MESSAGES", () => {
  // 網羅は Record<ErrorCode, string> が tsc で見る。ここで見るのは中身のほう。
  it("空の文言を持たない", () => {
    for (const [code, message] of Object.entries(ERROR_MESSAGES)) {
      expect(message, code).not.toBe("");
    }
  });

  // 設定するものが違うので畳まない。畳むと「何を設定すればよいか」を言えない。
  it("未設定の 4 つは別々の文言を持つ", () => {
    const messages = [
      ERROR_MESSAGES.llm_not_configured,
      ERROR_MESSAGES.github_not_configured,
      ERROR_MESSAGES.auth_not_configured,
      ERROR_MESSAGES.sharing_not_configured,
    ];

    expect(new Set(messages).size).toBe(messages.length);
  });
});
