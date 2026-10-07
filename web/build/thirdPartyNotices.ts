import { existsSync, readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import type { Plugin } from "vite";

import { FONT_NOTICES, type FontNotice, type LicenseId } from "./fontNotices.ts";
import { PACKAGE_NOTICES, type PackageNotice } from "./packageNotices.ts";

/**
 * 同梱したものの表示を書き出す先。`web/dist` の直下に置く（ADR 0070）。
 *
 * Vite の既定（`.vite/license.md`）にしないのは、ドットで始まる場所は配信の
 * 仕方によって届かないため。etoki 自身（`http.Dir`）は配るが、`web/dist` を
 * 別の配信元に載せたときに表示だけが落ちる。`.txt` にするのは、Markdown の
 * Content-Type は配信元によって決まらず、ブラウザがその場で開かないことがある
 * ため。中身は Markdown で書く。
 */
export const NOTICES_FILE = "third-party-notices.txt";

/**
 * Vite の `build.license` に書かせる中間のファイル。`.json` で終わると、
 * Markdown ではなく集めた生の値を書く。読んだら成果物からは外す。
 */
const PACKAGES_JSON = "third-party-packages.json";

/** ライセンスの本文の置き場所。`build/licenses/<id>.txt`。 */
export function licenseTextPath(id: LicenseId): string {
  // new URL(相対, import.meta.url) にしない。vitest の jsdom では URL が
  // 差し替わっていて、file: を基準にした解決が http://localhost に化ける。
  return path.join(path.dirname(fileURLToPath(import.meta.url)), "licenses", `${id}.txt`);
}

/**
 * ファイルの先頭に並んだコメントを返す。行コメントは 1 行ずつ返す。
 *
 * **見るのは先頭だけ。** 写したものの表示はファイルの頭に置く慣習で、途中の
 * コメントまで見るには文字列やテンプレートの中を区別する字句解析が要る。
 * 行コメントも返すのは、表示が行コメントで書かれたときに見逃さないため。
 * `eslint-disable` のような指示も来るので、どれを咎めるかは呼ぶ側が決める。
 */
export function leadingComments(code: string): string[] {
  const out: string[] = [];
  let i = 0;

  for (;;) {
    while (i < code.length && /\s/.test(code[i] ?? "")) i++;

    if (code.startsWith("//", i)) {
      const eol = code.indexOf("\n", i);
      const end = eol === -1 ? code.length : eol;
      out.push(code.slice(i, end).trimEnd());
      i = end;
      continue;
    }

    if (code.startsWith("/*", i)) {
      const end = code.indexOf("*/", i + 2);
      if (end === -1) break;
      out.push(code.slice(i, end + 2));
      i = end + 2;
      continue;
    }

    break;
  }

  return out;
}

/**
 * 表示の印が付いたコメントか。esbuild / rolldown が「法的なコメント」と見なす
 * 形に揃える（`/*!` か `//!` で始まるか、`@license` / `@preserve` を含む）。
 */
function isLegalComment(comment: string): boolean {
  return /^\/[*/]!/.test(comment) || /@license|@preserve/.test(comment);
}

/** 著作権の表示を含むか。`Copyright` と書かず `©` だけで書く表示もある。 */
function mentionsCopyright(comment: string): boolean {
  return /copyright|©/i.test(comment);
}

/**
 * ソースの先頭から、写したものの表示を取り出す。表示として拾うのは、印の
 * 付いたブロックコメントだけ。
 *
 * `unmarked` は、表示を含むのに書き出せないコメント。印の無いブロック
 * コメントのうち著作権の表示を含むものと、行コメントのうち著作権の表示か
 * 印を含むもの。**表示が成果物から黙って落ちる形なので、プラグインはこれを
 * 見つけたらビルドを止める。** 説明と表示を 1 つのコメントにまとめると、説明
 * まで表示として書き出すことになるので、表示だけを別のコメントに分けてもらう。
 * 行コメントを表示として拾わないのは、複数行の表示が 1 行ずつに割れるため。
 */
export function sourceNotices(code: string): { notices: string[]; unmarked: string[] } {
  const notices: string[] = [];
  const unmarked: string[] = [];

  for (const c of leadingComments(code)) {
    const block = c.startsWith("/*");
    if (block && isLegalComment(c)) notices.push(c);
    else if (mentionsCopyright(c) || isLegalComment(c)) unmarked.push(c);
  }

  return { notices, unmarked };
}

/** コメントの区切りと行頭の `*` を外し、本文だけにする。 */
export function commentBody(comment: string): string {
  return comment
    .replace(/^\/\*[*!]?/, "")
    .replace(/\*\/$/, "")
    .split("\n")
    .map((line) => line.replace(/^\s*\* ?/, "").trimEnd())
    .join("\n")
    .trim();
}

/**
 * 配っているフォントの家族と、表示を持っている家族を突き合わせる。
 *
 * `stale` も返す。配っていないフォントの表示が残っていても害は無いが、
 * ライブラリを上げて家族が入れ替わったことに気づけない。
 */
export function checkFontFamilies(
  served: string[],
  known: string[],
): { missing: string[]; stale: string[] } {
  return {
    missing: served.filter((f) => !known.includes(f)).sort(),
    stale: known.filter((f) => !served.includes(f)).sort(),
  };
}

/** Vite の `build.license` が `.json` に書く 1 件（`vite:license` の形）。 */
export type LicenseEntry = {
  name: string;
  version: string;
  identifier?: string;
  text?: string;
};

export type ResolvedPackage = {
  entry: LicenseEntry;
  /** 本文がパッケージに無く、`PACKAGE_NOTICES` から補ったもの。 */
  supplement?: PackageNotice;
};

/** `@scope/name` なら、スコープ全体を指すキー `@scope/*` も引く。 */
function lookupNotice(
  name: string,
  notices: Record<string, PackageNotice>,
): [string, PackageNotice] | undefined {
  const keys = name.startsWith("@") ? [name, `${name.split("/")[0]}/*`] : [name];
  for (const k of keys) {
    const n = notices[k];
    if (n) return [k, n];
  }
  return undefined;
}

/**
 * 束ねたパッケージに、本文の無いものの表示を補う。
 *
 * - `missing`: 本文が無く、補う表示も無い
 * - `mismatched`: 補う表示はあるが、パッケージの `license` と食い違う
 * - `stale`: どのパッケージにも使われなかった補い
 *
 * どれかがあればプラグインはビルドを止める。`stale` は、配布元がライセンスの
 * ファイルを同梱し始めたか、依存から外れたことの知らせ。
 */
export function resolvePackages(
  entries: LicenseEntry[],
  notices: Record<string, PackageNotice>,
): {
  packages: ResolvedPackage[];
  missing: string[];
  mismatched: string[];
  stale: string[];
} {
  const packages: ResolvedPackage[] = [];
  const missing: string[] = [];
  const mismatched: string[] = [];
  const used = new Set<string>();

  for (const entry of entries) {
    if (entry.text) {
      packages.push({ entry });
      continue;
    }

    const found = lookupNotice(entry.name, notices);
    if (!found) {
      missing.push(entry.name);
      continue;
    }

    const [key, supplement] = found;
    used.add(key);
    if (!supplement.licenses.some((id) => id === entry.identifier)) {
      mismatched.push(`${entry.name} (${entry.identifier ?? "no license field"})`);
      continue;
    }
    packages.push({ entry, supplement });
  }

  const stale = Object.keys(notices).filter((k) => !used.has(k));
  return { packages, missing, mismatched, stale };
}

export type CopiedNotice = {
  /** `web/` からの相対パス。 */
  file: string;
  /** コメントの区切りを外した本文。 */
  notice: string;
};

/**
 * 表示のファイルを組み立てる。
 *
 * **ライセンスの本文は、補ったものの分を最後にまとめて 1 度ずつ置く。** 同じ
 * ライセンスのフォントやパッケージがいくつもあり、1 件ずつ並べると同じ本文が
 * その数だけ出る。パッケージが自分で持っていた本文はそのまま置く（著作権の行と
 * 条文が 1 つになっているので、分けられない）。
 */
export function renderNotices(input: {
  packages: ResolvedPackage[];
  fonts: FontNotice[];
  copied: CopiedNotice[];
  licenseText: (id: LicenseId) => string;
}): string {
  const parts: string[] = [
    "# Third-party notices",
    "The etoki web UI includes the following third-party material.",
  ];

  if (input.packages.length > 0) {
    parts.push("# Bundled packages");
    for (const { entry, supplement } of input.packages) {
      const id = entry.identifier ? ` (${entry.identifier})` : "";
      parts.push(`## ${entry.name} - ${entry.version}${id}`);
      if (supplement) {
        parts.push(`Source: ${supplement.source}`, supplement.copyright.join("\n"));
      } else if (entry.text) {
        parts.push(entry.text.trim());
      }
    }
  }

  if (input.fonts.length > 0) {
    parts.push(
      "# Fonts",
      "The app also serves the following fonts, copied unmodified from the @excalidraw/excalidraw package.",
    );
    for (const f of input.fonts) {
      parts.push(`## ${f.family} (${f.licenses.join(", ")})`, `Source: ${f.source}`);
      parts.push(f.copyright.join("\n"));
      if (f.note) parts.push(f.note);
    }
  }

  if (input.copied.length > 0) {
    parts.push(
      "# Copied into the source",
      "The following files of the app contain material copied from third parties.",
    );
    for (const c of input.copied) {
      parts.push(`## ${c.file}`, c.notice);
    }
  }

  const ids = [
    ...new Set([
      ...input.packages.flatMap((p) => p.supplement?.licenses ?? []),
      ...input.fonts.flatMap((f) => f.licenses),
    ]),
  ];
  if (ids.length > 0) {
    parts.push(
      "# License texts",
      "Full texts of the licenses referred to by name above.",
    );
    for (const id of ids) {
      parts.push(`## ${id}`, input.licenseText(id).trim());
    }
  }

  return `${parts.join("\n\n")}\n`;
}

/**
 * 同梱したもののライセンスの表示を、`web/dist` に 1 つのファイルで残す（#215）。
 *
 * 口は 3 つあり、どれも同じファイルに書く。
 *
 * 1. **束ねた依存** — Vite の `build.license`。`node_modules` から束ねた
 *    パッケージのライセンスを集める。本文を同梱していないパッケージは
 *    `PACKAGE_NOTICES` で補う。縮小でライセンスのコメントが落ちる
 *    （`comments.legal` は縮小時に既定で false）が、コメントを残す案は
 *    採らない。持っていないパッケージもあり、チャンクのあちこちに散る。
 * 2. **フォント** — `FONT_NOTICES`。JavaScript として束ねていないので 1 に
 *    入らない。配っている家族と食い違ったらビルドを止める。
 * 3. **ソースに写したもの** — 束ねたモジュールのうち `web/` の中にあるものの
 *    先頭の表示（`sourceNotices`）。**一覧を別に持たない。** 写した人が表示を
 *    そのファイルに置けば、それだけで載る。書き忘れる場所を増やさない。
 *
 * **表示が欠けたら、ビルドを止める。** 欠けても画面のどこにも出ず、気づく
 * 機会が無い。
 *
 * `build.license` の指定をここで持つのは、書かせる口と読む口を 1 か所に置く
 * ため。`vite.config.ts` 側で別に指定すると、片方だけ変えた日に読む先が見つから
 * なくなる（そのときもビルドが落ちる）。
 */
export function thirdPartyNotices(options: { fontsDir: string }): Plugin {
  let root = "";

  return {
    name: "etoki:third-party-notices",
    apply: "build",

    config() {
      return { build: { license: { fileName: PACKAGES_JSON } } };
    },

    configResolved(config) {
      root = config.root;
    },

    generateBundle: {
      // Vite の `vite:license` は順序を指定していないので、post にすれば
      // そちらが書いた後に走る。
      order: "post",
      handler(_, bundle) {
        const asset = bundle[PACKAGES_JSON];
        if (asset?.type !== "asset") {
          this.error(`${PACKAGES_JSON} was not emitted. Is build.license overridden?`);
        }
        const raw =
          typeof asset.source === "string"
            ? asset.source
            : new TextDecoder().decode(asset.source);
        delete bundle[PACKAGES_JSON];

        const { packages, missing, mismatched, stale } = resolvePackages(
          JSON.parse(raw) as LicenseEntry[],
          PACKAGE_NOTICES,
        );
        if (missing.length > 0 || mismatched.length > 0 || stale.length > 0) {
          this.error(
            "package notices are incomplete: " +
              `no license text [${missing.join(", ")}], ` +
              `license mismatch [${mismatched.join(", ")}], ` +
              `unused [${stale.join(", ")}]. Update web/build/packageNotices.ts.`,
          );
        }

        const families = readdirSync(options.fontsDir, { withFileTypes: true })
          .filter((d) => d.isDirectory())
          .map((d) => d.name)
          .sort();
        const fontCheck = checkFontFamilies(families, Object.keys(FONT_NOTICES));
        if (fontCheck.missing.length > 0 || fontCheck.stale.length > 0) {
          this.error(
            `font notices are out of sync with ${options.fontsDir}: ` +
              `missing [${fontCheck.missing.join(", ")}], ` +
              `stale [${fontCheck.stale.join(", ")}]. Update web/build/fontNotices.ts.`,
          );
        }

        const moduleIds = new Set<string>();
        for (const output of Object.values(bundle)) {
          if (output.type === "chunk")
            output.moduleIds.forEach((id) => moduleIds.add(id));
        }

        const copied: CopiedNotice[] = [];
        for (const id of moduleIds) {
          const file = id.split("?")[0] ?? "";
          if (
            id.startsWith("\0") ||
            !file.startsWith(root + path.sep) ||
            file.includes(`${path.sep}node_modules${path.sep}`) ||
            !existsSync(file)
          ) {
            continue;
          }

          const rel = path.relative(root, file);
          const { notices, unmarked } = sourceNotices(readFileSync(file, "utf8"));
          if (unmarked.length > 0) {
            this.error(
              `${rel}: a leading comment carries a copyright or license notice but is not ` +
                "a block comment marked as one. Put the notice in its own block comment " +
                "starting with /*! or containing @license, so that it reaches the build output.",
            );
          }
          for (const n of notices) copied.push({ file: rel, notice: commentBody(n) });
        }
        copied.sort((a, b) => a.file.localeCompare(b.file));

        this.emitFile({
          type: "asset",
          fileName: NOTICES_FILE,
          source: renderNotices({
            packages,
            fonts: families.flatMap((f) => FONT_NOTICES[f] ?? []),
            copied,
            licenseText: (id) => readFileSync(licenseTextPath(id), "utf8"),
          }),
        });
      },
    },
  };
}
