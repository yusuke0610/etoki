import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { log, logUncaught, setLogLevel } from "./logger";

// spy はテストごとに張り直す。restoreAllMocks で console が元に戻るため。
let spies: Record<"debug" | "info" | "warn" | "error", ReturnType<typeof vi.spyOn>>;

beforeEach(() => {
  spies = {
    debug: vi.spyOn(console, "debug").mockImplementation(() => {}),
    info: vi.spyOn(console, "info").mockImplementation(() => {}),
    warn: vi.spyOn(console, "warn").mockImplementation(() => {}),
    error: vi.spyOn(console, "error").mockImplementation(() => {}),
  };
  setLogLevel("debug");
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("log", () => {
  // excalidraw やブラウザ拡張の出力に紛れるので、自前の行だと分かるようにする。
  it("メッセージに etoki のプレフィックスを付ける", () => {
    log.info("保存した");

    expect(spies.info).toHaveBeenCalledWith("[etoki] 保存した");
  });

  it("レベルごとに対応する console のメソッドを使う", () => {
    log.debug("d");
    log.info("i");
    log.warn("w");
    log.error("e");

    expect(spies.debug).toHaveBeenCalledWith("[etoki] d");
    expect(spies.warn).toHaveBeenCalledWith("[etoki] w");
    expect(spies.error).toHaveBeenCalledWith("[etoki] e");
  });

  // 例外は文字列化せずに渡す。console がスタックを展開してくれるため。
  it("追加の詳細はそのまま渡す", () => {
    const cause = new Error("boom");

    log.error("保存できなかった", cause, { boardId: "b1" });

    expect(spies.error).toHaveBeenCalledWith("[etoki] 保存できなかった", cause, {
      boardId: "b1",
    });
  });
});

describe("setLogLevel", () => {
  it("しきい値より軽いものは出力しない", () => {
    setLogLevel("warn");

    log.debug("d");
    log.info("i");

    expect(spies.debug).not.toHaveBeenCalled();
    expect(spies.info).not.toHaveBeenCalled();
  });

  it("しきい値以上は出力する", () => {
    setLogLevel("warn");

    log.warn("w");
    log.error("e");

    expect(spies.warn).toHaveBeenCalledTimes(1);
    expect(spies.error).toHaveBeenCalledTimes(1);
  });
});

describe("logUncaught", () => {
  // 境界（ErrorBoundary）が拾うのはレンダリング中の例外だけ。ここが無いと、
  // イベントハンドラの中で落ちた失敗は console のスタックだけになり、自前の
  // 行として残らない。
  it("捕まえていない例外を error として残す", () => {
    const stop = logUncaught(window);
    const cause = new Error("boom");

    window.dispatchEvent(new ErrorEvent("error", { error: cause, message: "boom" }));

    expect(spies.error).toHaveBeenCalledWith("[etoki] 捕まえていない例外", cause);
    stop();
  });

  // error が載らない経路（クロスオリジンのスクリプト）では message しか無い。
  it("例外が載っていなければ message を残す", () => {
    const stop = logUncaught(window);

    window.dispatchEvent(new ErrorEvent("error", { message: "Script error." }));

    expect(spies.error).toHaveBeenCalledWith(
      "[etoki] 捕まえていない例外",
      "Script error.",
    );
    stop();
  });

  it("拾われていない reject を error として残す", () => {
    const stop = logUncaught(window);
    const reason = new Error("rejected");
    const event = Object.assign(new Event("unhandledrejection"), { reason });

    window.dispatchEvent(event);

    expect(spies.error).toHaveBeenCalledWith(
      "[etoki] 拾われていない Promise の reject",
      reason,
    );
    stop();
  });

  it("返した関数で登録を外せる", () => {
    const stop = logUncaught(window);

    stop();
    window.dispatchEvent(new ErrorEvent("error", { message: "boom" }));

    expect(spies.error).not.toHaveBeenCalled();
  });
});
