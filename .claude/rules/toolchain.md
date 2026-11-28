---
paths:
  # web/node_modules を拾わないための除外 3 つ。
  - "go.mod"
  - ".golangci.yml"
  - ".markdownlint-cli2.yaml"
  # 整形の対象と printWidth（yamllint を入れない理由の line-length に効く）。
  - ".prettierignore"
  - ".prettierrc.json"
  # 包み直しの印は flake.nix と flake.lock のハッシュ。lint-nix の重ね方と、
  # 整形をルートから呼ぶことは Makefile。
  - "Makefile"
  - "flake.nix"
  - "flake.lock"
  # YAML の検査を足す・変える日に触る先。
  - ".github/workflows/**"
  # Bash(gh api:*) の許可。
  - ".claude/settings.json"
---

# ツールチェーン上の非自明な設定

触る前に理由を把握しておくべきもの。消すと壊れる。**理由は設定の隣の
コメントが正本で、ここには「どこにあるか」だけを置く。** コメントを書けない
ものと、置き場所になる設定が無いものだけは、ここに理由を書く。フロント
エンド側は `web/CLAUDE.md`、生成器は `api/CLAUDE.md`。

- **`web/node_modules` を拾わないための除外 3 つ** — `go.mod` の `ignore ./web`、
  `.golangci.yml` の `exclusions.paths: ^web/`、`.markdownlint-cli2.yaml` の
  `gitignore: true`。**どれも `bun install` 後にしか再現しない**ので、
  `bun install` する前の作業ツリーでは消しても緑のまま通る。理由は各ファイルの
  コメント。
- **`.prettierignore`** と、**整形を必ずリポジトリのルートから呼ぶこと** —
  理由は `.prettierignore` と Makefile の `lint-fmt` のコメント。
- **Makefile の `ETOKI_DEVSHELL` による包み直し** — 印の値を比べる理由と、包み
  直しの中で `$(MAKE)` と書かない理由は Makefile の冒頭と `nix-develop` の
  コメント。印（`ETOKI_DEVSHELL`）の値の作り方と、`shellHook` ではなく
  derivation の環境に置く理由は `flake.nix` のコメント。
- **devShell の `GOTOOLCHAIN=local`** — 理由は `flake.nix` のコメント。
- **`nix flake check` は Nix コードのフォーマット検査のみ**（ADR 0002）。同じ
  検査を `make lint`（`lint-nix`）にも重ねている理由は Makefile のコメント。
- **YAML を見ているのは prettier と actionlint。yamllint は入れていない。**
  構文エラーと重複キーは prettier がパースに失敗して落とす。yamllint を足して
  増えるのは `document-start` のような様式の指摘だけで、`line-length` は
  prettier の `printWidth` と食い違う。ワークフロー固有の検証（式、
  コンテキスト、`run:` の中のシェル）は actionlint の担当。
- **`.claude/settings.json` の `Bash(gh api:*)` は読み取りに限定できない。**
  許可のパターンはコマンドの前方一致なので、`gh api -X POST` も同じ許可に入る。
  `pr-review` の指摘取得とスレッド解決に要るので入れているが、**書き込みも
  通す判断**であって、うっかり広いわけではない。JSON にコメントを書けないので
  ここに置いてある。
