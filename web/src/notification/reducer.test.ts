import { describe, expect, it } from "vitest";

import { MAX_NOTIFICATIONS, notificationsReducer } from "./reducer";
import type { Notification, NotifyOptions } from "./types";

function add(state: Notification[], id: number, options: NotifyOptions) {
  return notificationsReducer(state, { type: "add", id, options });
}

const error = (message: string, extra: Partial<NotifyOptions> = {}): NotifyOptions => ({
  kind: "error",
  message,
  ...extra,
});

describe("notificationsReducer", () => {
  it("新しいものを先頭に積む", () => {
    const s = add(add([], 1, error("a")), 2, error("b"));
    expect(s.map((n) => n.message)).toEqual(["b", "a"]);
  });

  // 同じ失敗の繰り返しで上限を埋めない。埋めると、別の失敗が押し出される。
  it("同じ key は積まずに置き換える", () => {
    let s = add([], 1, error("一覧 1", { key: "list" }));
    s = add(s, 2, error("保存"));
    s = add(s, 3, error("一覧 2", { key: "list" }));
    expect(s.map((n) => n.message)).toEqual(["一覧 2", "保存"]);
  });

  it("key の無い通知どうしは置き換えない", () => {
    const s = add(add([], 1, error("a")), 2, error("a"));
    expect(s).toHaveLength(2);
  });

  // #89 の 1-1: 1 本の state で後から来たものが前を消していた。上限までは
  // 両方残ることと、上限を超えたら古いほうから落ちることを両方見る。
  it("上限を超えたら古いものから落とす", () => {
    let s: Notification[] = [];
    for (let i = 1; i <= MAX_NOTIFICATIONS + 1; i++) s = add(s, i, error(`e${i}`));
    expect(s.map((n) => n.message)).toEqual(["e4", "e3", "e2"]);
  });

  it("dismiss は id で 1 件だけ消し、無い id は無視する", () => {
    const s = add(add([], 1, error("a")), 2, error("b"));
    expect(notificationsReducer(s, { type: "dismiss", id: 1 }).map((n) => n.id)).toEqual([
      2,
    ]);
    expect(notificationsReducer(s, { type: "dismiss", id: 99 })).toEqual(s);
  });

  it("dismissKey はその key の通知だけ消す", () => {
    const s = add(add([], 1, error("a", { key: "save" })), 2, error("b"));
    expect(
      notificationsReducer(s, { type: "dismissKey", key: "save" }).map((n) => n.message),
    ).toEqual(["b"]);
  });

  describe("表示時間", () => {
    it("error と warning は既定で消さない", () => {
      expect(add([], 1, error("a"))[0]?.duration).toBeNull();
      expect(add([], 1, { kind: "warning", message: "w" })[0]?.duration).toBeNull();
    });

    it("success と info は既定で消える", () => {
      expect(add([], 1, { kind: "success", message: "s" })[0]?.duration).toBe(6000);
      expect(add([], 1, { kind: "info", message: "i" })[0]?.duration).toBe(6000);
    });

    it("指定があればそれを使う", () => {
      expect(
        add([], 1, { kind: "info", message: "i", duration: 1000 })[0]?.duration,
      ).toBe(1000);
    });

    // 消える瞬間に押そうとした手が空振りする。指定より優先する。
    it("action を持つ通知は指定があっても消さない", () => {
      const s = add([], 1, {
        kind: "info",
        message: "i",
        duration: 1000,
        action: { label: "再試行", run: () => {} },
      });
      expect(s[0]?.duration).toBeNull();
    });
  });

  it("detail は省けば空文字", () => {
    expect(add([], 1, error("a"))[0]?.detail).toBe("");
  });
});
