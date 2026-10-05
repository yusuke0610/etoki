import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ErrorBoundary } from "./ErrorBoundary";
import { log } from "./logger";

// React は境界が受け止めた例外も console.error に出す。テストの出力を汚さない
// ために黙らせるが、log.error のほうは spy で見る。
let logError: ReturnType<typeof vi.spyOn>;

beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  logError = vi.spyOn(log, "error").mockImplementation(() => {});
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const BOOM = "content_hash of undefined";

function Boom(): never {
  throw new Error(BOOM);
}

describe("ErrorBoundary", () => {
  it("子が落ちても真っ白にせず、理由と出口を出す", () => {
    render(
      <ErrorBoundary name="注釈パネル" recovery="remount">
        <Boom />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent("この部分を表示できませんでした");
    expect(screen.getByRole("button", { name: "再表示" })).toBeInTheDocument();
  });

  // フロントで完結する失敗はどこにも残らない（logger.ts）。境界が受け止めた
  // ぶんは、ここで流さなければ console のスタックだけになる。
  it("受け止めた例外を log.error に流す", () => {
    render(
      <ErrorBoundary name="注釈パネル" recovery="remount">
        <Boom />
      </ErrorBoundary>,
    );

    expect(logError).toHaveBeenCalledOnce();
    const [message, error] = logError.mock.calls[0] ?? [];
    expect(message).toContain("注釈パネル");
    // 例外は文字列化せずに渡す。console がスタックを展開するため。
    expect(error).toBeInstanceOf(Error);
    expect((error as Error).message).toBe(BOOM);
  });

  // 生のエラー文字列を画面に出さない（#46 と同じ論点）。読めないものが増える
  // だけで、打てる手は変わらない。
  it("例外の中身は画面に出さない", () => {
    render(
      <ErrorBoundary name="注釈パネル" recovery="remount">
        <Boom />
      </ErrorBoundary>,
    );

    expect(screen.queryByText(new RegExp(BOOM))).not.toBeInTheDocument();
    expect(screen.getByText(/コンソールに出しています/)).toBeInTheDocument();
  });

  // キャンバスの外側で落ちたときの出口。ページを読み込み直さずに戻せないと、
  // 保存していないブレストを捨てることになる。
  it("remount は子を作り直す", () => {
    let failing = true;
    function Flaky() {
      if (failing) throw new Error(BOOM);
      return <p>注釈</p>;
    }

    render(
      <ErrorBoundary name="注釈パネル" recovery="remount">
        <Flaky />
      </ErrorBoundary>,
    );

    failing = false;
    fireEvent.click(screen.getByRole("button", { name: "再表示" }));

    expect(screen.getByText("注釈")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // 読み込み直すとキャンバスの内容は戻らない。押す前に、何を失うのかが
  // 読めている必要がある（ADR 0021 と同じ立て付け）。
  it("reload は失われることを先に言う", () => {
    render(
      <ErrorBoundary name="アプリ全体" recovery="reload">
        <Boom />
      </ErrorBoundary>,
    );

    expect(screen.getByRole("alert")).toHaveTextContent(
      "保存していないブレストは失われています",
    );
    expect(screen.getByRole("button", { name: "読み込み直す" })).toBeInTheDocument();
  });
});
