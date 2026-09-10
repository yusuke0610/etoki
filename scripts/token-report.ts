// 1 セッションのトークン消費の内訳を出す（#125）。
//
// 消費は「context の大きさ × ターン数」で効く。API が返す usage は合計しか言わないので、
// 減らす先を決めるには「いま context に何が載っているか」が要る。Claude Code が残す
// transcript（JSONL）にはハーネスが差し込んだ本文がそのまま入っているので、そこから
// 内訳を組み立てる。
//
// bun で書いてあるのは devShell に既にあるため。jq や python を足すと、固定する道具が
// 1 つ増える（ADR 0002）。

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

/**
 * 文字数。バイト数ではなくコードポイントで数える。`wc -m` はロケールに依り、
 * C ロケールではバイト数になる（日本語は 3 倍に出る）。
 */
export const countChars = (s: string): number => Array.from(s).length;

const fmt = (n: number): string => n.toLocaleString("en-US");

/**
 * 見た目の桁。日本語は 1 文字で 2 桁ぶんを占めるので、`padEnd` に文字数を渡すと
 * 表がずれる。全角の範囲だけを 2 と数える。
 */
export const displayWidth = (s: string): number =>
  Array.from(s).reduce(
    (n, ch) => n + (/[ᄀ-ᅟ⺀-꓏가-힣豈-﫿︰-﹯＀-｠￠-￦]/.test(ch) ? 2 : 1),
    0,
  );

export const padRight = (s: string, width: number): string =>
  s + " ".repeat(Math.max(0, width - displayWidth(s)));

/** transcript の置き場所。ディレクトリ名は作業ディレクトリのパスから作られる。 */
function findSession(explicit?: string): string {
  if (explicit) return explicit;

  const root = join(homedir(), ".claude", "projects");
  const dir = join(root, process.cwd().replace(/[^a-zA-Z0-9]/g, "-"));
  if (!existsSync(dir)) {
    const known = existsSync(root) ? readdirSync(root).join("\n  ") : "(無し)";
    throw new Error(
      `transcript が見つからない: ${dir}\n` +
        `JSONL のパスを引数で渡すか、下のどれかに当たる場所で実行する:\n  ${known}`,
    );
  }

  const files = readdirSync(dir)
    .filter((name) => name.endsWith(".jsonl"))
    .map((name) => join(dir, name))
    .sort((a, b) => statSync(b).mtimeMs - statSync(a).mtimeMs);
  if (files.length === 0) throw new Error(`JSONL が 1 つも無い: ${dir}`);
  return files[0];
}

type Usage = {
  input_tokens?: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
  output_tokens?: number;
};

type Bucket = { group: string; label: string; chars: number };

/** 中身のテキストだけを数える。JSON の構文ぶんは context に載らない。 */
export function blockChars(content: unknown): number {
  if (typeof content === "string") return countChars(content);
  if (!Array.isArray(content)) return 0;

  let total = 0;
  for (const block of content) {
    if (typeof block !== "object" || block === null) continue;
    const b = block as Record<string, unknown>;
    if (typeof b.text === "string") total += countChars(b.text);
    if (typeof b.thinking === "string") total += countChars(b.thinking);
    // 道具の呼び出しは入力そのものが context に載る。
    if (b.type === "tool_use") total += countChars(JSON.stringify(b.input ?? {}));
  }
  return total;
}

/**
 * どの呼び出しかを 1 行で言えるようにする。ツール名だけだと `Bash` が並ぶだけで、
 * どの呼び出しが太いのかが分からない。
 */
export function callHint(name: string, input: unknown): string {
  const args = (input ?? {}) as Record<string, unknown>;
  const source =
    (typeof args.description === "string" && args.description) ||
    (typeof args.command === "string" && args.command) ||
    (typeof args.file_path === "string" && args.file_path) ||
    (typeof args.pattern === "string" && args.pattern) ||
    "";
  const hint = source.replace(/\s+/g, " ").slice(0, 40);
  return hint === "" ? name : `${name}: ${hint}`;
}

function main(): void {
  const path = findSession(process.argv[2]);

  const buckets = new Map<string, Bucket>();
  const add = (group: string, label: string, chars: number): void => {
    const key = `${group} ${label}`;
    const found = buckets.get(key) ?? { group, label, chars: 0 };
    found.chars += chars;
    buckets.set(key, found);
  };

  const instructionFiles = new Map<string, number>();
  const toolNames = new Map<string, string>();
  const toolHints = new Map<string, string>();
  const toolResults: { hint: string; chars: number }[] = [];
  const totals = { output: 0, created: 0, read: 0, calls: 0 };
  let lastContext = 0;
  let sessionId = "";

  for (const line of readFileSync(path, "utf8").split("\n")) {
    if (line.trim() === "") continue;

    let entry: Record<string, unknown>;
    try {
      entry = JSON.parse(line) as Record<string, unknown>;
    } catch {
      // 書き込みの途中で切れた行がありうる。読めた行だけで報告する。
      continue;
    }
    if (typeof entry.sessionId === "string") sessionId = entry.sessionId;

    if (entry.type === "attachment") {
      const attachment = (entry.attachment ?? {}) as Record<string, unknown>;
      const kind = typeof attachment.type === "string" ? attachment.type : "?";

      if (kind === "prompt_snapshot") {
        const parts = (attachment.systemPrompt ?? []) as string[];
        add(
          "ハーネス",
          "system prompt",
          parts.reduce((n, p) => n + countChars(p), 0),
        );
        continue;
      }

      // rendered が context に入る本文そのもの（system-reminder の囲みごと）。
      const rendered = (entry.rendered ?? []) as { content?: string }[];
      const chars = rendered.reduce((n, r) => n + countChars(r.content ?? ""), 0);
      if (kind !== "instructions") {
        add("ハーネス", kind, chars);
        continue;
      }

      add("リポジトリ", "CLAUDE.md 群", chars);
      for (const file of (attachment.files ?? []) as Record<string, unknown>[]) {
        const name = String(file.path ?? "?").replace(`${process.cwd()}/`, "");
        instructionFiles.set(name, countChars(String(file.content ?? "")));
      }
      continue;
    }

    if (entry.type === "assistant") {
      const message = (entry.message ?? {}) as Record<string, unknown>;
      const usage = message.usage as Usage | undefined;
      if (usage) {
        totals.calls += 1;
        totals.output += usage.output_tokens ?? 0;
        totals.created += usage.cache_creation_input_tokens ?? 0;
        totals.read += usage.cache_read_input_tokens ?? 0;
        lastContext =
          (usage.cache_read_input_tokens ?? 0) +
          (usage.cache_creation_input_tokens ?? 0) +
          (usage.input_tokens ?? 0);
      }
      for (const block of (message.content ?? []) as Record<string, unknown>[]) {
        if (block?.type === "tool_use") {
          const name = String(block.name);
          toolNames.set(String(block.id), name);
          toolHints.set(String(block.id), callHint(name, block.input));
          add("会話", "道具の呼び出し", blockChars([block]));
        } else if (block?.type === "thinking") {
          add("会話", "assistant（思考）", blockChars([block]));
        } else {
          add("会話", "assistant（応答）", blockChars([block]));
        }
      }
      continue;
    }

    if (entry.type === "user") {
      const message = (entry.message ?? {}) as Record<string, unknown>;
      const content = message.content;
      if (!Array.isArray(content)) {
        add("会話", "user", blockChars(content));
        continue;
      }
      for (const block of content as Record<string, unknown>[]) {
        if (block?.type !== "tool_result") {
          add("会話", "user", blockChars([block]));
          continue;
        }
        const id = String(block.tool_use_id);
        const name = toolNames.get(id) ?? "(不明)";
        const chars = blockChars(block.content);
        add("ツール結果", name, chars);
        toolResults.push({ hint: toolHints.get(id) ?? name, chars });
      }
    }
  }

  const rows = [...buckets.values()]
    .filter((r) => r.chars > 0)
    .sort((a, b) => b.chars - a.chars);
  const measured = rows.reduce((n, r) => n + r.chars, 0);

  console.log(`セッション ${sessionId || "(不明)"}`);
  console.log(`transcript ${path}`);
  console.log(`API 呼び出し ${fmt(totals.calls)} 回`);
  console.log("");

  console.log("トークン（API が数えたもの）");
  console.log(`  出力          ${fmt(totals.output).padStart(11)}`);
  console.log(`  cache 作成    ${fmt(totals.created).padStart(11)}`);
  console.log(`  cache 読み    ${fmt(totals.read).padStart(11)}  <- context x ターン数`);
  console.log(`  最終 context  ${fmt(lastContext).padStart(11)}`);
  console.log("");

  console.log(`context に載った文字数（計測できたぶん ${fmt(measured)} 字）`);
  for (const row of rows) {
    const share = `${((row.chars / measured) * 100).toFixed(1)}%`;
    console.log(
      `  ${padRight(row.group, 10)} ${padRight(row.label, 32)} ` +
        `${fmt(row.chars).padStart(8)} ${share.padStart(6)}`,
    );
  }
  console.log("  ツール定義は transcript に残らないので計測できていない。");
  console.log("");

  if (instructionFiles.size > 0) {
    console.log("CLAUDE.md 群の内訳");
    for (const [name, chars] of [...instructionFiles].sort((a, b) => b[1] - a[1])) {
      console.log(`  ${padRight(name, 44)} ${fmt(chars).padStart(8)}`);
    }
    console.log("");
  }

  const heaviest = [...toolResults].sort((a, b) => b.chars - a.chars).slice(0, 5);
  if (heaviest.length > 0) {
    console.log("ツール結果の重い呼び出し（単発の上位）");
    for (const r of heaviest) {
      console.log(`  ${padRight(r.hint, 52)} ${fmt(r.chars).padStart(8)}`);
    }
    console.log("");
  }

  // 削減の効き目。context は毎ターン読み直されるので、削った文字は残りターン数ぶん効く。
  const ratio = measured > 0 ? lastContext / measured : 0;
  console.log("削減の効き目");
  console.log(`  1 字 = 約 ${ratio.toFixed(2)} トークン（計測できたぶんからの概算）`);
  console.log(
    `  規約を 1,000 字削ると、同じ長さのセッション（${fmt(totals.calls)} 回）で` +
      ` 約 ${fmt(Math.round(1000 * ratio * totals.calls))} トークン減る`,
  );
}

// 直に呼ばれたときだけ走らせる。import しただけで報告が出ると、テストから
// 純関数を読めない。
if (import.meta.main) {
  try {
    main();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}
