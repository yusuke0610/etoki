import { describe, expect, test } from "bun:test";

import {
  blockChars,
  callHint,
  countChars,
  displayWidth,
  padRight,
  summarize,
} from "./token-report";

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

describe("summarize", () => {
  // Claude Code は 1 回の API 応答を、同じ message.id を持つ複数の assistant
  // レコードに割る。usage はどの行にも同じ値が載る。
  const usage = {
    input_tokens: 2,
    cache_creation_input_tokens: 100,
    cache_read_input_tokens: 900,
    output_tokens: 50,
  };
  const split = [
    {
      type: "assistant",
      message: { id: "msg_1", usage, content: [{ type: "text", text: "ab" }] },
    },
    {
      type: "assistant",
      message: {
        id: "msg_1",
        usage,
        content: [{ type: "tool_use", id: "t1", name: "Bash", input: {} }],
      },
    },
  ].map((e) => JSON.stringify(e));

  test("同じ message.id の usage を二重に数えない", () => {
    // 行ごとに足すと呼び出し 2 回・出力 100 になり、削減見積りまで倍に出る。
    const s = summarize(split);
    expect(s.calls).toBe(1);
    expect(s.output).toBe(50);
    expect(s.created).toBe(100);
    expect(s.read).toBe(900);
  });

  test("内訳は割られた行の両方から集める", () => {
    // usage を畳むのに合わせて content まで畳むと、道具の呼び出しが消える。
    const s = summarize(split);
    const labels = s.rows.map((r) => r.label);
    expect(labels).toContain("assistant（応答）");
    expect(labels).toContain("道具の呼び出し");
  });

  test("message.id が違えば別の呼び出しとして数える", () => {
    const other = JSON.stringify({
      type: "assistant",
      message: { id: "msg_2", usage, content: [] },
    });
    expect(summarize([...split, other]).calls).toBe(2);
  });

  test("message.id が無い行は畳まない", () => {
    // 畳むと、id を持たない別々の応答が 1 つに潰れる。
    const anonymous = JSON.stringify({
      type: "assistant",
      message: { usage, content: [] },
    });
    expect(summarize([anonymous, anonymous]).calls).toBe(2);
  });

  test("最終 context は最後の呼び出しのもの", () => {
    const last = JSON.stringify({
      type: "assistant",
      message: {
        id: "msg_2",
        usage: {
          input_tokens: 1,
          cache_creation_input_tokens: 5,
          cache_read_input_tokens: 2000,
        },
        content: [],
      },
    });
    expect(summarize([...split, last]).lastContext).toBe(2006);
  });

  test("読めない行は飛ばして続ける", () => {
    // transcript は書き込みの途中で切れることがある。
    expect(summarize(["", "{壊れた", ...split]).calls).toBe(1);
  });
});

describe("規約の数え方", () => {
  const rendered = (content: string) => [{ content }];

  test("ルートの CLAUDE.md はリポジトリに数える", () => {
    const s = summarize([
      JSON.stringify({
        type: "attachment",
        attachment: {
          type: "instructions",
          files: [{ path: "CLAUDE.md", content: "abc" }],
        },
        rendered: rendered("abcde"),
      }),
    ]);
    expect(s.rows).toEqual([{ group: "リポジトリ", label: "規約", chars: 5 }]);
    expect(s.instructionFiles).toEqual([["CLAUDE.md", 3]]);
  });

  test(".claude/rules/ もリポジトリに数える", () => {
    // rules は nested_memory という別の形で来る。ハーネスに数えると、#125 で
    // 最も増えている場所が「動かせない費用」に見える。
    const s = summarize([
      JSON.stringify({
        type: "attachment",
        attachment: {
          type: "nested_memory",
          displayPath: ".claude/rules/async-ui.md",
          path: "/repo/.claude/rules/async-ui.md",
        },
        rendered: rendered("abcd"),
      }),
    ]);
    expect(s.rows).toEqual([{ group: "リポジトリ", label: "規約", chars: 4 }]);
    expect(s.instructionFiles).toEqual([[".claude/rules/async-ui.md", 4]]);
  });

  test("ファイルの読み直しはツール結果に数える", () => {
    // 載る量を決めているのはこちらのやり方なので、ハーネスに混ぜない。
    const s = summarize([
      JSON.stringify({
        type: "attachment",
        attachment: { type: "edited_text_file", filename: "Makefile" },
        rendered: rendered("abc"),
      }),
    ]);
    expect(s.rows[0]?.group).toBe("ツール結果");
  });

  test("それ以外の差し込みはハーネスに数える", () => {
    const s = summarize([
      JSON.stringify({
        type: "attachment",
        attachment: { type: "skill_listing" },
        rendered: rendered("abc"),
      }),
    ]);
    expect(s.rows).toEqual([{ group: "ハーネス", label: "skill_listing", chars: 3 }]);
  });
});
