import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useEffect } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { NotificationProvider, useNotify, type NotifyApi } from "./NotificationProvider";
import { Notifications } from "./Notifications";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/** Provider の内側で useNotify() を取り出し、テストから呼べるようにする。 */
function setup() {
  const api: { current: NotifyApi | null } = { current: null };
  function Capture() {
    const notify = useNotify();
    useEffect(() => {
      api.current = notify;
    }, [notify]);
    return null;
  }
  render(
    <NotificationProvider>
      <Capture />
      <Notifications />
    </NotificationProvider>,
  );
  const notify: NotifyApi["notify"] = (options) => {
    let id = 0;
    act(() => {
      id = api.current!.notify(options);
    });
    return id;
  };
  return { notify, api };
}

describe("Notifications", () => {
  // 入れ物ごと差し込むと、読み上げソフトが変化として拾わないことがある。
  it("0 件でも入れ物が DOM にある", () => {
    setup();
    expect(document.querySelector(".notifications")).toBeInTheDocument();
  });

  it("error は alert、info は status で読める", () => {
    const { notify } = setup();
    notify({ kind: "error", message: "保存できませんでした" });
    notify({ kind: "info", message: "お知らせです" });

    expect(screen.getByRole("alert")).toHaveTextContent("保存できませんでした");
    expect(screen.getByRole("status")).toHaveTextContent("お知らせです");
  });

  // 色だけで種別を伝えない（#62）。
  it("種別を文字でも出す", () => {
    const { notify } = setup();
    notify({ kind: "error", message: "m" });
    expect(screen.getByRole("alert")).toHaveTextContent("エラー");
  });

  // #89 の 1-1: 1 本の state では後から来た失敗が前を消していた。
  it("複数出しても両方読める", () => {
    const { notify } = setup();
    notify({ kind: "error", message: "保存できませんでした" });
    notify({ kind: "error", message: "注釈の状態を取得できませんでした" });

    const alerts = screen.getAllByRole("alert");
    expect(alerts).toHaveLength(2);
    expect(alerts[0]).toHaveTextContent("注釈の状態を取得できませんでした");
    expect(alerts[1]).toHaveTextContent("保存できませんでした");
  });

  it("詳細は畳んで出す", () => {
    const { notify } = setup();
    notify({ kind: "error", message: "m", detail: "etoki: boom" });
    const details = screen.getByText("etoki: boom").closest("details");
    expect(details).not.toHaveAttribute("open");
  });

  it("閉じるで消える。どれを閉じるかが名前で分かる", () => {
    const { notify } = setup();
    notify({ kind: "error", message: "一覧を取得できませんでした" });

    fireEvent.click(
      screen.getByRole("button", { name: "「一覧を取得できませんでした」を閉じる" }),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("action を押すと呼ばれ、通知は消える", () => {
    const { notify } = setup();
    const run = vi.fn();
    notify({
      kind: "error",
      message: "保存できませんでした",
      action: { label: "再試行", run },
    });

    fireEvent.click(screen.getByRole("button", { name: "再試行" }));
    expect(run).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // フォーカスが飛ぶと、描いている手が止まる。
  it("出てもフォーカスを奪わない", () => {
    const { notify } = setup();
    const before = document.activeElement;
    notify({ kind: "error", message: "m", action: { label: "再試行", run: () => {} } });
    expect(document.activeElement).toBe(before);
  });

  describe("自動で消える", () => {
    beforeEach(() => {
      vi.useFakeTimers();
    });

    it("info は時間が経つと消える", () => {
      const { notify } = setup();
      notify({ kind: "info", message: "i", duration: 1000 });

      act(() => {
        vi.advanceTimersByTime(999);
      });
      expect(screen.getByRole("status")).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(screen.queryByRole("status")).not.toBeInTheDocument();
    });

    it("error は時間が経っても消えない", () => {
      const { notify } = setup();
      notify({ kind: "error", message: "e" });
      act(() => {
        vi.advanceTimersByTime(60_000);
      });
      expect(screen.getByRole("alert")).toBeInTheDocument();
    });

    // 読んでいる途中で消さない。離れたら残りから再開する（最初からではない）。
    it("ホバー中は止まり、離れたら残りから再開する", () => {
      const { notify } = setup();
      notify({ kind: "info", message: "i", duration: 1000 });
      const item = screen.getByRole("status");

      act(() => {
        vi.advanceTimersByTime(600);
      });
      fireEvent.mouseEnter(item);
      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.getByRole("status")).toBeInTheDocument();

      fireEvent.mouseLeave(item);
      act(() => {
        vi.advanceTimersByTime(399);
      });
      expect(screen.getByRole("status")).toBeInTheDocument();
      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(screen.queryByRole("status")).not.toBeInTheDocument();
    });

    it("フォーカスが中にあるあいだは止まる", () => {
      const { notify } = setup();
      notify({ kind: "info", message: "i", duration: 1000 });

      fireEvent.focus(screen.getByRole("button", { name: "「i」を閉じる" }));
      act(() => {
        vi.advanceTimersByTime(5000);
      });
      expect(screen.getByRole("status")).toBeInTheDocument();
    });
  });

  // 配線を忘れた画面の失敗が、どこにも出ないまま消えるのを防ぐ。
  it("Provider の外で useNotify を呼ぶと投げる", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    function Outside() {
      useNotify();
      return null;
    }
    expect(() => render(<Outside />)).toThrow("NotificationProvider");
  });
});
