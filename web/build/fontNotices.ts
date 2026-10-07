/**
 * 画面が配るキャンバスのフォントの表示（#215）。
 *
 * フォントは `@excalidraw/excalidraw` の `dist/prod/fonts` を丸ごと写して配って
 * いる（`vite.config.ts` の `excalidrawFontAssets`）。**JavaScript として束ねて
 * いないので、Vite の `build.license` には入らない。** パッケージにはライセンスの
 * ファイルも同梱されていないので、ここに手で持つ。
 *
 * キーは `dist/prod/fonts` の下のディレクトリ名。**ライブラリを上げて家族が
 * 増えたり減ったりすると、ビルドとテストが落ちる**（`checkFontFamilies`）。
 * 表示の漏れは画面のどこにも出ないので、落として気づかせる。
 *
 * **著作権の行は推測で書かない。** 配っているファイルの name テーブル
 * （copyright）と、配布元のライセンスのファイルの両方から写した。2 つが違う
 * ものは両方を載せている。
 */

/** `build/licenses/<id>.txt` に本文を置いたライセンス。 */
export type LicenseId = "OFL-1.1" | "MIT" | "GPL-2.0-only" | "Liberation-1.x";

export type FontNotice = {
  /** 表示に出す名前。 */
  family: string;
  /** 配布元。 */
  source: string;
  copyright: string[];
  /** 並べた順に本文を付ける。 */
  licenses: LicenseId[];
  /** 補足。事実だけを書く。 */
  note?: string;
};

export const FONT_NOTICES: Record<string, FontNotice> = {
  Assistant: {
    family: "Assistant",
    source: "https://github.com/hafontia/Assistant",
    copyright: [
      "Copyright 2020 The Assistant Project Authors (https://github.com/hafontia/Assistant).",
      "Copyright 2010 The Source Sans Pro Authors (https://github.com/adobe-fonts/source-sans-pro), with Reserved Font Name 'Source'. Source is a trademark of Adobe Systems Incorporated in the United States and/or other countries.",
    ],
    licenses: ["OFL-1.1"],
  },
  Cascadia: {
    family: "Cascadia Code",
    source: "https://github.com/microsoft/cascadia-code",
    copyright: [
      "Copyright (c) 2019 - Present, Microsoft Corporation, with Reserved Font Name Cascadia Code.",
      "© 2020 Microsoft Corporation. All Rights Reserved.",
    ],
    licenses: ["OFL-1.1"],
  },
  ComicShanns: {
    family: "Comic Shanns",
    source: "https://github.com/jesusmgg/comic-shanns-mono",
    copyright: [
      "Copyright (c) 2018 Shannon Miwa",
      "Copyright (c) 2023 Jesus Gonzalez",
      "Copyright (c) 2023 Rodrigo Batista de Moraes",
      "Copyright (c) 2024 Fini Jastrow",
      "Copyright (c) 2024 Kyle Beechly",
    ],
    licenses: ["MIT"],
  },
  Excalifont: {
    family: "Excalifont",
    source: "https://plus.excalidraw.com/excalifont",
    copyright: ["Copyright (c) 2024 by Excalidraw. All rights reserved."],
    licenses: ["OFL-1.1"],
  },
  Liberation: {
    family: "Liberation Sans",
    source: "https://github.com/liberationfonts/liberation-fonts",
    copyright: [
      "Digitized data © 2007 Ascender Corporation. All rights reserved.",
      "Copyright © 2007-2011 Red Hat, Inc. All rights reserved. LIBERATION is a trademark of Red Hat, Inc.",
    ],
    licenses: ["Liberation-1.x", "GPL-2.0-only"],
    // 2.x 以降は OFL-1.1 だが、配っているのは 1.x の版。1.05 の配布物の
    // License.txt は手に入らなかったので、同じ 1.x 系の 1.07.4（Debian の
    // fonts-liberation）から写した。
    note: "This is version 1.05 of the font, which predates the move of Liberation Fonts to OFL-1.1 in 2.00. The 1.x releases are licensed under the GNU General Public License v.2 with the exceptions below.",
  },
  Lilita: {
    family: "Lilita One",
    source: "https://github.com/google/fonts/tree/main/ofl/lilitaone",
    copyright: [
      'Copyright (c) 2011 Juan Montoreano (juan@remolacha.biz), with Reserved Font Names "Lilita One"',
    ],
    licenses: ["OFL-1.1"],
  },
  Nunito: {
    family: "Nunito",
    source: "https://github.com/googlefonts/nunito",
    copyright: [
      "Copyright 2014 The Nunito Project Authors (https://github.com/googlefonts/nunito)",
    ],
    licenses: ["OFL-1.1"],
  },
  Virgil: {
    family: "Virgil",
    source: "https://github.com/excalidraw/virgil",
    copyright: [
      "Copyright (c) 2021 - Present, Ellinor Rapp, with Reserved Font Name Virgil.",
      "Copyright (c) 2011 by Your Own Font Foundry. All rights reserved.",
    ],
    licenses: ["OFL-1.1"],
  },
  Xiaolai: {
    family: "Xiaolai SC",
    source: "https://github.com/lxgw/kose-font",
    copyright: [
      "Copyright 2020-2024 LXGW (https://github.com/lxgw/kose-font)",
      "Copyright 2014 Nozomi Seto (https://ja.osdn.net/projects/setofont/)",
    ],
    licenses: ["OFL-1.1"],
  },
};
