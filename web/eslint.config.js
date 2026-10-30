import path from "node:path";
import { fileURLToPath } from "node:url";

import js from "@eslint/js";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "node_modules"] },
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
      // 型情報を使う規則（下）のために、tsconfig.json の型を渡す。lint の対象は
      // どれも tsconfig.json の include に入っている。
      parserOptions: {
        projectService: true,
        tsconfigRootDir: path.dirname(fileURLToPath(import.meta.url)),
      },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      /*
        型情報を使う規則（#381）。**`recommendedTypeChecked` を丸ごとは入れない。**
        入れたのは、人とレビューでしか見つけられなかった形を機械で見るものだけ。

        Promise の 2 つは `.claude/rules/async-ui.md` の「`void f()` で呼ぶ非同期は、
        `f` 自身が失敗状態を更新する」を支える。付け忘れた `void` と、Promise を
        返す関数を `() => void` の口に渡す形は、どちらも失敗が誰にも受け取られずに
        消える。**`void` は「失敗は `f` の中で見せている」という宣言**なので、
        付けるなら `f` がそうなっていることを確かめてから付ける。
      */
      "@typescript-eslint/no-floating-promises": "error",
      "@typescript-eslint/no-misused-promises": "error",
      "@typescript-eslint/await-thenable": "error",
      // 型が既に言っていることを `as` で言い直さない。言い直しは型が変わった
      // ときに古いまま残り、本当に必要な `as` が埋もれる。
      "@typescript-eslint/no-unnecessary-type-assertion": "error",
      // 契約に値が増えたとき、分岐の書き漏れを tsc の前にここで見つける。
      "@typescript-eslint/switch-exhaustiveness-check": "error",
    },
  },
  /*
    形の規約はここで見る（ADR 0039）。**判断の規約は見られない**ので、そちらは
    e2e/a11y.spec.ts が持つ。

    recommended ではなく strict を採るのは、差が「recommended が既定で緩めて
    いるオプション」だけで、入れた時点の指摘が 1 件しか増えないため。緩い側を
    既定にすると、緩めてあること自体が誰にも見えないまま効き続ける。
  */
  {
    files: ["**/*.tsx"],
    ...jsxA11y.flatConfigs.strict,
  },
);
