import type { LicenseId } from "./fontNotices.ts";

/**
 * npm の配布物にライセンスのファイルを同梱していないパッケージの表示（#215）。
 *
 * Vite の `build.license` は、パッケージの中にある `LICENSE` などのファイルから
 * 本文を写す。**無ければ名前と識別子（`MIT` など）だけになる。** MIT が求める
 * 著作権表示が成果物に残らないので、配布元のリポジトリのライセンスのファイルから
 * 写したものをここに持つ。
 *
 * キーはパッケージ名か、スコープ全体を指す `@scope/*`。**本文の無いパッケージが
 * ここにも無ければビルドが落ちる**（`resolvePackages`）。依存を足した日や
 * 上げた日に、表示の欠けたパッケージが黙って入らないようにするため。
 */
export type PackageNotice = {
  source: string;
  copyright: string[];
  /**
   * パッケージの `license` がこのどれかでなければビルドを止める。配布元が
   * ライセンスを変えたのに、古い表示を出し続けないようにするため。
   */
  licenses: LicenseId[];
};

export const PACKAGE_NOTICES: Record<string, PackageNotice> = {
  "@excalidraw/excalidraw": {
    source: "https://github.com/excalidraw/excalidraw",
    copyright: ["Copyright (c) 2020 Excalidraw"],
    licenses: ["MIT"],
  },
  "@radix-ui/*": {
    source: "https://github.com/radix-ui/primitives",
    copyright: ["Copyright (c) 2022 WorkOS"],
    licenses: ["MIT"],
  },
  "react-remove-scroll-bar": {
    source: "https://github.com/theKashey/react-remove-scroll-bar",
    copyright: ["Copyright (c) 2025 Anton Korzunov <thekashey@gmail.com>"],
    licenses: ["MIT"],
  },
};
