import { describe, expect, test } from "bun:test";

import {
  applyMutation,
  formatOutcomes,
  Interrupted,
  type Io,
  occurrences,
  parseSpec,
  type RunResult,
  runMutations,
  type Spec,
} from "./mutate";

describe("parseSpec", () => {
  const base = {
    cmd: ["go", "test", "./port/"],
    file: "port/board.go",
    mutations: [{ name: "上下を逆にする", edits: [{ from: ">=", to: "<=" }] }],
  };

  test("書き換えの file を省くと、最上位の file を使う", () => {
    const spec = parseSpec(base);
    expect(spec.mutations[0].edits[0].file).toBe("port/board.go");
    expect(spec.cwd).toBe(".");
  });

  test("書き換えごとの file は最上位より優先する", () => {
    const spec = parseSpec({
      ...base,
      mutations: [{ name: "x", edits: [{ file: "port/other.go", from: "a", to: "b" }] }],
    });
    expect(spec.mutations[0].edits[0].file).toBe("port/other.go");
  });

  test("前後が同じ書き換えは弾く（何も壊していないのに落ちなかったと出る）", () => {
    expect(() =>
      parseSpec({ ...base, mutations: [{ name: "x", edits: [{ from: "a", to: "a" }] }] }),
    ).toThrow("何も壊していない");
  });

  test("file がどこにも無ければ弾く", () => {
    const { file: _, ...noFile } = base;
    expect(() => parseSpec(noFile)).toThrow("file が無い");
  });

  test("壊し方が 1 つも無ければ弾く", () => {
    expect(() => parseSpec({ ...base, mutations: [] })).toThrow("1 つ以上");
  });

  test("cmd が文字列の配列でなければ弾く", () => {
    expect(() => parseSpec({ ...base, cmd: "go test ./port/" })).toThrow("cmd");
  });
});

describe("occurrences", () => {
  test("重ならずに数える", () => {
    expect(occurrences("aaaa", "aa")).toBe(2);
    expect(occurrences("abc", "x")).toBe(0);
  });
});

describe("applyMutation", () => {
  const files: Record<string, string> = {
    "a.ts": "if (ok) return;\nnext();\n",
    "b.ts": "deps: [a, a]\n",
  };
  const read = (f: string): string => files[f];

  test("ちょうど 1 回現れる from だけを書き換える", () => {
    const next = applyMutation(
      { name: "x", edits: [{ file: "a.ts", from: "if (ok) return;", to: "" }] },
      read,
    );
    expect(next.get("a.ts")).toBe("\nnext();\n");
  });

  test("from が無ければ投げる（壊せていない）", () => {
    expect(() =>
      applyMutation(
        { name: "x", edits: [{ file: "a.ts", from: "missing", to: "" }] },
        read,
      ),
    ).toThrow("0 回");
  });

  test("from が 2 回以上あれば投げる（どれを壊したか決まらない）", () => {
    expect(() =>
      applyMutation({ name: "x", edits: [{ file: "b.ts", from: "a", to: "c" }] }, read),
    ).toThrow("2 回");
  });

  test("同じファイルへの書き換えは順に当てる", () => {
    const next = applyMutation(
      {
        name: "x",
        edits: [
          { file: "a.ts", from: "if (ok)", to: "if (!ok)" },
          // 1 つ目を当てた後の中身で数える。元の中身には "!ok" が無い。
          { file: "a.ts", from: "!ok", to: "ok && false" },
        ],
      },
      read,
    );
    expect(next.get("a.ts")).toBe("if (ok && false) return;\nnext();\n");
  });

  test("to の中の $ を置換の記法として読まない", () => {
    const next = applyMutation(
      { name: "x", edits: [{ file: "a.ts", from: "next()", to: "$&$&" }] },
      read,
    );
    expect(next.get("a.ts")).toBe("if (ok) return;\n$&$&;\n");
  });
});

/**
 * メモリの上のファイルと、決めた結果を返すコマンド。走ったときの中身を控える。
 * 関数で結果を決めるときは、`stop` でこのプロセスが止められたことにできる。
 */
function fakeIo(
  files: Record<string, string>,
  results: RunResult[] | ((label: string, stop: () => void) => RunResult),
) {
  const seen: Record<string, Record<string, string>> = {};
  let n = 0;
  let stopped = false;
  const stop = (): void => {
    stopped = true;
  };
  const io: Io = {
    read: (f) => {
      if (!(f in files)) throw new Error(`無いファイル: ${f}`);
      return files[f];
    },
    write: (f, content) => {
      files[f] = content;
    },
    run: async (label) => {
      seen[label] = { ...files };
      return typeof results === "function" ? results(label, stop) : results[n++];
    },
    stopped: () => stopped,
  };
  return { io, files, seen };
}

const ok: RunResult = { status: 0, signal: null };
const fail: RunResult = { status: 1, signal: null };

const twoMutations: Spec = {
  cwd: ".",
  cmd: ["true"],
  mutations: [
    { name: "守り 1", edits: [{ file: "a.ts", from: "guard1", to: "" }] },
    {
      name: "守り 2",
      edits: [
        { file: "a.ts", from: "guard2", to: "" },
        { file: "b.ts", from: "deps", to: "nodeps" },
      ],
    },
  ],
};

describe("runMutations", () => {
  test("落ちたかどうかを壊し方ごとに返す", async () => {
    const { io } = fakeIo({ "a.ts": "guard1 guard2", "b.ts": "deps" }, [ok, fail, ok]);
    expect(await runMutations(twoMutations, io)).toEqual([
      { name: "守り 1", detected: true, status: 1 },
      { name: "守り 2", detected: false, status: 0 },
    ]);
  });

  test("走るときは壊した中身で、走った後は元に戻っている", async () => {
    const { io, files, seen } = fakeIo({ "a.ts": "guard1 guard2", "b.ts": "deps" }, [
      ok,
      fail,
      fail,
    ]);
    await runMutations(twoMutations, io);
    expect(seen.baseline).toEqual({ "a.ts": "guard1 guard2", "b.ts": "deps" });
    expect(seen["1"]).toEqual({ "a.ts": " guard2", "b.ts": "deps" });
    // 2 つ目は 1 つ目を戻した上に当てる。2 つのファイルを同時に壊す。
    expect(seen["2"]).toEqual({ "a.ts": "guard1 ", "b.ts": "nodeps" });
    expect(files).toEqual({ "a.ts": "guard1 guard2", "b.ts": "deps" });
  });

  test("コマンドが投げても元に戻す", async () => {
    const { io, files } = fakeIo({ "a.ts": "guard1 guard2", "b.ts": "deps" }, (label) => {
      if (label === "2") throw new Error("spawn に失敗");
      return label === "baseline" ? ok : fail;
    });
    await expect(runMutations(twoMutations, io)).rejects.toThrow("spawn に失敗");
    expect(files).toEqual({ "a.ts": "guard1 guard2", "b.ts": "deps" });
  });

  test("子がシグナルで止められたら、戻してから Interrupted を投げ、残りは走らせない", async () => {
    const { io, files, seen } = fakeIo({ "a.ts": "guard1 guard2", "b.ts": "deps" }, [
      ok,
      { status: null, signal: "SIGINT" },
      fail,
    ]);
    await expect(runMutations(twoMutations, io)).rejects.toThrow(Interrupted);
    expect(files).toEqual({ "a.ts": "guard1 guard2", "b.ts": "deps" });
    expect(seen["2"]).toBeUndefined();
  });

  test("子が終了コードで返しても、止められていたら「落ちた」と数えずに Interrupted を投げる", async () => {
    // vitest は 130、go test は 1 で返り、シグナルは空。終わり方だけを見ると、
    // 止めた回を「落ちた」と数えて次の壊し方へ進む。
    const { io, files, seen } = fakeIo(
      { "a.ts": "guard1 guard2", "b.ts": "deps" },
      (label, stop) => {
        if (label === "1") {
          stop();
          return fail;
        }
        return label === "baseline" ? ok : fail;
      },
    );
    await expect(runMutations(twoMutations, io)).rejects.toThrow(Interrupted);
    expect(files).toEqual({ "a.ts": "guard1 guard2", "b.ts": "deps" });
    expect(seen["2"]).toBeUndefined();
  });

  test("壊す前から落ちていたら、何も壊さずに投げる", async () => {
    const { io, seen } = fakeIo({ "a.ts": "guard1 guard2", "b.ts": "deps" }, [
      fail,
      fail,
      fail,
    ]);
    await expect(runMutations(twoMutations, io)).rejects.toThrow("壊す前から落ちている");
    expect(Object.keys(seen)).toEqual(["baseline"]);
  });

  test("当たらない書き換えが 1 つでもあれば、何も走らせずに投げる", async () => {
    const { io, seen } = fakeIo({ "a.ts": "guard1", "b.ts": "deps" }, [ok, fail, fail]);
    await expect(runMutations(twoMutations, io)).rejects.toThrow("「守り 2」");
    expect(seen).toEqual({});
  });
});

describe("formatOutcomes", () => {
  test("落ちなかったものの数を最後に出す", () => {
    const text = formatOutcomes([
      { name: "a", detected: true, status: 1 },
      { name: "b", detected: false, status: 0 },
    ]);
    expect(text).toContain("! 落ちなかった   2. b");
    expect(text).toContain("2 通りのうち 1 通りで落ちなかった");
  });

  test("すべて落ちたらそう言う", () => {
    expect(formatOutcomes([{ name: "a", detected: true, status: 1 }])).toContain(
      "1 通りすべてで落ちた",
    );
  });
});
