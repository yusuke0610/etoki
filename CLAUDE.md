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
make lint        # golangci-lint + eslint + tsc + 整形検査（gofmt / prettier）
make fmt         # Go / Nix / フロントエンドを整形する
make test        # go test + vitest
make test-e2e    # Playwright（test には含まれない）
make codegen     # api/openapi.yaml から Go / TS の型を再生成する
make migrate     # etoki migrate サブコマンドを呼ぶ
```

コミット前に `make lint` と `make test` を通す。UI かハンドラを触ったなら
`make test-e2e` も通す。

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

# E2E: ファイル指定 / テスト名指定 / ブラウザを見ながら
cd web && bunx playwright test e2e/interpretation.spec.ts
cd web && bunx playwright test -g "解釈するまで"
cd web && bunx playwright test --ui
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

### HTTP 契約は OpenAPI が正本

**`api/openapi.yaml` が境界の DTO の唯一の定義。Go も TypeScript も生成物。**
手で型を足すと、そこだけ二重定義に戻る（ADR 0011）。

| 生成物 | 生成元 | 使う側 |
| --- | --- | --- |
| `internal/httpapi/apitypes/types.gen.go` | `oapi-codegen` | Gin ハンドラ |
| `web/src/api/generated.ts` | `openapi-typescript` | `web/src/api/types.ts` 経由でフロント全体 |

- **契約を変えたら `make codegen` を実行し、生成物を同じコミットに含める。**
  忘れると CI の codegen drift で落ちる。
- 生成物は手で編集しない。次の生成で消える。
- フロントは `web/src/api/types.ts` の名前を import する。
  `components["schemas"][...]` を直書きしない。**独自の別名を付けない。**
  名前が食い違うと、契約を直したときに追随先を機械的に辿れなくなる。
- エラー本文も `ErrorResponse` に揃える。`gin.H{"error": ...}` を直に書かない。
- E2E のモック応答も生成型で縛ってある（`web/e2e/helpers/api.ts`）。
  モックだけが古い契約のまま緑になるのを防ぐため。

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

## E2E テスト

`web/e2e/` に置く。Playwright で実ブラウザを動かし、**バックエンドは起動しない。**
API はすべて `page.route` で差し替える（ADR 0012）。

- **確かめるのは画面側の約束。** 3 状態の表示、解釈するまで作成ボタンが出ない
  こと、途中失敗の見せ方、保存で解釈結果を捨てること。外部連携そのものは
  アダプタの単体テストの担当。E2E に持ち込まない。
- **モックの応答本文は生成型で書く。** `web/e2e/helpers/api.ts` の `Reply<T>`。
  型を外すと、モックだけが古い契約のまま緑になる。
- **ルートはパス名の述語でマッチさせる。** パスの途中に `api` を含むグロブは
  Vite が配信する `/src/api/types.ts` まで傍受してしまう。
- `make test` には含めない。速さを保ちたいので、E2E はコミット前と CI で回す。

### 報告にブラウザの実行結果を添える

**UI に触れる修正をしたら、`make test-e2e` を実行し、ブラウザの実行結果の
スクリーンショットを報告に添える。** 「動くはず」ではなく、実際にどう表示された
かを見せる。テストが緑であることと、画面が意図どおりであることは別の話で、
セレクタが通っても見た目が壊れていることはある。

- 画像は `web/e2e/screenshots.spec.ts` が `web/e2e-output/screenshots/` に
  書き出す（gitignore 済み）。`make test-e2e` を回せば毎回作り直される。
- 添えるのは変更が現れている画面。全部を貼らない。新しい画面や状態を足した
  なら `screenshots.spec.ts` に撮影を 1 つ足す。
- 意図と違う表示になっていたら、それも隠さず添えて報告する。落ちたテストの
  スクリーンショットは `web/e2e-output/results/` にある。

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
- **`@playwright/test` のバージョンは exact 指定。** devShell が渡す
  `playwright-driver.browsers`（nixpkgs 側）と揃っていないと、要求される
  リビジョンのブラウザが見つからず E2E が起動しない。`nix flake update` で
  `playwright-driver` が動いたら `web/package.json` も同じ値に上げる。
- **`web/tsconfig.json` の `include` に `e2e` がある。** E2E のモックが契約の
  生成型から外れたことを `tsc` に見つけさせるため。外すと ADR 0012 の仕組みが
  黙って効かなくなる。
- **`web/vite.config.ts` の `test.include`** — vitest の既定は `*.spec.ts` も
  拾うため、明示しないと Playwright の spec を vitest が実行しようとする。
- **生成器のバージョンで生成物の形が変わる。** `oapi-codegen` は `flake.lock`
  が、`openapi-typescript` は `bun.lock` が握っている。`nix flake update` や
  `bun update` のコミットには `make codegen` の結果も含める。
- **`make codegen` は必ず devShell の中で実行する。** `types.gen.go` の冒頭
  バナーには生成器のバージョン文字列が埋まる。しかもこれは「どのバージョンか」
  ではなく「どうビルドされたか」で変わる。nixpkgs は
  `-X main.noVCSVersionOverride=2.5.1` を渡すので `2.5.1` になるが、
  `go run ...@v2.5.1` で入れた同じバージョンは `v2.5.1` と出る。devShell の外で
  生成すると、中身が同じでも codegen drift で落ちる。

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

### 実装後のセルフレビュー

実装が一段落したら、コミット前に `/rv` を実行する。差分をこのファイルの規約に
照らして点検し、問題があれば直して再度点検する。手順と観点は
`.claude/skills/rv/SKILL.md` にある。

- **サイクルは最大 3 周。** 3 周して残った指摘は直さずに報告して判断を仰ぐ。
  同じ箇所を 3 周直して収まらないなら設計の問題であり、手直しでは解けない。
- 指摘ゼロならその周で終える。周回数を消化しない。
- スキルなので手動でも呼べる。実装の直後に限らず、レビューしたいときに使う。

### PR 作成後のレビュー対応

PR を作ったら CodeRabbit のレビューが付く。**作りっぱなしにせず、指摘が
なくなるまで対応する。** 指摘は数分遅れて付くので、直後に見て無ければ待つ。

```sh
gh pr view <番号> --json reviews -q '.reviews[].body'
gh api repos/yusuke0610/etoki/pulls/<番号>/comments -q '.[] | "\(.path):\(.line)\n\(.body)"'
```

- **1 件ずつ決着をつける。** 直すか、直さない理由をコメントで返すか。放置
  しない。「指摘がなくなるまで」は全部そのとおりに直すという意味ではない。
- **このファイルの規約や仕様と衝突する指摘は直さない。** レビュアーは
  リポジトリの事情を知らない。`content_hash` がテキストのみなのを「バグ」と
  言われても直さず、理由を返す。
- 修正を push すると再レビューが走る。**もう一度確認する。** 対応した結果に
  新しい指摘が付くことがある。
- 指摘が収束しない、または同じ箇所を何度も往復するときは、そこで止めて
  ユーザーに報告する。

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
