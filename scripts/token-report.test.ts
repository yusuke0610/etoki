import { describe, expect, test } from "bun:test";

import { blockChars, callHint, countChars, displayWidth, padRight } from "./token-report";

describe("countChars", () => {
  test("日本語を 1 文字ずつ数える（バイト数にしない）", () => {
    expect(countChars("規約")).toBe(2);
  });

  test("サロゲートペアを 1 文字と数える", () => {
    // 予算の判定に使う数え方なので、UTF-16 の単位で数えると 2 に見えてしまう。
    expect(countChars("🙂")).toBe(1);
  });
});

describe("displayWidth", () => {
  test("日本語は 2 桁、ASCII は 1 桁", () => {
    expect(displayWidth("abc")).toBe(3);
    expect(displayWidth("規約")).toBe(4);
    expect(displayWidth("規約 ok")).toBe(7);
  });
});

describe("padRight", () => {
  test("日本語混じりでも見た目の桁で揃う", () => {
    // 文字数で詰めると「ハーネス」の行だけ 4 桁ぶん右にずれる。
    expect(displayWidth(padRight("ハーネス", 12))).toBe(12);
    expect(displayWidth(padRight("harness", 12))).toBe(12);
  });

  test("桁を超える文字列は切らない", () => {
    // 切ると、どの呼び出しが太いのかを読むための手がかりが消える。
    expect(padRight("harness", 3)).toBe("harness");
  });
});

describe("blockChars", () => {
  test("文字列はそのまま数える", () => {
    expect(blockChars("abc")).toBe(3);
  });

  test("text と thinking を数える", () => {
    expect(blockChars([{ type: "text", text: "abcd" }])).toBe(4);
    expect(blockChars([{ type: "thinking", thinking: "abc" }])).toBe(3);
  });

  test("道具の呼び出しは入力そのものを数える", () => {
    // context に載るのは入力の JSON なので、名前だけでは足りない。
    expect(blockChars([{ type: "tool_use", input: { a: "bc" } }])).toBe(
      countChars(JSON.stringify({ a: "bc" })),
    );
  });

  test("読めないものは 0 にする", () => {
    expect(blockChars(undefined)).toBe(0);
    expect(blockChars([null, 42])).toBe(0);
  });
});

describe("callHint", () => {
  test("description を優先する", () => {
    expect(callHint("Bash", { description: "差分を見る", command: "git diff" })).toBe(
      "Bash: 差分を見る",
    );
  });

  test("description が無ければ command、それも無ければ file_path", () => {
    expect(callHint("Bash", { command: "git diff" })).toBe("Bash: git diff");
    expect(callHint("Read", { file_path: "CLAUDE.md" })).toBe("Read: CLAUDE.md");
  });

  test("改行を畳んで 40 字で切る", () => {
    // 1 行に収まらないと、重い呼び出しの一覧が読めなくなる。
    const hint = callHint("Bash", { command: `${"a".repeat(50)}\n${"b".repeat(50)}` });
    expect(hint).toBe(`Bash: ${"a".repeat(40)}`);
  });

  test("手がかりが無ければ名前だけ", () => {
    expect(callHint("ExitPlanMode", {})).toBe("ExitPlanMode");
    expect(callHint("ExitPlanMode", undefined)).toBe("ExitPlanMode");
  });
});
