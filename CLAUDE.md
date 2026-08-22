# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with
code in this repository.

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

## 規約の置き場所

**このファイルには全体に効くものだけを置く。** 領域ごとの約束はその
ディレクトリの `CLAUDE.md` にあり、そこを触るときに読み込まれる。

| 場所                               | 中身                                                                   |
| ---------------------------------- | ---------------------------------------------------------------------- |
| `CONTRIBUTING.md`                  | ブランチ・コミット・PR 本文・レビュー対応・CI                          |
| `.github/pull_request_template.md` | PR 本文の雛形                                                          |
| `internal/CLAUDE.md`               | 3 状態判定のデータフロー、ハンドラ、メンバーと権限、Origin 検証        |
| `web/CLAUDE.md`                    | E2E テスト、報告にスクリーンショットを添える、vite / playwright の設定 |
| `api/CLAUDE.md`                    | OpenAPI が正本、生成器のバージョン                                     |
| `.claude/skills/rv/`               | 実装後のセルフレビュー（`/rv`）                                        |
| `.claude/skills/pr-review/`        | PR に付いたレビュー指摘への対応                                        |

`CONTRIBUTING.md` だけは**ディレクトリに紐づかないので自動では読み込まれない**。
**ブランチを切る前に読む。**

**分割の判断基準は「どこを触るときに要るか」。** 片側だけを触るときにも要る
規約（フロントとバックで一致させる定義など）は、遅延して読まれる場所に
置かない。

## 開発コマンド

[Nix](https://nixos.org/) 以外はインストール不要。**すべて `nix develop` の中で
実行する。** README にグローバルインストールの手順を書かないこと。

`make` は devShell の外から呼ばれると `nix develop --command` で自分をやり直す
ので、入り忘れても道具の無いまま走ることはない。**ただし `go test` や
`bunx vitest` を直に叩く経路は包まれない。** そちらは従来どおり devShell の中で
実行する。

```sh
nix develop      # 開発シェル（Go / Bun / SQLite / golangci-lint / air）
make help        # ターゲット一覧
make setup       # 依存取得と DB 初期化（migrate を含む）
make dev         # バックエンド(:8080)とフロントエンド(:5173)を同時起動
make lint        # Go / フロントエンド / Markdown / Nix / Actions と整形を検査する
make fmt         # Go / フロントエンド / Markdown / Nix を整形する
make test        # go test + vitest
make test-e2e    # Playwright（test には含まれない）
make codegen     # api/openapi.yaml から Go / TS の型を再生成する
make migrate     # etoki migrate サブコマンドを呼ぶ
```

コミット前に `make lint` と `make test` を通す。UI かハンドラを触ったなら
`make test-e2e` も通す。

Go の単体テストはパッケージとテスト名で絞る。フロントエンドと E2E の絞り方は
`web/CLAUDE.md`。

```sh
go test ./internal/domain/ -run TestComputeContentHash_NormalizesUnicode -v
go test ./internal/adapter/sqlite/ -run TestSaveRun -v
```

`.envrc` があるので、direnv を使っているなら `direnv allow` 一度で `cd` 時に
devShell が有効になる（`nix develop` を毎回打たなくてよい）。direnv は任意で、
サポートする導線はあくまで `nix develop`。

`make dev` は `trap 'kill 0'` でプロセスグループごと止める。air と bun が
取り残されないようにするため。

## アーキテクチャ

### 依存の方向

```text
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

### HTTP 契約は OpenAPI が正本

**`api/openapi.yaml` が境界の DTO の唯一の定義。Go も TypeScript も生成物。**
手で型を足すと、そこだけ二重定義に戻る（ADR 0011）。契約を変えたら
`make codegen` を実行し、生成物を同じコミットに含める。生成物は手で編集
しない。詳細は `api/CLAUDE.md`。

### フロントとバックで一致させる必要がある定義

**注釈の判定規則が 2 箇所にある。片方だけ変えると壊れる。**

| 実装       | 場所                                                 |
| ---------- | ---------------------------------------------------- |
| Go         | `internal/domain/scene.go` の `Element.isAnnotation` |
| TypeScript | `web/src/excalidraw/annotation.ts` の `isAnnotation` |

規則は「`type === "frame"` かつ `customData.etoki` を持つ」。frame 単体を条件に
すると、ブレスト中にユーザーが使った frame まで注釈と誤認する。

注釈の frame 自体は **Excalidraw のフレームツールで作らせる**。etoki は
`customData` を付けるだけ。frame を自前で生成すると、境界にまたがる要素の
帰属判定を自分で持つことになり、frame を選んだ理由が消える。

**注釈にした frame を見分けさせるのも、キャンバスに重ねるだけで要素は変えない**
（ADR 0035）。frame の `name` は `content_hash` の入力なので、印を付けると
見分けたいだけの操作で状態が「変更あり」に落ちる。

## 認証について

**認証は任意。** 設定しなければ単一ユーザーのローカルツールのまま（ADR 0004）で、
既定で `127.0.0.1` にのみバインドする。この前提が崩れると何が壊れるかは
ADR 0004 に列挙してある。

GitHub App を設定するとログインを要求する（ADR 0015）。継ぎ目は **3 つに割って
ある**。1 つにまとめないこと。まとめると「GitHub 以外を差せる」と言いながら
GitHub の形しか差せなくなる。

| 差し替える対象                  | 継ぎ目                   |
| ------------------------------- | ------------------------ |
| 誰であるかを決める基盤          | `port.IdentityProvider`  |
| GitHub を叩くトークンの出どころ | `port.GitHubTokenSource` |
| セッションの置き場所            | `port.SessionRepository` |

- **利用者は `context.Context` で運ぶ。** 出入口は `port.ContextWithUserID` /
  `port.UserIDFromContext`。`port/` に置いてあるのは、外部リポジトリが
  `GitHubTokenSource` を自前実装するときに読む必要があるため。

配線（`cmd/etoki`）の約束:

- **`Authenticator` は 1 つだけ作る。** GitHub クライアントの `TokenSource` と
  `etoki.Options.Auth` に同じものを渡す。2 つ作るとトークン更新の直列化が効かず、
  GitHub が使い捨てにした refresh token を掴む。
- **リポジトリ一覧は `github.Config.Mode` で分岐する。** GitHub App では
  インストール経由の REST、PAT では GraphQL。「使えるリポジトリ」の定義が
  GitHub 側で違うため。判断するのは `cmd/etoki` だけ。
- **OAuth を設定したら PAT は無視する。** フォールバックにすると作成の主体が
  リクエストごとに変わり、誰が作ったのか追えなくなる。

ボードのメンバーと権限、Origin 検証は `internal/CLAUDE.md`。

## ツールチェーン上の非自明な設定

触る前に理由を把握しておくべきもの。消すと壊れる。フロントエンド側は
`web/CLAUDE.md`、生成器は `api/CLAUDE.md`。

- **`go.mod` の `ignore ./web`** と **`.golangci.yml` の `exclusions.paths: ^web/`**
  — `web/node_modules` に Go ファイルを同梱した npm パッケージ（`flatted`）が
  あり、`go ./...` と golangci-lint が拾ってしまう。`bun install` 後にしか
  再現しない。
- **`.markdownlint-cli2.yaml` の `gitignore: true`** — Markdown の検査対象は
  `**/*.md` なので、`bun install` 後は `web/node_modules` の README まで拾う。
  除外を自前で列挙せず `.gitignore` を見ているのは、上の 2 つと同じ知識を
  3 箇所目に増やさないため。こちらも `bun install` 後にしか再現しない。
- **`.prettierignore` の `web/bun.lock`** — prettier は bun のロックファイルを
  解析できず、対象に入ると落ちる。`web/node_modules` や `web/dist` を書いて
  いないのは、prettier が `.gitignore` も既定で見るため。整形の対象は
  リポジトリ全体で、`web/` の中から呼ぶと `docs/adr` とルートの Markdown が
  外れる。
- **Makefile の `ifndef ETOKI_DEVSHELL` による包み直し** — devShell の外から
  呼ばれたら `nix develop --command make` で全ターゲットをやり直す。判定に
  `IN_NIX_SHELL` を使わないのは、あれが「何かの nix shell の中」としか言わず、
  別プロジェクトの shell から呼ぶと包み直しを飛ばすため。印は `flake.nix` の
  `ETOKI_DEVSHELL` で、`shellHook` ではなく derivation の環境に置いてある
  （`shellHook` が走るかは経路によって変わる）。包み直しの中で `$(MAKE)` では
  なく `make` と書くのは、`$(MAKE)` だと外側の make（macOS なら 3.81）を
  呼び直してしまい、devShell が固定している gnumake が使われないため。
- **devShell の `GOTOOLCHAIN=local`** — `go.mod` の go ディレクティブが nixpkgs の
  Go より新しいと Go がツールチェーンを自動ダウンロードし、Nix による固定が
  無意味になる。
- **`nix flake check` は Nix コードのフォーマット検査のみ。** Go とフロント
  エンドのビルドは Makefile と CI に任せる（ADR 0002）。同じ検査は
  `make lint`（`lint-nix`）にもある。重複しているのは、`nix flake check` が
  CI でしか回らず、手元で `make lint` だけ通すと整形崩れを見落とすため。
- **YAML を見ているのは prettier と actionlint。yamllint は入れていない。**
  構文エラーと重複キーは prettier がパースに失敗して落とす。yamllint を足して
  増えるのは `document-start` のような様式の指摘だけで、`line-length` は
  prettier の `printWidth` と食い違う。ワークフロー固有の検証（式、
  コンテキスト、`run:` の中のシェル）は actionlint の担当。

## 貢献の手順

**ブランチ・コミット・PR 本文・レビュー対応の正本は `CONTRIBUTING.md`。**
**作業を始める前に読む。** ここには最低限だけ置く。

- **必ず `main` を起点に `<prefix>/<短い説明>` でブランチを切る。`main` に直接
  コミットしない。** `main` への反映は Pull Request 経由。
- コミットは Conventional Commits 形式（`feat(scope): 要約`）。要約は英語、本文は
  日本語で「なぜそうしたか」。常に緑のコミットにする。
- **PR 本文は `.github/pull_request_template.md` の見出しに沿って埋める。**
  該当しない節は消す。各節に何を書くかは `CONTRIBUTING.md`。

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

### 実装後のセルフレビュー

実装が一段落したら、**コミット前に `/rv` を実行する。** 最大 3 周で打ち切り、
残った指摘は直さずに報告して判断を仰ぐ。スキルなので手動でも呼べる。
条件は `CONTRIBUTING.md`、手順と観点は `.claude/skills/rv/SKILL.md`。

### PR 作成後のレビュー対応

PR を作ったら CodeRabbit のレビューが付く。**作りっぱなしにせず、指摘が
なくなるまで対応する。** 手順は `/pr-review`（`.claude/skills/pr-review/SKILL.md`）。

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
- 招待リンク（単回・期限つきトークン）。誰が入ってくるかを etoki が決められない
  （ADR 0017）
- GitHub 側の権限を etoki に複製すること。必ず古くなる（ADR 0017）
- 誰が draft issue を作ったかの記録。共有すると問いとして立つが、必要なら
  `sync_runs.created_by_user_id` を足す判断を別に行う
