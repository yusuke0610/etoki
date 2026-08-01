# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## etoki とは

ホワイトボード上のブレスト内容を Vision LLM に解釈させ、GitHub Projects v2 の
draft issue に変換するツール。Go + Gin のバックエンドと、Excalidraw を埋め込んだ
React フロントエンドからなる、単一ユーザー向けのローカルツール。

## 中核となる思想

設計判断で迷ったらここに立ち返る。

1. **ユーザーはブレスト中に構造を意識してはならない。** issue / epic といった
   概念をブレストフェーズに持ち込まない。構造化の責務はツール側と開発者
   フェーズに寄せる。
2. **構造は座標から推測せず、LLM に解釈させる。** 付箋の空間的近接やコネクタ
   から構造をルールベースで推測する実装は行わない。
3. **システムは自動で判断せず、状態を見せて開発者に選ばせる。** 自動同期・
   自動更新・自動作成は実装しない。すべて開発者の手動トリガー。

## 開発コマンド

[Nix](https://nixos.org/) 以外はインストール不要。**すべて `nix develop` の中で
実行する。** README にグローバルインストールの手順を書かないこと。

```sh
nix develop      # 開発シェル（Go / Bun / SQLite / golangci-lint / air）
make help        # ターゲット一覧
make setup       # 依存取得と DB 初期化（migrate を含む）
make dev         # バックエンド(:8080)とフロントエンド(:5173)を同時起動
make lint        # golangci-lint + eslint + tsc
make test        # go test + vitest
make migrate     # etoki migrate サブコマンドを呼ぶ
```

コミット前に `make lint` と `make test` を通す。

`.envrc` があるので、direnv を使っているなら `direnv allow` 一度で `cd` 時に
devShell が有効になる（`nix develop` を毎回打たなくてよい）。direnv は任意で、
サポートする導線はあくまで `nix develop`。

### 単体テストの実行

```sh
# Go: パッケージ + テスト名
go test ./internal/domain/ -run TestComputeContentHash_NormalizesUnicode -v
go test ./internal/adapter/sqlite/ -run TestSaveRun -v

# フロントエンド: ファイル指定 / テスト名指定
cd web && bunx vitest run src/excalidraw/annotation.test.ts
cd web && bunx vitest run -t "customData"
```

`make dev` は `trap 'kill 0'` でプロセスグループごと止める。air と bun が
取り残されないようにするため。

## アーキテクチャ

### 依存の方向

```
フロントエンド → Gin ハンドラ → ユースケース層 → port のインターフェース → アダプタ実装
```

ユースケース層がクラウド SDK や GitHub SDK の型を直接参照してはならない。
**コアは特定のクラウド基盤を意識しない。** クラウド SDK を `go.mod` に持ち込まない。

### 公開 API 面（`internal/` の外）

`port/` と `etoki.go` だけが `internal/` の外にある。**これは意図的。** Go の
`internal/` は他モジュールから import できないため、外部リポジトリが
`LLMClient` を実装して差し込むには公開されている必要がある（ADR 0001）。

その帰結として:

- `port/` は `internal/` に依存しない。境界 DTO を自前で持ち、ドメインモデル
  との詰め替えはユースケース層が行う（例: `SyncRun.ContentHash` は
  `domain.ContentHash` ではなく `string`）。
- `etoki.New(Options)` は**リポジトリを引数で受け取る**。SQLite の配線を知って
  いるのは `cmd/etoki` だけ。

### 3 状態判定のデータフロー

これが etoki の中心。複数ファイルにまたがるので全体像を把握しておく。

```
boards.scene （SQLite に保存された Excalidraw シーン JSON）
   ↓ domain.ParseScene
Scene.Annotations()           … customData.etoki を持つ frame 要素
   ↓ Scene.AnnotationTexts(id)  … frameId と containerId の両方を辿る
   ↓ domain.ComputeContentHash  … 正規化して SHA-256
現在のハッシュ
   ↕ 比較（domain.DecideState）
sync_runs の最新 run の content_hash
   ↓
uncreated / created / changed
```

判定は**保存済みシーンが基準**。フロントで編集中の内容は反映されないので、
UI は未保存の変更があることを表示する。

押さえるべき点:

- **`content_hash` の入力はテキストのみ。** 図形・矢印・座標だけの変更は検知
  しない。これは仕様であり、`TestComputeContentHash_IgnoresNonTextChanges` で
  固定してある。善意で「直さない」こと。
- **`sync_runs` は履歴。** 再実行しても過去の run を消さない。上書きすると
  GitHub 側に残っている draft issue を追跡できなくなるため（ADR 0007）。
- **最新 run は `created_at` ではなく `id` で決める。** 時刻は呼び出し側が与える
  設計なので同一時刻の run がありうる。
- **GitHub に作るのは epic と issue の 2 階層のみ。** LLM 出力の最上位
  `summary` は作成前の確認表示にだけ使い、GitHub には作らない（ADR 0006）。

### フロントとバックで一致させる必要がある定義

**注釈の判定規則が 2 箇所にある。片方だけ変えると壊れる。**

| 実装 | 場所 |
| --- | --- |
| Go | `internal/domain/scene.go` の `Element.isAnnotation` |
| TypeScript | `web/src/excalidraw/annotation.ts` の `isAnnotation` |

規則は「`type === "frame"` かつ `customData.etoki` を持つ」。frame 単体を条件に
すると、ブレスト中にユーザーが使った frame まで注釈と誤認する。

注釈の frame 自体は **Excalidraw のフレームツールで作らせる**。etoki は
`customData` を付けるだけ。frame を自前で生成すると、境界にまたがる要素の
帰属判定を自分で持つことになり、frame を選んだ理由が消える。

## ツールチェーン上の非自明な設定

触る前に理由を把握しておくべきもの。消すと壊れる。

- **`go.mod` の `ignore ./web`** と **`.golangci.yml` の `exclusions.paths: ^web/`**
  — `web/node_modules` に Go ファイルを同梱した npm パッケージ（`flatted`）が
  あり、`go ./...` と golangci-lint が拾ってしまう。`bun install` 後にしか
  再現しない。
- **`web/vite.config.ts` の test セクション** — excalidraw を vitest で読むのに
  3 つ必要。prod バンドルへの `alias`、`open-color`（実体が JSON）の `inline`、
  そして `src/test-setup.ts` の canvas スタブ（import 時に 2D コンテキストの
  機能検出が走る）。
- **devShell の `GOTOOLCHAIN=local`** — `go.mod` の go ディレクティブが nixpkgs の
  Go より新しいと Go がツールチェーンを自動ダウンロードし、Nix による固定が
  無意味になる。
- **`nix flake check` は Nix コードのフォーマット検査のみ。** Go とフロント
  エンドのビルドは Makefile と CI に任せる（ADR 0002）。
- **React は 18 系に固定。** `@excalidraw/excalidraw` との組み合わせを揃えるため。
- **`web/src/excalidraw/assumptions.test.ts`** — `customData` が serialize と
  restore を越えて残るという、注釈設計の前提そのものを固定するテスト。
  ライブラリ更新で落ちたら設計を見直す合図。

## ブランチ運用

**必ず `main` を起点にブランチを切って作業する。`main` に直接コミットしない。**

```sh
git switch main && git pull
git switch -c <prefix>/<短い説明>
```

`<prefix>` はコミットの種別に合わせる（`feat` / `fix` / `docs` / `build` / `ci`
/ `chore` / `refactor` / `test`）。`main` への反映は Pull Request 経由。

## コミット

- 1 コミットの粒度を小さく保つ。
- Conventional Commits 形式（`feat(scope): 要約`）。要約は英語、本文は日本語。
- **本文には「何をしたか」ではなく「なぜそうしたか」を書く。** 差分を読めば
  分かることは書かない。
- 常に緑のコミットにする。テストだけ先にコミットして赤い状態を履歴に残さない。

## 実装の進め方

- **テストを先に書く。** 特に状態判定と冪等性は、テストケースを提示して合意して
  から実装する。
- 外部サービスに触れる部分はインターフェースを先に切り、テストではフェイク実装
  を使う。
- Go は慣用的な書き方を優先する。一般的でない書き方をした箇所にはコメントで
  理由を添える。
- **設計判断を下したら `docs/adr/` に記録する。** 実装を読んでも分からない
  「なぜそうしたか」だけを書く。索引は `docs/adr/README.md`。
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

## 認証について

単一ユーザーがローカルで動かす前提のため、認証・認可・マルチテナントを実装
していない。既定で `127.0.0.1` にのみバインドする。この前提が崩れると何が
壊れるかは ADR 0004 に列挙してある。
