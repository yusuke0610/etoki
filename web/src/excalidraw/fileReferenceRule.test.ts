import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { referencedFileIds, type FileReferencing } from "./files";

/**
 * 判定対象は Go と共有する。
 *
 * **internal/domain/file_rule_test.go が同じファイルを読む。** 動かすなら両方を
 * 直す。読み方は `annotationRule.test.ts` と同じ（web/ の外にあるので fs で読む）。
 */
const rulePath = resolve(process.cwd(), "../testdata/file-reference-rule.json");

type RuleCase = {
  name: string;
  fileIds: string[];
  element: FileReferencing;
};

const rule = JSON.parse(readFileSync(rulePath, "utf8")) as { cases: RuleCase[] };

/**
 * 「どの画像を参照しているか」を共有のテストデータで固定する。
 *
 * **回帰止め。切れると何が起きるか。** 規則は TypeScript（ここ）と Go
 * （internal/domain/scene.go）の 2 箇所にある。フロントは参照している画像の
 * うちサーバーがまだ持っていないものを送り、サーバーは参照していない画像を
 * 消す（ADR 0074）。片方だけ変えると、送らなかった画像が開き直したときに
 * 欠けるか、送った画像が 400 で弾かれる。
 */
describe("referencedFileIds（Go と共有する規則）", () => {
  it("テストデータが読めている", () => {
    expect(rule.cases.length).toBeGreaterThan(0);
  });

  it.each(rule.cases)("$name", ({ element, fileIds }) => {
    expect(referencedFileIds([element])).toEqual(fileIds);
  });
});
