import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { isAnnotation, type SceneElement } from "./annotation";

/**
 * 判定対象は Go と共有する。
 *
 * **internal/domain/annotation_rule_test.go が同じファイルを読む。** 動かすなら
 * 両方を直す。バンドラの解決を挟まず fs で読むのは、リポジトリのルート側に
 * ある（web/ の外の）ファイルだから。vitest は web/ を root に走るので、
 * そこからの相対で解く。
 */
const rulePath = resolve(process.cwd(), "../testdata/annotation-rule.json");

type RuleCase = {
  name: string;
  isAnnotation: boolean;
  element: SceneElement;
};

const rule = JSON.parse(readFileSync(rulePath, "utf8")) as { cases: RuleCase[] };

/**
 * 注釈の判定規則を共有のテストデータで固定する。
 *
 * **回帰止め。切れると何が起きるか。** 判定規則は TypeScript（ここ）と Go
 * （internal/domain/scene.go）の 2 箇所にあり、構造上まとめられない。片方だけ
 * 変えると、画面が注釈として出すものとサーバーが解釈の対象にするものがずれる。
 * どちらも同じ規則のつもりなので、**ずれに気づくのは開発者ではなく利用者になる。**
 */
describe("isAnnotation（Go と共有する規則）", () => {
  it("テストデータが読めている", () => {
    expect(rule.cases.length).toBeGreaterThan(0);
  });

  it.each(rule.cases)("$name", ({ element, isAnnotation: want }) => {
    expect(isAnnotation(element)).toBe(want);
  });
});
