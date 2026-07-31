# etoki

ホワイトボード上のブレスト内容を Vision LLM に解釈させ、GitHub Projects v2 の
draft issue に変換するツール。

## 中核となる思想

設計判断で迷ったらここに立ち返る。

1. **ユーザーはブレスト中に構造を意識してはならない。** issue / epic といった
   概念をブレストフェーズに持ち込まない。構造化の責務はツール側と開発者
   フェーズに寄せる。
2. **構造は座標から推測せず、LLM に解釈させる。** 付箋の空間的近接やコネクタ
   から構造をルールベースで推測する実装は行わない。
3. **システムは自動で判断せず、状態を見せて開発者に選ばせる。** 自動同期・
   自動更新・自動作成は実装しない。すべて開発者の手動トリガー。

## ブランチ運用

**必ず `main` を起点にブランチを切って作業する。`main` に直接コミットしない。**

```sh
git switch main
git pull
git switch -c <prefix>/<短い説明>
```

`<prefix>` はコミットの種別に合わせる（`feat` / `fix` / `docs` / `build` / `ci`
/ `chore` / `refactor` / `test`）。

作業が終わったらブランチを push する。`main` への反映は Pull Request 経由。

## コミット

- 1 コミットの粒度を小さく保つ。
- Conventional Commits 形式（`feat(scope): 要約`）。要約は英語、本文は日本語。
- **本文には「何をしたか」ではなく「なぜそうしたか」を書く。** 差分を読めば
  分かることは書かない。
- 常に緑のコミットにする。テストだけ先にコミットして赤い状態を履歴に残さない。

## 開発コマンド

[Nix](https://nixos.org/) 以外はインストール不要。**README にグローバル
インストールの手順を書かないこと。**

```sh
nix develop      # 開発シェル（Go / Bun / SQLite / golangci-lint / air）
make help        # ターゲット一覧
make setup       # 依存取得と DB 初期化
make dev         # バックエンドとフロントエンドを同時起動
make lint        # golangci-lint + eslint + tsc
make test        # go test + vitest
```

コミット前に `make lint` と `make test` を通す。

## アーキテクチャ

依存は必ず内向き。ユースケース層がクラウド SDK や GitHub SDK の型を直接
参照してはならない。

```
フロントエンド → Gin ハンドラ → ユースケース層 → port のインターフェース → アダプタ実装
```

| パス | 役割 |
| --- | --- |
| `port/` | **公開 API。** インターフェースと境界 DTO。`internal/` に依存しない |
| `etoki.go` | **公開 API。** サーバーの組み立てと起動 |
| `internal/domain/` | エンティティと純粋ロジック（ハッシュ算出、3 状態判定） |
| `internal/usecase/` | ユースケース層 |
| `internal/adapter/` | LLM / GitHub / SQLite のアダプタ実装 |
| `internal/httpapi/` | Gin ハンドラとルーティング |
| `prompts/`, `examples/` | プロンプトと few-shot 例 |

**`port/` と `etoki.go` が `internal/` の外にあるのは意図的。** 外部リポジトリが
`LLMClient` を実装して差し込むには、`internal/` では import できないため。
詳細は `docs/adr/0001-dependency-direction-and-ports.md`。

**コアは特定のクラウド基盤を意識しない。** クラウド SDK を `go.mod` に持ち込まない。

## 実装の進め方

- **テストを先に書く。** 特に状態判定と冪等性は、テストケースを提示して合意して
  から実装する。
- 外部サービスに触れる部分はインターフェースを先に切り、テストではフェイク実装
  を使う。
- Go は慣用的な書き方を優先する。一般的でない書き方をした箇所にはコメントで
  理由を添える。
- **設計判断を下したら `docs/adr/` に記録する。** 実装を読んでも分からない
  「なぜそうしたか」だけを書く。
- 判断できないときは実装を止めて質問する。

## スコープ外（このリポジトリに入れない）

- Terraform / OpenTofu などの IaC、IAM ロール設計
- 特定クラウド基盤前提の実装（Bedrock / Vertex AI アダプタ等）
- デプロイ先の構成、CI/CD のデプロイ部分（lint / test を回す CI は可）
- Postgres 実装（インターフェースのみ用意）
- GitHub → ボードへの逆方向同期
- ボードのスナップショット / バージョニング
- 差分の自動検知による自動再同期
- draft issue へのラベル付与（GitHub の仕様上できない。識別が必要なら
  Projects v2 のカスタムフィールドを使う）
