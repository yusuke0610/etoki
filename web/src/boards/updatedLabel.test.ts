import { describe, expect, it } from "vitest";

import { updatedLabel } from "./updatedLabel";

const NOW = new Date("2026-10-03T12:00:00Z");

/** `NOW` から `ms` だけ前の時刻。 */
function ago(ms: number): string {
  return new Date(NOW.getTime() - ms).toISOString();
}

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

describe("updatedLabel", () => {
  // 境目の両側を並べる。片側だけだと、単位を 1 つずらした実装が通る。
  it.each([
    ["0 秒", "更新: たった今", 0],
    ["59 秒", "更新: たった今", 59 * SECOND],
    ["60 秒", "更新: 1 分前", 60 * SECOND],
    ["59 分", "更新: 59 分前", 59 * MINUTE],
    ["60 分", "更新: 1 時間前", 60 * MINUTE],
    ["23 時間", "更新: 23 時間前", 23 * HOUR],
    ["24 時間", "更新: 昨日", 24 * HOUR],
    ["3 日", "更新: 3 日前", 3 * DAY],
    ["6 日", "更新: 6 日前", 6 * DAY],
    ["7 日", "更新: 先週", 7 * DAY],
    ["14 日", "更新: 2 週間前", 14 * DAY],
    ["34 日", "更新: 4 週間前", 34 * DAY],
    ["35 日", "更新: 先月", 35 * DAY],
    ["90 日", "更新: 3 か月前", 90 * DAY],
    ["364 日", "更新: 12 か月前", 364 * DAY],
    ["365 日", "更新: 昨年", 365 * DAY],
    ["730 日", "更新: 2 年前", 730 * DAY],
  ])("%s 前なら「%s」", (_, want, elapsed) => {
    expect(updatedLabel(ago(elapsed), NOW)).toBe(want);
  });

  // 手元の時計が遅れていると、保存した直後のボードが未来の時刻になる。
  it("未来の時刻は「たった今」にする", () => {
    expect(updatedLabel(ago(-5 * MINUTE), NOW)).toBe("更新: たった今");
  });
});
