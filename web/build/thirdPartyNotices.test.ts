import { existsSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { FONT_NOTICES, type FontNotice } from "./fontNotices.ts";
import { PACKAGE_NOTICES, type PackageNotice } from "./packageNotices.ts";
import {
  checkFontFamilies,
  commentBody,
  leadingComments,
  licenseTextPath,
  renderNotices,
  resolvePackages,
  sourceNotices,
} from "./thirdPartyNotices.ts";

describe("leadingComments", () => {
  it("ファイルの先頭に並んだコメントを返す。行コメントは 1 行ずつ", () => {
    const code = [
      "// eslint-disable-next-line",
      "/* 1 つ目 */",
      "",
      "/*! 2 つ目 */",
      "// 3 つ目",
      'import x from "x";',
      "/* コードの後ろは読まない */",
    ].join("\n");

    expect(leadingComments(code)).toEqual([
      "// eslint-disable-next-line",
      "/* 1 つ目 */",
      "/*! 2 つ目 */",
      "// 3 つ目",
    ]);
  });

  it("改行で終わらない行コメントも拾う", () => {
    expect(leadingComments("// 最後の行")).toEqual(["// 最後の行"]);
  });

  it("閉じていないコメントは拾わない", () => {
    expect(leadingComments("/* 閉じない")).toEqual([]);
  });

  it("コメントが無ければ空", () => {
    expect(leadingComments('import x from "x";')).toEqual([]);
  });
});

describe("sourceNotices", () => {
  it("`/*!` で始まるか、`@license` / `@preserve` を含むコメントを表示として拾う", () => {
    const code = [
      "/* 説明 */",
      "/*! Foo — MIT */",
      "/**",
      " * @license",
      " * Copyright (c) 2026 Bar",
      " */",
      "/* @preserve Baz — ISC */",
      "export {};",
    ].join("\n");

    expect(sourceNotices(code)).toEqual({
      notices: [
        "/*! Foo — MIT */",
        "/**\n * @license\n * Copyright (c) 2026 Bar\n */",
        "/* @preserve Baz — ISC */",
      ],
      unmarked: [],
    });
  });

  // 写した側が印を付け忘れると、表示が成果物から黙って落ちる（#214 の形）。
  // 落ちたことは画面のどこにも出ないので、見つけたら止める。
  it("印の無いコメントに著作権の表示があれば unmarked に返す", () => {
    const code = "/*\n * 説明\n *\n * Copyright (c) 2020 Someone\n */\nexport {};";

    expect(sourceNotices(code)).toEqual({
      notices: [],
      unmarked: [code.split("\nexport")[0]],
    });
  });

  it("`©` だけの表示も著作権の表示として扱う", () => {
    expect(sourceNotices("/* © 2020 Example */\nexport {};")).toEqual({
      notices: [],
      unmarked: ["/* © 2020 Example */"],
    });
  });

  // 書き出せるのはブロックコメントの本文だけ。行コメントに置いた表示は、
  // 印があっても無くても成果物に届かないので、ブロックコメントに移してもらう。
  it("行コメントに置いた表示は、印があっても unmarked に返す", () => {
    const code = [
      "// eslint-disable-next-line",
      "// Copyright (c) 2020 Someone",
      "//! Foo — MIT",
      "// @license MIT",
      "export {};",
    ].join("\n");

    expect(sourceNotices(code)).toEqual({
      notices: [],
      unmarked: ["// Copyright (c) 2020 Someone", "//! Foo — MIT", "// @license MIT"],
    });
  });
});

describe("commentBody", () => {
  it("区切りと行頭の `*` を外す", () => {
    expect(commentBody("/*!\n * Foo\n *\n * Bar\n */")).toBe("Foo\n\nBar");
    expect(commentBody("/** @license MIT */")).toBe("@license MIT");
  });
});

describe("checkFontFamilies", () => {
  it("表示の無い家族と、配っていない家族の表示を返す", () => {
    expect(checkFontFamilies(["A", "B", "C"], ["B", "C", "D"])).toEqual({
      missing: ["A"],
      stale: ["D"],
    });
  });

  // ライブラリを上げてフォントの家族が変わったら、ここで気づく。
  it("同梱のフォントの家族と FONT_NOTICES が一致する", () => {
    // new URL(相対, import.meta.url) にしない理由は licenseTextPath と同じ。
    const fontsDir = path.join(
      path.dirname(fileURLToPath(import.meta.url)),
      "../node_modules/@excalidraw/excalidraw/dist/prod/fonts",
    );
    const families = readdirSync(fontsDir, { withFileTypes: true })
      .filter((d) => d.isDirectory())
      .map((d) => d.name);

    expect(checkFontFamilies(families, Object.keys(FONT_NOTICES))).toEqual({
      missing: [],
      stale: [],
    });
  });
});

describe("resolvePackages", () => {
  const mit: PackageNotice = {
    source: "https://example.com/radix",
    copyright: ["Copyright (c) 2022 WorkOS"],
    licenses: ["MIT"],
  };

  it("本文を持つパッケージはそのまま、持たないものはスコープのキーでも補う", () => {
    const got = resolvePackages(
      [
        { name: "react", version: "18.3.1", identifier: "MIT", text: "MIT License ..." },
        { name: "@radix-ui/react-tabs", version: "1.0.2", identifier: "MIT" },
      ],
      { "@radix-ui/*": mit },
    );

    expect(got).toEqual({
      packages: [
        {
          entry: {
            name: "react",
            version: "18.3.1",
            identifier: "MIT",
            text: "MIT License ...",
          },
        },
        {
          entry: { name: "@radix-ui/react-tabs", version: "1.0.2", identifier: "MIT" },
          supplement: mit,
        },
      ],
      missing: [],
      mismatched: [],
      stale: [],
    });
  });

  // 本文の無いパッケージが黙って入ると、MIT の著作権表示が成果物から欠ける。
  it("本文も補いも無いパッケージを missing に返す", () => {
    expect(
      resolvePackages([{ name: "left-pad", version: "1.0.0", identifier: "MIT" }], {}),
    ).toMatchObject({ packages: [], missing: ["left-pad"] });
  });

  it("配布元がライセンスを変えたら mismatched に返し、古い表示を出さない", () => {
    expect(
      resolvePackages(
        [{ name: "@radix-ui/react-id", version: "2.0.0", identifier: "Apache-2.0" }],
        {
          "@radix-ui/*": mit,
        },
      ),
    ).toMatchObject({ packages: [], mismatched: ["@radix-ui/react-id (Apache-2.0)"] });
  });

  it("使われなかった補いを stale に返す", () => {
    expect(resolvePackages([], { "@radix-ui/*": mit })).toMatchObject({
      stale: ["@radix-ui/*"],
    });
  });
});

describe("licenseTextPath", () => {
  it("FONT_NOTICES と PACKAGE_NOTICES が使うライセンスの本文がすべて置いてある", () => {
    const ids = new Set(
      [...Object.values(FONT_NOTICES), ...Object.values(PACKAGE_NOTICES)].flatMap(
        (f) => f.licenses,
      ),
    );

    for (const id of ids) {
      expect(existsSync(licenseTextPath(id)), id).toBe(true);
    }
  });
});

describe("renderNotices", () => {
  const ofl: FontNotice = {
    family: "Alpha",
    source: "https://example.com/alpha",
    copyright: ["Copyright 2020 Alpha Authors"],
    licenses: ["OFL-1.1"],
  };
  const texts: Record<string, string> = { "OFL-1.1": "OFL TEXT", MIT: "MIT TEXT" };
  const licenseText = (id: string) => texts[id] ?? "";

  it("フォントごとの著作権と、ライセンスの本文を 1 度ずつ並べる", () => {
    const out = renderNotices({
      packages: [],
      fonts: [ofl, { ...ofl, family: "Beta", copyright: ["Copyright 2021 Beta"] }],
      copied: [],
      licenseText,
    });

    expect(out).toContain("## Alpha (OFL-1.1)");
    expect(out).toContain("Copyright 2020 Alpha Authors");
    expect(out).toContain("## Beta (OFL-1.1)");
    expect(out.match(/OFL TEXT/g)).toHaveLength(1);
    expect(out).not.toContain("MIT TEXT");
  });

  it("パッケージの本文はそのまま置き、補ったものは著作権だけ置いて本文を末尾に回す", () => {
    const out = renderNotices({
      packages: [
        {
          entry: {
            name: "react",
            version: "18.3.1",
            identifier: "MIT",
            text: "REACT LICENSE",
          },
        },
        {
          entry: { name: "@radix-ui/react-tabs", version: "1.0.2", identifier: "MIT" },
          supplement: {
            source: "https://example.com",
            copyright: ["Copyright (c) 2022 WorkOS"],
            licenses: ["MIT"],
          },
        },
        {
          entry: { name: "@radix-ui/react-id", version: "1.1.0", identifier: "MIT" },
          supplement: {
            source: "https://example.com",
            copyright: ["Copyright (c) 2022 WorkOS"],
            licenses: ["MIT"],
          },
        },
      ],
      fonts: [],
      copied: [],
      licenseText,
    });

    expect(out).toContain("## react - 18.3.1 (MIT)\n\nREACT LICENSE");
    expect(out).toContain("## @radix-ui/react-tabs - 1.0.2 (MIT)");
    expect(out.match(/Copyright \(c\) 2022 WorkOS/g)).toHaveLength(2);
    expect(out.match(/MIT TEXT/g)).toHaveLength(1);
    expect(out.indexOf("MIT TEXT")).toBeGreaterThan(out.indexOf("# License texts"));
  });

  it("写したソースの表示をファイルごとに並べ、無ければ節ごと出さない", () => {
    const withCopied = renderNotices({
      packages: [],
      fonts: [],
      copied: [
        { file: "src/board/menuIcons.tsx", notice: "Tabler Icons\n\nMIT License" },
      ],
      licenseText,
    });
    expect(withCopied).toContain("## src/board/menuIcons.tsx");
    expect(withCopied).toContain("Tabler Icons\n\nMIT License");

    const without = renderNotices({ packages: [], fonts: [], copied: [], licenseText });
    expect(without).not.toContain("Copied into the source");
    expect(without).not.toContain("# License texts");
  });

  it("補足があれば著作権の後ろに添える", () => {
    const out = renderNotices({
      packages: [],
      fonts: [{ ...ofl, note: "Older version." }],
      copied: [],
      licenseText,
    });

    expect(out.indexOf("Older version.")).toBeGreaterThan(out.indexOf("Copyright 2020"));
  });
});
