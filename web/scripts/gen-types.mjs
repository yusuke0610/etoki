/**
 * api/openapi.yaml から TypeScript の型定義（src/api/generated.ts）を生成する。
 *
 * 通常は `make codegen` から Go の型生成とまとめて呼ばれる。片方だけ再生成
 * すると、生成物どうしがずれたまま緑になってしまうため、単体で叩く前提には
 * していない。判断の経緯は docs/adr/0011-openapi-as-contract-ssot.md。
 */
import { writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

import openapiTS, { astToString } from "openapi-typescript";

const here = dirname(fileURLToPath(import.meta.url));
const SPEC_PATH = resolve(here, "../../api/openapi.yaml");
const OUTPUT_PATH = resolve(here, "../src/api/generated.ts");

const BANNER = `/**
 * 自動生成ファイル — 手編集禁止。
 *
 * api/openapi.yaml から openapi-typescript で生成される。再生成は \`make codegen\`。
 * 契約の正本は openapi.yaml であり、このファイルはその機械的な写し。直接編集
 * しても次の生成で上書きされ、CI の codegen-drift ジョブが落ちる（ADR 0011）。
 */
`;

// URL で渡すと相対 $ref の解決基準がファイルの場所になる。いまは 1 ファイル
// だが、分割したくなったときに壊れないようにしておく。file:// を文字列で
// 組み立てると、チェックアウト先に空白などが含まれたときに壊れる。
const ast = await openapiTS(pathToFileURL(SPEC_PATH));
writeFileSync(OUTPUT_PATH, BANNER + astToString(ast), "utf8");
console.log(`型定義を生成しました: ${OUTPUT_PATH}`);
