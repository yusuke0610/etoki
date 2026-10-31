// 守りを 1 つずつ壊して、テストが落ちるかを確かめる（#402）。
//
// `.claude/rules/test-effectiveness.md` の「修正を一時的に戻してテストが落ちることを
// 確認する」を、毎回の手書きの置き換えではなく宣言で行う。手で壊すと、書き換える
// 文字列が見つからないまま回して「落ちなかった」と読む・途中で止めて壊したまま
// 残す、を繰り返した。
//
// bun で書いてあるのは devShell に既にあるため（token-report.ts と同じ、ADR 0002）。

import { spawn } from "node:child_process";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

/** 1 か所の書き換え。`from` は対象のファイルにちょうど 1 回現れなければならない。 */
export type Edit = { file: string; from: string; to: string };

/** 1 つの壊し方。複数の書き換えを 1 つとして扱う（条件と依存の配列を同時に変える、など）。 */
export type Mutation = { name: string; edits: Edit[] };

export type Spec = { cwd: string; cmd: string[]; mutations: Mutation[] };

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

const isString = (v: unknown): v is string => typeof v === "string";

/**
 * 宣言を読む。`file` は書き換えごとに書けるが、省いたら最上位の `file` を使う。
 * **書き換えの前後が同じものは弾く。** 何も壊していないのに「落ちなかった」と出る。
 */
export function parseSpec(raw: unknown): Spec {
  if (!isRecord(raw)) throw new Error("宣言はオブジェクトで書く");
  const { cmd, cwd = ".", file, mutations } = raw;
  if (!Array.isArray(cmd) || cmd.length === 0 || !cmd.every(isString)) {
    throw new Error('cmd はコマンドと引数の配列で書く（例: ["go", "test", "./port/"]）');
  }
  if (!isString(cwd)) throw new Error("cwd は文字列で書く");
  if (file !== undefined && !isString(file)) throw new Error("file は文字列で書く");
  if (!Array.isArray(mutations) || mutations.length === 0) {
    throw new Error("mutations に壊し方を 1 つ以上書く");
  }

  const parsed = mutations.map((m, i): Mutation => {
    if (!isRecord(m) || !isString(m.name) || m.name === "") {
      throw new Error(`mutations[${i}] に name を書く`);
    }
    if (!Array.isArray(m.edits) || m.edits.length === 0) {
      throw new Error(`「${m.name}」に edits を 1 つ以上書く`);
    }
    const edits = m.edits.map((e, j): Edit => {
      const where = `「${m.name}」の edits[${j}]`;
      if (!isRecord(e) || !isString(e.from) || !isString(e.to)) {
        throw new Error(`${where} に from と to を文字列で書く`);
      }
      const target = e.file ?? file;
      if (!isString(target))
        throw new Error(`${where} の file が無い（最上位の file も無い）`);
      if (e.from === "") throw new Error(`${where} の from が空`);
      if (e.from === e.to)
        throw new Error(`${where} は from と to が同じで、何も壊していない`);
      return { file: target, from: e.from, to: e.to };
    });
    return { name: m.name, edits };
  });

  return { cmd, cwd, mutations: parsed };
}

/** 重ならずに現れる回数。 */
export const occurrences = (text: string, needle: string): number =>
  text.split(needle).length - 1;

/**
 * 1 つの壊し方を、ファイルの中身に当てる。**`from` がちょうど 1 回現れなければ投げる。**
 * 0 回なら壊せていないし、2 回以上ならどれを壊したのか決まらない。同じファイルへの
 * 書き換えは順に当て、2 つ目以降はその前の書き換えを当てた後の中身で数える。
 */
export function applyMutation(
  mutation: Mutation,
  read: (file: string) => string,
): Map<string, string> {
  const next = new Map<string, string>();
  for (const edit of mutation.edits) {
    const text = next.get(edit.file) ?? read(edit.file);
    const n = occurrences(text, edit.from);
    if (n !== 1) {
      throw new Error(
        `「${mutation.name}」: ${edit.file} に ${JSON.stringify(edit.from.slice(0, 60))} が ` +
          `${n} 回ある（ちょうど 1 回でなければ壊せない）`,
      );
    }
    next.set(
      edit.file,
      text.replace(edit.from, () => edit.to),
    );
  }
  return next;
}

/** コマンドを 1 回走らせた結果。`signal` は子がシグナルで終わったとき。 */
export type RunResult = { status: number | null; signal: string | null };

export type Io = {
  read: (file: string) => string;
  write: (file: string, content: string) => void;
  /** `label` は出力の置き場所を分けるため（壊す前の 1 回は "baseline"）。 */
  run: (label: string) => Promise<RunResult>;
  /**
   * このプロセスが止められたか（Ctrl-C など）。
   *
   * **子の終わり方だけでは決めない。** 子は止められても終了コードで返すことが
   * ある（vitest は 130、go test は 1 で、シグナルは空）。それだけを見ると、止めた
   * 回を「落ちた」と数えて次の壊し方へ進み、表がすべて「落ちた」で埋まりうる。
   */
  stopped: () => boolean;
};

export type Outcome = { name: string; detected: boolean; status: number | null };

/** 止められたとき。壊したファイルは戻してから投げる。 */
export class Interrupted extends Error {}

const interrupted = (r: RunResult, io: Io): boolean =>
  io.stopped() || r.signal === "SIGINT" || r.signal === "SIGTERM";

/**
 * 壊し方を 1 つずつ当てて、コマンドが落ちるかを見る。
 *
 * - **先に全部の壊し方を当ててみる。** 書き換えが 1 つでも当たらなければ、何も
 *   走らせずに投げる。途中まで回してから宣言の誤りに気づくと、結果の表が半端になる。
 * - **壊す前に 1 回走らせて、通ることを見る。** 壊す前から落ちていると、どの壊し方も
 *   「落ちた」になる。
 * - **壊し方ごとに元に戻す。** 走らせた結果に関係なく、投げられても戻す。
 */
export async function runMutations(spec: Spec, io: Io): Promise<Outcome[]> {
  const files = new Set(spec.mutations.flatMap((m) => m.edits.map((e) => e.file)));
  const originals = new Map([...files].map((f) => [f, io.read(f)]));
  const readOriginal = (f: string): string => {
    const text = originals.get(f);
    if (text === undefined) throw new Error(`読んでいないファイル: ${f}`);
    return text;
  };

  const planned = spec.mutations.map((m) => ({
    mutation: m,
    next: applyMutation(m, readOriginal),
  }));

  const baseline = await io.run("baseline");
  if (interrupted(baseline, io)) throw new Interrupted("壊す前の 1 回で止められた");
  if (baseline.status !== 0) {
    throw new Error(
      `壊す前から落ちている（終了コード ${baseline.status}）。先に通る状態にする`,
    );
  }

  const outcomes: Outcome[] = [];
  for (const [i, { mutation, next }] of planned.entries()) {
    let result: RunResult;
    try {
      for (const [file, content] of next) io.write(file, content);
      result = await io.run(String(i + 1));
    } finally {
      for (const file of next.keys()) io.write(file, readOriginal(file));
    }
    if (interrupted(result, io))
      throw new Interrupted(`「${mutation.name}」の途中で止められた`);
    outcomes.push({
      name: mutation.name,
      detected: result.status !== 0,
      status: result.status,
    });
  }
  return outcomes;
}

/** 結果の表。落ちなかったものを先頭の印で目立たせる。 */
export function formatOutcomes(outcomes: Outcome[]): string {
  const lines = outcomes.map(
    (o, i) =>
      `${o.detected ? "  落ちた      " : "! 落ちなかった"}  ${String(i + 1).padStart(2)}. ${o.name}` +
      (o.detected ? `（終了コード ${o.status}）` : ""),
  );
  const missed = outcomes.filter((o) => !o.detected).length;
  lines.push("");
  lines.push(
    missed === 0
      ? `${outcomes.length} 通りすべてで落ちた。`
      : `${outcomes.length} 通りのうち ${missed} 通りで落ちなかった。テストを足すか、届かない理由を書く。`,
  );
  return lines.join("\n");
}

/** 宣言を読み、リポジトリのルートを基準に走らせる。出力は tmp/mutate/ に落とす。 */
async function main(): Promise<number> {
  const specPath = process.argv[2];
  if (!specPath) {
    console.error("使い方: bun scripts/mutate.ts <宣言の JSON>（make mutate SPEC=…）");
    return 2;
  }
  const root = process.cwd();
  const spec = parseSpec(JSON.parse(readFileSync(resolve(specPath), "utf8")));
  const logDir = join(root, "tmp", "mutate");
  mkdirSync(logDir, { recursive: true });

  // Ctrl-C は子と同じくこのプロセスにも届き、既定のままだと壊したファイルを戻す前に
  // 終わる。ここでは止められたことだけを覚え、子が終わるのを待ってから、戻して
  // 抜ける（runMutations の Interrupted）。
  //
  // **子は spawnSync で待たない。** 同期で待つあいだはこのハンドラが走らないので、
  // 止められたことを子の終わり方からしか知れない（Io.stopped の doc）。
  let stopped = false;
  const stop = (): void => {
    stopped = true;
  };
  process.on("SIGINT", stop);
  process.on("SIGTERM", stop);

  const io: Io = {
    read: (f) => readFileSync(join(root, f), "utf8"),
    write: (f, content) => writeFileSync(join(root, f), content),
    run: (label) =>
      new Promise((done, fail) => {
        // 出力は判断に使わないので画面に出さない。落ちた理由を確かめるときだけ開く。
        const child = spawn(spec.cmd[0], spec.cmd.slice(1), {
          cwd: join(root, spec.cwd),
          stdio: ["ignore", "pipe", "pipe"],
        });
        const output: Buffer[] = [];
        child.stdout.on("data", (chunk: Buffer) => output.push(chunk));
        child.stderr.on("data", (chunk: Buffer) => output.push(chunk));
        child.on("error", fail);
        child.on("close", (status, signal) => {
          writeFileSync(join(logDir, `${label}.log`), Buffer.concat(output));
          done({ status, signal });
        });
      }),
    stopped: () => stopped,
  };

  const outcomes = await runMutations(spec, io);
  console.log(formatOutcomes(outcomes));
  console.log(`\n出力は ${join("tmp", "mutate")}/<番号>.log`);
  return outcomes.every((o) => o.detected) ? 0 : 1;
}

// 直に呼ばれたときだけ走らせる。import しただけで走ると、テストから純関数を読めない。
if (import.meta.main) {
  main().then(
    (code) => process.exit(code),
    (error: unknown) => {
      console.error(error instanceof Error ? error.message : String(error));
      process.exit(error instanceof Interrupted ? 130 : 2);
    },
  );
}
