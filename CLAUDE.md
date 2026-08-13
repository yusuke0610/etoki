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
- **フロントの「未保存」判定（`web/src/excalidraw/dirty.ts`）は別物。** 保存は
  シーン全体を書くので、図形を動かしただけでも未保存にする。`content_hash` に
  揃えると、保存すべき変更を取りこぼす。
- **解釈の入力だけは 2 つの出どころを持つ。** テキストは保存済みシーンから、
  注釈範囲の画像はフロントのキャンバスから取る（ADR 0018）。`exportToBlob` は
  ブラウザにしか無いため。**揃えているのは「未保存のあいだは解釈させない」と
  いう UI 側の約束だけである。** API を直接叩けば食い違わせられるが、解釈は
  GitHub に何も作らないので取り消せない副作用が出ない。作成側は同じ穴を
  `contentHash` の照合で塞いである（ADR 0010）。両者を同じ扱いにしないこと。
- **`sync_runs` は履歴。** 再実行しても過去の run を消さない。上書きすると
  GitHub 側に残っている draft issue を追跡できなくなるため（ADR 0007）。
- **最新 run は `created_at` ではなく `id` で決める。** 時刻は呼び出し側が与える
  設計なので同一時刻の run がありうる。
- **GitHub に作るのは epic と issue の 2 階層のみ。** LLM 出力の最上位
  `summary` は作成前の確認表示にだけ使い、GitHub には作らない（ADR 0006）。
- **作成先の Projects v2 はボードごと。** プロセス全体の設定ではない
  （`ETOKI_GITHUB_PROJECT_ID` は廃止済み）。`boards` の
  `repository_owner` / `repository_name` / `project_id` に持つ。**作成時に必須**
  なので新規のボードは必ず選択済みだが、移行前のボードは 3 つとも空文字の
  「未選択」で残るため、その経路は消さない（ADR 0017）。**そのボードで最初の
  run ができたら固定**し、以後の変更は 409 で拒む。作成先が変わると `sync_runs`
  が指す item を見失うため（ADR 0014）。変えられるのは owner だけ（ADR 0017）。
  判定は `BoardService.SetTarget` にある。**ハンドラに移さないこと。**
  **作成と作成先の変更はボード単位で直列化する**（`usecase.BoardLocks`）。判定
  材料の `sync_runs` は作成の最後に書かれるので、排他が無いと作成の途中で
  作成先を変えられる。`BoardService` と `CreationService` には**同じ
  `*BoardLocks` を渡す**。別々に持たせると直列化が素通りするので、任意の設定
  ではなくコンストラクタの引数にしてある。
- **一覧は作成先でまとめて見せる**（ADR 0019）。木は実体の包含ではなく射影。
  1 つの Project に複数のボードがぶら下がる。組み立ては
  `web/src/board/grouping.ts` の純関数にある。
- **`project_number` / `project_title` は表示用のスナップショット。**
  作成先を選んだ時点の値を保存するだけで、**判定には使わない**。
  `BoardTarget.Selected` は 3 つ（owner / name / projectId）だけを見る。ここに
  足すと、名前を送らずに設定した正しい作成先が「未選択」に落ちる。古くなったら
  選び直しで直す。**自動で GitHub に取りにいって書き戻さない。**

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
  こと、途中失敗の見せ方、保存で解釈結果を捨てること、未保存のあいだは解釈
  できないこと、ロールと GitHub の権限による出し分け（`sharing.spec.ts`）。
  外部連携そのものはアダプタの単体テストの担当。E2E に持ち込まない。
- **注釈範囲の画像を書き出せることは E2E でしか確かめられない。** canvas が
  要るので vitest では動かない。`interpretation.spec.ts` がリクエストに画像が
  載ったことを見ている。ここを消すと、書き出しが壊れても単体テストは緑のまま。
- **モックの応答本文は生成型で書く。** `web/e2e/helpers/api.ts` の `Reply<T>`。
  型を外すと、モックだけが古い契約のまま緑になる。
- **ルートはパス名の述語でマッチさせる。** パスの途中に `api` を含むグロブは
  Vite が配信する `/src/api/types.ts` まで傍受してしまう。
- **作成先の選択画面を触るときは `.picker` に絞る**（`helpers/board.ts` の
  `picker` / `chooseTarget`）。サイドバーの木にも `acme/web` や
  `#1 ロードマップ` と同じ名前のボタンが並ぶので、ページ全体から名前で引くと
  2 つ見つかって落ちる（ADR 0019）。
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
- 招待リンク（単回・期限つきトークン）。誰が入ってくるかを etoki が決められない
  （ADR 0017）
- GitHub 側の権限を etoki に複製すること。必ず古くなる（ADR 0017）
- 誰が draft issue を作ったかの記録。共有すると問いとして立つが、必要なら
  `sync_runs.created_by_user_id` を足す判断を別に行う

## 認証について

**認証は任意。** 設定しなければ単一ユーザーのローカルツールのまま（ADR 0004）で、
既定で `127.0.0.1` にのみバインドする。この前提が崩れると何が壊れるかは
ADR 0004 に列挙してある。

GitHub App を設定するとログインを要求する（ADR 0015）。継ぎ目は **3 つに割って
ある**。1 つにまとめないこと。まとめると「GitHub 以外を差せる」と言いながら
GitHub の形しか差せなくなる。

| 差し替える対象 | 継ぎ目 |
| --- | --- |
| 誰であるかを決める基盤 | `port.IdentityProvider` |
| GitHub を叩くトークンの出どころ | `port.GitHubTokenSource` |
| セッションの置き場所 | `port.SessionRepository` |

- **利用者は `context.Context` で運ぶ。** 出入口は `port.ContextWithUserID` /
  `port.UserIDFromContext`。`port/` に置いてあるのは、外部リポジトリが
  `GitHubTokenSource` を自前実装するときに読む必要があるため。
- **`Authenticator` は 1 つだけ作る。** GitHub クライアントの `TokenSource` と
  `etoki.Options.Auth` に同じものを渡す。2 つ作るとトークン更新の直列化が効かず、
  GitHub が使い捨てにした refresh token を掴む。
- **リポジトリ一覧は `github.Config.Mode` で分岐する。** GitHub App では
  インストール経由の REST、PAT では GraphQL。「使えるリポジトリ」の定義が
  GitHub 側で違うため。判断するのは `cmd/etoki` だけ。
- **OAuth を設定したら PAT は無視する。** フォールバックにすると作成の主体が
  リクエストごとに変わり、誰が作ったのか追えなくなる。

### ボードのメンバーと権限（ADR 0016 / 0017）

**権限は 2 層ある。混ぜないこと。** 共有すると「ボードは開けるが GitHub には
書けない」が例外ではなく普通に起きる。1 つの権限に畳むと、招待された人が
ブレストには参加できて作成だけができない、という要件そのものを表現できない。

| | 決める主体 | 持ち場 |
| --- | --- | --- |
| そのボードを開けるか | **etoki** | `board_members` |
| その Project に書けるか | **GitHub** | 複製しない。実行時に GitHub が返す |

- **GitHub 側の権限を etoki の DB に複製しない。** etoki は実行者のトークンで
  GitHub を叩くので（ADR 0015）、リポジトリの権限は複製しなくてもすでに効いて
  いる。複製は、効いている判定の手前にずれる判定を置くだけ。
- **`GET /api/boards/{id}/access` の `projectAccess` は表示用。判定に使わない。**
  実際の可否は作成時に GitHub が返したものが正しい。確かめられなかったときは
  `allowed` / `denied` のどちらにも倒さず `unknown` を返す。確かめていないことを
  確かめたように見せない（中核思想 3）。

ロールは owner / editor / viewer の 3 つ。

- **ロールの上下を知っているのは `port.BoardRole.AtLeast` だけ。** SQL にロールの
  集合を書かない。永続化層の `WHERE` が見るのは「メンバーかどうか」まで。書くと
  判定が 2 箇所になり、片方だけ変わる。
- **判定の入口は `usecase.boardGuard.access` に 1 本化してある。** ボードに触る
  ユースケースが `boards.Find` を直に呼ばないこと。呼ぶと、足した操作だけロールを
  見ない取りこぼしが起きる。落ちる側を固定した表テストが
  `internal/usecase/access_test.go` にある。
- **viewer に解釈を許さない。** 解釈は LLM を叩く外部呼び出しで課金も伴う。
  閲覧者に外部呼び出しを許すのは「閲覧」ではない。
- **`BoardRepository` は操作者を引数で受け取る。** ctx から実装が勝手に読む形に
  変えないこと。引数なら渡し忘れがコンパイルエラーになる。認可は「忘れたら
  気づける」形で持つ。
- **空文字は無効値ではなく「認証なしの所有者」1 人。** 認証を設定していない構成
  では全ボードがその 1 人のものになり、これまでどおり全部が見える。認証あり /
  なし / 移行前 が同じ式で説明できるので、「所有者が無い」の特別扱いを増やさない。
- **非メンバーは 404、メンバーの権限不足は 403。** 分けるかどうかは「存在を隠す
  必要があるか」で決める。非メンバーに 403 を返すと ID の総当たりで存在を
  確かめられる。メンバーはボードの存在をすでに知っているので、何が足りないのかを
  隠す理由が無い。
- **`sync_runs` はメンバーで絞らない。** `board_id` で引く経路しかなく、その board を
  取れるのはメンバーだけ。二重に絞ると、絞り忘れたときにどちらが効いているのか
  分からなくなる。
- **招待できるのは一度ログインした利用者だけ。** 未ログインの login 宛に招待を
  積まない。login は改名で変わるので、空いた login を取った別人に権限が渡る。
  `etoki claim` と同じ制約。
- **最後の owner は外せない・降格できない。** 通すと、誰も招待できず作成先も
  変えられないボードが残る。判定は「自分自身か」ではなく「他に owner がいるか」。
- **ボードの作成には作成先が要る。** 候補は `minPermissionLevel: WRITE` で絞って
  あるので（ADR 0014）、書ける Project を 1 つも持たない人はボードを作れない。
  「作成にはリポジトリへのアクセス権が要る」はこの形で満たす。作成時に GitHub へ
  問い合わせて確かめはしない。
- 移行前のボードは自動で誰のものにもしない。`etoki claim <login>` で引き受ける
  （中核思想 3）。

**バインド先を絞るだけでは足りない。** ループバックにバインドしていても、
ブラウザ経由なら外部サイトのページから API を叩ける（CSRF / DNS
リバインディング）。`internal/httpapi/origin.go` が Host と Origin を検証して
弾いている（ADR 0013）。ここを触るときの約束:

- **`Origin` が無いリクエストは通す。** curl やスクリプトが該当する。ブラウザは
  cross-origin の POST に必ず `Origin` を付け、攻撃者はそれを省略・偽装できない。
  ここを塞いでも守れるものは増えず、CLI からの利用だけが壊れる。
- **副作用を持つ GET は `/api/auth/callback` だけ。** これは例外で、Origin では
  なく `state` が守っている。**これ以上増やさないこと。** 増やすなら ADR 0013 の
  判断ごと見直す。送り出し側の `/api/auth/login` を POST にしてあるのも、
  state の発行を Origin 検証の内側に置くため。
- **ループバック判定でポートを見ない。** `make dev` では Vite の dev サーバーが
  `Host` と `Origin` を書き換えずに転送するため、`:8080` ではなく `:5173` が
  届く。リッスンポートに絞ると開発時に落ちる。
- **ハンドラのテストは Host を明示する。** `httptest.NewRequest` の既定は
  `example.com` なので、指定を忘れると 403 で落ちる。
