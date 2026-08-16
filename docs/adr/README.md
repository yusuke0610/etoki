# Architecture Decision Records

設計上の判断とその理由を記録する。実装を読んでも分からない「なぜそうしたか」だけを書く。

| # | タイトル | ステータス |
|---|---|---|
| [0001](0001-dependency-direction-and-ports.md) | 依存の方向とポートの配置 | 採用 |
| [0002](0002-toolchain.md) | 開発ツールチェーンは flake.nix と Makefile に限定する | 採用 |
| [0003](0003-sqlite-and-migrations.md) | SQLite は modernc.org/sqlite、マイグレーションは embed + 自前 runner | 採用 |
| [0004](0004-single-user-local-tool.md) | 単一ユーザーのローカルツールとして設計する | 置き換え済み（0016） |
| [0005](0005-llm-client-abstraction.md) | LLMClient は 1 回の呼び出しだけを担う | 採用 |
| [0006](0006-two-level-hierarchy.md) | GitHub に作るのは epic と issue の 2 階層のみ | 採用 |
| [0007](0007-run-history.md) | マッピングは実行単位で履歴を残す | 採用 |
| [0008](0008-llm-swap-seams.md) | LLM の差し替えは 2 段の継ぎ目で受ける | 採用 |
| [0009](0009-partial-creation-is-recorded.md) | 途中まで作れた run も記録する | 採用 |
| [0010](0010-require-content-hash-for-creation.md) | 作成時に解釈時点の contentHash を必須にする | 採用 |
| [0011](0011-openapi-as-contract-ssot.md) | HTTP 契約の正本を OpenAPI に置き、Go と TypeScript の型を生成する | 採用 |
| [0012](0012-e2e-tests-mock-the-backend.md) | E2E はバックエンドを起動せず、契約の型でモックする | 採用 |
| [0013](0013-reject-cross-site-browser-requests.md) | ブラウザ由来の cross-site リクエストを Host と Origin で弾く | 採用 |
| [0014](0014-board-scoped-github-target.md) | 作成先の Projects v2 はボードごとに持ち、最初の作成で固定する | 採用 |
| [0015](0015-pluggable-auth-seams.md) | 認証は 3 つの継ぎ目で受け、GitHub App を既定実装にする | 採用 |
| [0016](0016-boards-have-owners.md) | 単一ユーザー前提を捨て、ボードに所有者を持たせる | 採用 |
| [0017](0017-board-sharing-and-permissions.md) | ボードを共有し、etoki の権限と GitHub の権限を別々に持つ | 採用 |
| [0018](0018-annotation-image-for-interpretation.md) | 注釈範囲の画像はフロントで書き出し、保存済みシーンと揃ってからだけ渡す | 採用 |
| [0019](0019-group-boards-by-target.md) | ボードは作成先でまとめて見せ、Project の表示名はスナップショットで持つ | 採用 |
| [0021](0021-link-panel-items-to-frames.md) | パネルの項目とキャンバスのフレームは選択で結ぶ | 採用 |
