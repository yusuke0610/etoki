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
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: reactHooks.configs.recommended.rules,
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
