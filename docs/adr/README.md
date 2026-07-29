# Architecture Decision Records

設計上の判断とその理由を記録する。実装を読んでも分からない「なぜそうしたか」だけを書く。

| # | タイトル | ステータス |
|---|---|---|
| [0001](0001-dependency-direction-and-ports.md) | 依存の方向とポートの配置 | 採用 |
| [0002](0002-toolchain.md) | 開発ツールチェーンは flake.nix と Makefile に限定する | 採用 |
| [0003](0003-sqlite-and-migrations.md) | SQLite は modernc.org/sqlite、マイグレーションは embed + 自前 runner | 採用 |
| [0004](0004-single-user-local-tool.md) | 単一ユーザーのローカルツールとして設計する | 採用 |
| [0005](0005-llm-client-abstraction.md) | LLMClient は 1 回の呼び出しだけを担う | 採用 |
| [0006](0006-two-level-hierarchy.md) | GitHub に作るのは epic と issue の 2 階層のみ | 採用 |
