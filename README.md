# etoki（絵解き）

> [!NOTE]
> **人に使ってもらうための開発は 2026-11-25 に終了しました。** ホストして
> 運用し続けるイメージが持てなかったためです。今後は作者が自分で使いながら、
> その範囲で手を入れていきます。

**ブレストの絵を解いて、GitHub の設計に落とす。**

ホワイトボード上の自由なブレスト内容を Vision LLM に解釈させ、GitHub Projects v2 の
draft issue に変換するツールです。

## 中核となる思想

1. **ユーザーはブレスト中に構造を意識してはならない。** issue / epic といった概念を
   ブレストフェーズに持ち込まない。
2. **構造は座標から推測せず、LLM に解釈させる。** 付箋の空間的近接やコネクタから
   構造をルールベースで推測しない。
3. **システムは自動で判断せず、状態を見せて開発者に選ばせる。** 自動同期・自動更新・
   自動作成は行わない。

## クラウドで動かす

etoki は 1 人専用のツールで、クラウドに載せて使います
（[ADR 0108](docs/adr/0108-single-user-cloud-tool.md)）。下の「開発」の手順でも
手元で動きますが、それは開発と確かめのためです。

載せる先の構成（いまは Google Cloud Run）は、このリポジトリに置いていません。
ここに書くのは、どこに載せるときにも etoki の作りから決まることです。

- **認証を設定する。** ループバックの外で待ち受けるなら必須で、無ければ起動
  しません（[ADR 0105](docs/adr/0105-only-the-owner-signs-in.md)）。手順は
  [GitHub App でログインする](#github-app-でログインする)にあります。
- **載せた先の URL を渡す。** `ETOKI_PUBLIC_URL` に https の URL を、
  `ETOKI_ALLOWED_ORIGINS` にそのオリジンを渡します。TLS を基盤の側で終端するなら、
  `ETOKI_PUBLIC_URL` を渡さないと、etoki は認可の戻り先を http で組み、
  セッションの cookie に Secure を付けません。
- **1 台で動かす。** 解釈と図の生成のジョブ、ボードごとの排他、LLM の上限は
  プロセスのメモリにあります。2 台に分かれると、片方で始めたジョブをもう片方
  から受け取れず、排他と上限も台ごとに分かれます。
- **DB のファイルを残す。** 作成の記録は取り直せません
  （[ADR 0023](docs/adr/0023-record-created-issue-body.md)）。止まるたびに
  ディスクが消える基盤なら、外へ複製し続け、起動のたびに戻します。
- **応答を返したあとも CPU を止めない。** 解釈と図の生成は、受け付けた
  リクエストが返ったあとで走ります
  （[ADR 0085](docs/adr/0085-interpretation-as-server-side-job.md)・
  [ADR 0091](docs/adr/0091-diagram-draft-as-server-side-job.md)）。
- **起動の前に `etoki migrate` を当てる。** etoki は移行を自分では当てず、
  足りなければ起動しません。
- **止めるときは待たせる。** 作成の途中で止められると、作れたぶんを記録して
  から止まります。待つ上限は `etoki.go` の `shutdownTimeout` です。基盤の猶予が
  それより短いと、記録が間に合わないことがあります。
- **画面も etoki に配らせる。** `ETOKI_WEB_DIR` にビルドした成果物を渡します
  （[dev サーバーを使わずに動かす](#dev-サーバーを使わずに動かす)）。

コンテナイメージは、まだこのリポジトリで作っていません。

## 開発

手元で動かすのは、開発と確かめのためです。[Nix](https://nixos.org/) があれば
他に何もインストールする必要はありません。

```sh
nix develop      # 開発用シェルに入る（Go / Bun / SQLite / lint などが揃う）
make setup       # 依存関係の取得と DB 初期化
make dev         # バックエンドとフロントエンドを同時に起動
make help        # ターゲット一覧
```

**ターゲットの一覧は `make help` が出します。** ここに書き写すと、Makefile に
足したものが漏れたまま残ります。

`make` は `nix develop` に入り忘れても、自分で開発用シェルに入り直して動きます。

HTTP API の仕様は [`api/openapi.yaml`](api/openapi.yaml) にあります。これが
契約の正本で、Go と TypeScript の型はここから生成しています。仕様を変えたら
`make codegen` で生成物を作り直してください。

### direnv（任意）

[direnv](https://direnv.net/) を使っているなら、一度許可するだけで
このディレクトリに `cd` したときに開発用シェルが有効になります。

```sh
direnv allow
```

キャッシュ用の nix-direnv は `.envrc` が flake から取り出すので、
別途インストールする必要はありません。direnv を使わない場合は
`nix develop` のままで構いません。

## 設定

環境変数で設定します。**LLM も GitHub も設定しなくても起動します。** LLM が
無ければ解釈が、GitHub が無ければ draft issue の作成が「設定されていない」と
返し、ボードの作成と編集と注釈の状態表示はそのまま使えます。ブレストだけ先に
やる、という使い方を潰さないためです。GitHub が無いあいだに作ったボードは作成先が
未選択のまま残り、設定したあとで開くと作成先の選択から始まります。

| 変数                             | 既定値                      | 用途                                                               |
| -------------------------------- | --------------------------- | ------------------------------------------------------------------ |
| `ETOKI_ADDR`                     | `127.0.0.1:8080`            | リッスンアドレス                                                   |
| `ETOKI_ALLOWED_ORIGINS`          | （なし）                    | 追加で許すオリジン（カンマ区切り）。ループバックは常に許す         |
| `ETOKI_DB_PATH`                  | `etoki.db`                  | SQLite ファイルのパス                                              |
| `ETOKI_WEB_DIR`                  | （なし）                    | ビルド済みフロントエンドの置き場所。未設定なら画面を配らない       |
| `ETOKI_LLM_BASE_URL`             | `https://api.anthropic.com` | LLM のエンドポイント。鍵を設定しているなら http はループバックだけ |
| `ETOKI_LLM_API_KEY`              | （なし）                    | LLM の API キー。認証不要なら未設定でよい                          |
| `ETOKI_LLM_MODEL`                | `claude-opus-5`             | モデル ID                                                          |
| `ETOKI_LLM_TIMEOUT`              | `5m`                        | LLM の 1 回の呼び出しを待つ上限                                    |
| `ETOKI_LLM_THINKING`             | （なし）                    | `disabled` で思考を切らせる。Anthropic の接続先では使えない        |
| `ETOKI_LLM_MAX_CONCURRENT`       | `1`                         | 1 人が同時に走らせられる解釈・図の生成の数                         |
| `ETOKI_LLM_RATE_LIMIT`           | （なし）                    | 窓のあいだに始められる回数。未設定なら無制限。単独で設定してよい   |
| `ETOKI_LLM_RATE_WINDOW`          | `1h`                        | 回数を数える窓。単独では設定できない（回数の上限が要る）           |
| `ETOKI_GITHUB_TOKEN`             | （なし）                    | GitHub の classic PAT。認証を設定しても、GitHub はこの 1 本で叩く  |
| `ETOKI_GITHUB_BASE_URL`          | `https://api.github.com`    | GitHub API のルート。http はループバックだけ。GHES は未確認        |
| `ETOKI_GITHUB_APP_CLIENT_ID`     | （なし）                    | GitHub App の client ID。設定するとログインを要求する              |
| `ETOKI_GITHUB_APP_CLIENT_SECRET` | （なし）                    | 同 client secret                                                   |
| `ETOKI_ALLOWED_GITHUB_USER_ID`   | （なし）                    | ログインを通す 1 人の GitHub の数値 ID。認証を使うなら必須         |
| `ETOKI_PUBLIC_URL`               | （なし）                    | 認可から戻る先。空ならリクエストの Host から組む                   |
| `ETOKI_GITHUB_KIND_FIELD`        | `Kind`                      | 種別のカスタムフィールド名                                         |
| `ETOKI_GITHUB_PARENT_FIELD`      | `Parent`                    | 親のカスタムフィールド名                                           |

解釈と図のドラフト生成は LLM を叩くので課金を伴います。**上限は利用者ごとに
効き、2 つで 1 つの枠を共有します**（ADR 0044）。既定で効くのは同時実行だけで、
回数の上限は `ETOKI_LLM_RATE_LIMIT` を設定したときだけ効きます。妥当な回数は
料金プランと使い方で決まる値なので、etoki は既定を決めません。上限に当たると
429 が返り、そのとき LLM は 1 回も呼ばれていません。

認証は既定では持ちません。GitHub App を設定するとログインを要求します（後述）。
どちらの構成でも、許可していない Host / Origin を持つブラウザからのリクエストは
拒否します（ADR 0013）。ループバックは常に許可されるので、通常の
使い方では意識する必要はありません。`curl` やスクリプトからの利用にも影響しません。
`ETOKI_ADDR` で公開インターフェースにバインドする場合は、
`ETOKI_ALLOWED_ORIGINS` にそのオリジンを足さないと自分のブラウザからも
届かなくなります。

**認証を設定しないまま `ETOKI_ADDR` を広げると、起動しません。** 認証なしの構成では
全ボードが 1 人のもの（[ADR 0016](docs/adr/0016-boards-have-owners.md)）なので、
そのアドレスに届く人は誰でもログインなしに全ボードを読み書きでき、
`ETOKI_GITHUB_TOKEN` で draft issue を作り、`ETOKI_LLM_API_KEY` で LLM を
叩けてしまいます。秘密の値を 1 つ渡し忘れただけでこの構成になるので、警告では
なく起動を止めます（[ADR 0105](docs/adr/0105-only-the-owner-signs-in.md)）。
クラウドに載せるときや別の端末から開くときは、GitHub App を設定してログインを
要求してください（後述）。ループバック（既定の `127.0.0.1`）で待ち受けるなら、
認証なしで動きます。手元で開発するときの形です。

### dev サーバーを使わずに動かす

`make dev` は Vite の開発サーバー（:5173）が画面を配ります。**クラウドに載せた
etoki では、ビルドした成果物を etoki 自身に配らせてください。** 手元の
`make start` は、その形を確かめるためのものです。

```sh
make start   # make build のあと ETOKI_WEB_DIR=web/dist で起動する
```

`make build` で作った `web/dist` を `ETOKI_WEB_DIR` で渡しているだけなので、
バイナリを直接動かしても同じです。

```sh
make build
ETOKI_WEB_DIR=web/dist bin/etoki
```

**ブラウザがいるのは :8080 になります**（`ETOKI_ADDR` を変えたならそのポート）。
GitHub App を設定しているなら、Callback URL と `ETOKI_PUBLIC_URL` もそちらに
合わせてください。

| 設定               | `make dev`                                | `make start`                              |
| ------------------ | ----------------------------------------- | ----------------------------------------- |
| Callback URL       | `http://127.0.0.1:5173/api/auth/callback` | `http://127.0.0.1:8080/api/auth/callback` |
| `ETOKI_PUBLIC_URL` | `http://127.0.0.1:5173`                   | `http://127.0.0.1:8080`                   |

`ETOKI_WEB_DIR` を渡さなければ画面は配りません。`make dev` では Vite が同じものを
持っているので、両方が配ると画面の出どころが構成によって変わるためです
（[ADR 0032](docs/adr/0032-serve-the-built-frontend.md)）。渡したのに `index.html`
が無い場合は起動時に落ちます。公開インターフェースにバインドするときの
`ETOKI_ALLOWED_ORIGINS` は[上に書いたとおり](#設定)で、足さないと API だけでなく
画面も開けません。

### データを残す

ボード・メンバー・作成の記録は `ETOKI_DB_PATH`（既定は `etoki.db`）の SQLite
ファイルにあります。**作成の記録は取り直せません。** どの注釈からどの draft issue
を作ったかは作成の瞬間にしか控えられず、GitHub 側の draft issue は etoki から
消せないので残ります（[ADR 0023](docs/adr/0023-record-created-issue-body.md)）。
クラウドに載せるときの残し方は[クラウドで動かす](#クラウドで動かす)にあります。

`make clean` はこのファイルに触りません。消すターゲットは `make reset-db CONFIRM=1`
に分けてあり、消す先は `ETOKI_DB_PATH` ではなく `make migrate` と同じ `DB_PATH`
（既定は `etoki.db`）です。

**消す前に etoki を止め、`sqlite3` で開いているなら閉じてください。** 開いたまま
消すと、開いている側は消えたファイルを読み書きし続け、そのあいだの書き込みは
閉じた時点で失われます。動いている etoki が DB を開いていれば、`reset-db` は
消さずに止まります。

バックアップは SQLite の `.backup` で取ってください。動いているあいだでも
一貫した 1 ファイルになります。

```sh
sqlite3 "${ETOKI_DB_PATH:-etoki.db}" ".backup 'etoki-backup.db'"
```

**`etoki.db` だけを `cp` しないでください。** DB は WAL で開いているので、
まだ本体に書き戻されていない書き込みが `etoki.db-wal` に残っています。
戻すときは etoki を止め、残っている `-wal` / `-shm` を消してから、取ったファイルを
`ETOKI_DB_PATH` に置きます。

### 偽の GitHub / LLM で通しを確かめる

鍵もネットワークも無しで、画面から GitHub への書き込みまでを 1 本通せます。
各層の単体テストと E2E（バックエンドを起動しない）では、この並び全体を
通していないためです（[ADR 0050](docs/adr/0050-walk-through-against-fake-upstream.md)）。
同じ起動の上で、保存や作成の往復だけは数本の E2E（`make test-e2e-stack`）が
CI で PR ごとに通します（[ADR 0097](docs/adr/0097-stack-e2e-in-ci.md)）。

```sh
make try-fake   # 偽の上流（:8090）と、それに向けた etoki（:8080）を起動する
```

`.env` は読まず、DB は起動のたびに一時ディレクトリへ作り直します。止めると
消えます。ポートは `ETOKI_ADDR` と `FAKE_ADDR=127.0.0.1:18090` のように変え
られます。

通す筋道:

1. <http://127.0.0.1:8080> を開き、作成先に `fake-owner/fake-repo` の
   `#1 Fake Project` を選んでボードを作る
2. 文字を書き、フレームツール（F）で囲み、そのフレームを選んで「〈名前〉を絵にする」を押す
3. 右上の「絵解き」を押し、開いた面で「絵解く」→「GitHub に作成する」。
   状態が「作成済み」になる。未保存なら「絵解き」が保存してから開く
4. 偽の GitHub に積まれたものを見る。epic と issue、`Kind` と `Parent` が入る

   ```sh
   curl -s http://127.0.0.1:8090/_fake/items
   ```

5. 囲みの文字を書き換えて、右上の「保存」（か `⌘/Ctrl+S`）で保存する。状態が
   「変更あり」になる
6. もう一度絵解くと、項目に「更新」が付く。作成すると 4 の item の
   `title` が変わり、`updates` が増える

対象外のもの:

- **図のドラフト生成。** 偽の LLM は解釈と同じ JSON を返すので、画面では
  失敗します。
- **GitHub App の構成。** 認証の変数は外して起動します。
- **本物の GitHub と一致していること。** 偽物が合わせているのは etoki の
  アダプタが送るリクエストだけです。

### 鍵を `.env` に置く

毎回 `export` する代わりに、`.env` に置けます。雛形が `.env.example` にあります。

```sh
cp .env.example .env
$EDITOR .env
make dev
```

読むのは `make dev` / `make dev-api` だけです。**etoki のバイナリは環境変数しか
見ません。** 設定ファイルを読ませると、`cmd/etoki` を写して独自のアダプタを
差し込む使い方に、その前提まで付いていくためです。

そのため `etoki claim` のように make を通さず叩くコマンドには効きません。
必要なら自分でシェルに読み込ませてください。

```sh
set -a; . ./.env; set +a
go run ./cmd/etoki claim <login>
```

`.env` は gitignore 済みです。**`.env.example` に実際の値を書かないでください。**
こちらは追跡されます。

### Anthropic API を使う

```sh
export ETOKI_LLM_API_KEY=sk-ant-...
make dev
```

### ローカル LLM を使う

etoki が話すのは **Anthropic Messages API 形状**です（`POST {BASE_URL}/v1/messages`）。
ローカルのモデルに繋ぐには、その形状で受けられるプロキシを間に置き、そちらへ
向けます。認証を要求しないなら API キーは未設定のままで構いません。手元で
開発するときの道具です（[ADR 0108](docs/adr/0108-single-user-cloud-tool.md)）。

```sh
export ETOKI_LLM_BASE_URL=http://localhost:4000
export ETOKI_LLM_MODEL=<プロキシ側のモデル名>
make dev
```

LM Studio は 0.4.1 から Anthropic 互換の `POST /v1/messages` も公開しています。
ただし etoki が読む応答のフィールド（`content`・`stop_reason`・`usage`）まで
揃うかは確かめていないので、直接つなげるとは言い切れません。つなげて動かない
ときは、モデル ID（`ETOKI_LLM_MODEL`）を LM Studio 側の名前に合わせたうえで、
形状を変換するプロキシを挟むか、`port.LLMClient` を自前実装して差し込んで
ください。それより前の版は OpenAI Chat Completions 形状だけなので、直接は
動きません。Ollama は
Messages API 形状（`/v1/messages`）も公開していて、0.34.4 では
`ETOKI_LLM_BASE_URL=http://localhost:11434` で動くことを確かめています。

**Ollama はコンテキスト長が小さいと、応答が途中で切れます。** 既定は VRAM に
応じて 4k / 32k / 256k で、4k だと入力と出力の合計が 4096 トークンに達した時点で
`stop_reason` が `max_tokens` になります（etoki が送る上限の 16000 には届いて
いなくても）。思考するモデルは思考だけで出力を使い切り、4k では 1 回目から
切れることがあります（下の「思考するモデルと待ち時間」）。解釈は再送のたびに
入力が増えるので、思考しなくても 4k では 2 回目で切れやすくなります。「LLM の応答が
途中で打ち切られました」と出て、詳細に `stopped at N output tokens, before our limit`
とあれば、Ollama 側で広げてください。

```sh
OLLAMA_CONTEXT_LENGTH=16384 ollama serve
```

これは**新しい `ollama serve` を起動するだけ**です。Ollama のアプリや Linux の
サービスがすでに `11434` を使っていると、bind に失敗し、動いているサーバーの
コンテキスト長も変わりません。その場合は、先にそのサーバーを止めてから上のコマンドを
実行するか、起動設定に環境変数を足して再起動してください（macOS のアプリは
`launchctl setenv OLLAMA_CONTEXT_LENGTH 16384` のあとアプリを再起動、Linux の
systemd は `systemctl edit ollama.service` で開いた drop-in に、`[Service]` の
見出しを付けて `Environment="OLLAMA_CONTEXT_LENGTH=16384"` を書き、
`systemctl restart ollama`）。見出しが無いと環境変数は適用されません。

#### 思考するモデルと待ち時間

**思考に対応したモデルは、頼まなくても思考します。** 短い答えにも思考のぶんの
時間とトークンがかかります。etoki は既定では `thinking` を送らず、思考するかは
モデルに任せます（Anthropic のモデルは、明示的に切ると本文にタグが混ざることが
あるため）。手元の LLM で思考を切らせるには次を設定します。

```sh
export ETOKI_LLM_THINKING=disabled
```

`thinking: {"type": "disabled"}` を送ります。Ollama の Anthropic 互換の口は、
これを思考を切る指示として受けます。思考を切れないモデルは、そのモデルの
いちばん低い段階で思考します（Ollama の文書）。**接続先が Anthropic
（`api.anthropic.com`）のときは、起動時に断ります。**

**1 回の呼び出しは既定で 5 分まで待ちます。** 手元の LLM ではこれを超えることが
あります。超えると「LLM を呼び出せませんでした」と出て、詳細に
`no response within 5m0s` とあります。延ばすには次を設定します。

```sh
export ETOKI_LLM_TIMEOUT=20m
```

解釈と図のドラフト生成は、出力の形が違うと直させるために呼び直すので、1 回の
実行はその回数ぶんまで待ちます。待ちきれなかった呼び出しは呼び直しません。

確かめたこと（2026-11-13）: Apple M1 / メモリ 16GB、Ollama 0.40.16、
`qwen3-vl:8b`（画像と思考に対応）で、付箋 6 枚と画像 1 枚の囲みを 1 回ずつ
解釈しました。コンテキスト長 4096 では、216 秒かけて思考だけで出力を使い切り、
打ち切られました。16384 に広げると、5 分の上限に当たって切れました。どのモデルと
コンテキスト長を勧めるかは、まだ測っていません。

### GitHub Projects v2 を使う

draft issue の作成には classic PAT が要ります。scope は **`repo`** と
**`project`** です。

```sh
export ETOKI_GITHUB_TOKEN=ghp_...
```

**ログイン（後述）を設定しても、GitHub はこの PAT 1 本で叩きます**
（[ADR 0106](docs/adr/0106-github-with-one-pat.md)）。`repo` はリポジトリの一覧に
使います。無いと選択肢が 1 件も出ません。classic にしているのは、個人アカウント
所有の Projects v2 を扱えるのが classic PAT だけだからです。org / リポジトリ所有の
Project だけを使うなら、fine-grained PAT（`Metadata: Read-only` と
`Projects: Read and write`）でも動きます。

作成先の候補に出るのは、PAT の持ち主が見えるリポジトリです。候補が多いときは途中で
一覧を打ち切り、打ち切ったことを選択の画面に出します。**打ち切った先に何が残って
いるかはサーバーにも分かりません**（辿るのをやめているため）。目当てが出ないときは
絞り込みで探してください（[ADR 0054](docs/adr/0054-report-repository-list-truncation.md)）。

**作成先は環境変数では指定しません。** ボードごとに画面で選びます
（[ADR 0014](docs/adr/0014-board-scoped-github-target.md)）。ボードを開くと
リポジトリと、そこに紐づく Projects v2 を選ぶ画面が出ます。draft issue は
リポジトリではなく Project に属するので、2 段で選ぶことになります。

選んだ作成先は、**そのボードで最初の draft issue を作った時点で固定されます。**
`sync_runs` は GitHub 側に残っている item の追跡表であり、作成先が変わると
記録が指す先を見失うためです。

**プロジェクト側にカスタムフィールドを 2 つ用意してください。** draft issue には
ラベルを付けられず native な親子関係も持てないため、種別と親はカスタム
フィールドで表します（[ADR 0006](docs/adr/0006-two-level-hierarchy.md)）。

| フィールド | 種類     | 内容                       |
| ---------- | -------- | -------------------------- |
| `Kind`     | 単一選択 | 選択肢に `epic` と `issue` |
| `Parent`   | テキスト | 親 epic のタイトルが入る   |

名前は `ETOKI_GITHUB_KIND_FIELD` / `ETOKI_GITHUB_PARENT_FIELD` で変えられます。
足りない場合、作成は実行せずに何を作ればよいかを返します。黙って作ると種別も
親子も無い draft issue が並ぶだけになるためです。

作成先の Project を選んだときにも確かめて、足りないものを見せます
（[ADR 0088](docs/adr/0088-check-project-fields-when-choosing.md)）。**選ぶのは
止めません。** 先にブレストを始めて、フィールドは作成までに足せば足ります。

### GitHub App でログインする

GitHub の画面でログインさせ、ログインしていないリクエストを 401 にできます
（[ADR 0015](docs/adr/0015-pluggable-auth-seams.md)）。**App が決めるのは誰で
あるかだけです。** GitHub への読み書きは、ログインを設定しても
`ETOKI_GITHUB_TOKEN` の PAT で行います（[ADR 0106](docs/adr/0106-github-with-one-pat.md)）。
ログインで GitHub から受け取ったトークンは、誰であるかを確かめたら捨て、保存も
しません。

**ログインできるのは 1 人だけです。** `ETOKI_ALLOWED_GITHUB_USER_ID` に渡した
GitHub の利用者だけが通り、それ以外の人は GitHub で認可を済ませても etoki に
入れません（[ADR 0105](docs/adr/0105-only-the-owner-signs-in.md)）。自分用に動かす
道具として、URL を知った人に LLM を叩かせないためです。login ではなく数値の ID で
渡します。login は改名で変わり、空いた login は別の人が取れます。数値の ID は
`https://api.github.com/users/<あなたの login>` の `id` で分かります。

別のアカウントでログインすると、ログインの画面に「このアカウントでは etoki を
使えません」と出ます。GitHub のアカウントを切り替えてからやり直してください。
切り替えずに押し直すと、GitHub が同じアカウントで通すので同じところへ戻ります。

1. [GitHub App を作る](https://github.com/settings/apps/new)
   - **Callback URL**: ブラウザがいるポートに合わせる。`make dev` と
     `make start` での値は[上の表](#dev-サーバーを使わずに動かす)
   - **Repository permissions**: `Metadata: Read-only`
   - Webhook は要りません（Active のチェックを外す）
2. 自分のアカウントにインストールする。GitHub への読み書きは PAT で行うので、
   どのリポジトリを許可しても作成先の候補は変わりません。
3. 環境変数を設定する。

```sh
export ETOKI_GITHUB_APP_CLIENT_ID=Iv23li...
export ETOKI_GITHUB_APP_CLIENT_SECRET=...
export ETOKI_ALLOWED_GITHUB_USER_ID=1234567     # あなたの GitHub の数値 ID
export ETOKI_PUBLIC_URL=http://127.0.0.1:5173   # make dev のとき（上の表）
```

**揃っていなければ起動しません。** client ID と secret の片方だけ、通す利用者が
無い、その値が数値の ID の形でない、通す利用者だけを書いて App を設定していない、
のどれでも起動時に落ちます。

以前の版は、ログインした人ごとの GitHub のトークンを暗号化して DB に持って
いました。`make migrate` で表ごと消えます。`ETOKI_TOKEN_ENCRYPTION_KEY` は
もう読まないので外してください。消したトークンは GitHub 側ではまだ有効なので、
気になるなら GitHub の Settings → Applications → Authorized GitHub Apps から
App の認可を取り消してください（次にログインするとき認可し直します）。

#### 認証を有効にすると、それ以前のボードは見えなくなる

ボードには所有者があります（[ADR 0016](docs/adr/0016-boards-have-owners.md)）。
認証を設定する前に作ったボードは所有者を持たないので、有効にした時点で画面から
消えます。**消えているだけで、消えてはいません。** 起動時に警告が出ます。

```sh
etoki claim <あなたの GitHub login>
```

引き受けるには、その login で **一度ログインしている必要があります**（利用者の
記録はログイン時に作られるため）。初回ログインした人に自動で寄せないのは、
共有サーバーで先に入った人が全部を持っていく決まり方を説明できないためです。

実行すると、引き当てた相手（表示名・login・ID・最終ログイン）を出して `y/N` で
確かめます。スクリプトから呼ぶなど端末でないときは `--yes` を付けてください。

#### ボードを共有する

**いまは招待できる相手がいません。** ログインできるのは 1 人だけなので（上）、
招待の前提になる「一度ログインした相手」が自分のほかに居ません。共有の仕組みは
外す予定です。

ボードは作った人（オーナー）が招待した相手にだけ見えます。メンバーでないボードは
ID を知っていても 404 になります（[ADR 0017](docs/adr/0017-board-sharing-and-permissions.md)）。

ロールは 3 つです。**招待される側にリポジトリへのアクセス権は要りません。**
ブレストに呼ぶ相手と、GitHub に書ける相手は同じではないためです。

|                              | オーナー | 編集できる | 読むだけ |
| ---------------------------- | -------- | ---------- | -------- |
| 閲覧・絵の状態・メンバー一覧 | ✓        | ✓          | ✓        |
| シーンの保存                 | ✓        | ✓          |          |
| 絵解き                       | ✓        | ✓          |          |
| draft issue の作成           | ✓ ※      | ✓ ※        |          |
| ボードの改名                 | ✓        | ✓          |          |
| ボードの背景の書き換え       | ✓        | ✓          |          |
| 作成先の変更                 | ✓        |            |          |
| 招待・解除・ロール変更       | ✓        |            |          |
| ボードの削除                 | ✓        |            |          |

※ **作成できるかを最終的に決めるのは GitHub です。** etoki は設定した PAT で
叩くので、GitHub が見るのは押した人ではなく PAT の権限です。PAT がその Project に
書けなければ、GitHub 側が拒みます。画面には
「この Project に書き込む権限がありません」と理由が出て、ブレストと絵解きは
そのまま続けられます。

メンバーの一覧と招待は、キャンバスのメニューの「共有…」から開きます。

ボードの背景（絵解きのたびに LLM へ添える文、[ADR 0093](docs/adr/0093-board-premise-for-interpretation.md)）は、
キャンバスのメニューの「ボードの背景…」から開きます。読むだけの人も中身は
読めます。

招待できるのは **一度 etoki にログインしたことがある相手だけ**です。login は
改名で変わるため、未ログインの login 宛に招待を積むと、空いた login を取った
別人に権限が渡ります。

**招待する前に、login が当たった相手（表示名・ID・最終ログイン）が出ます。**
etoki が知っているのは「最後にその login でログインした人」までなので、改名で
空いた login を別の人が取っていないか、そこで確かめてから招待してください
（[ADR 0053](docs/adr/0053-confirm-login-before-granting.md)）。

**ボードを作るには、書き込める Projects v2 が 1 つ以上必要です。** 作成時に
作成先を選ぶので、選択肢が無いとボードを作れません。

#### ボードを削除する

打ち間違えたボードや、作成先を間違えたボードは削除できます。オーナーだけです
（[ADR 0042](docs/adr/0042-delete-boards-after-showing-what-remains.md)）。

**GitHub に作った draft issue は消えません。** etoki からは消せないためです。
消えるのは etoki 側の記録のほうで、その結果、**GitHub に残った draft issue が
どのボードのどの注釈から作られたのかを辿れなくなります。** 削除の確認では、
そのボードから作成したと記録している draft issue の件数を先に出します。何を
失うかを見せてから選ばせる、という形にしてあります。

### MCP クライアントから読む

etoki を起動しておくと、`/mcp` に MCP（Streamable HTTP）の入口が開きます。
Claude Code なら次のように登録します。

```sh
claude mcp add --transport http etoki http://127.0.0.1:8080/mcp
```

**`make dev` では開発サーバー（:5173）に向けます。** 認証ありの構成では、
許可の途中で画面（同意の画面）を開くためです。

```sh
claude mcp add --transport http etoki http://127.0.0.1:5173/mcp
```

公開しているのは読み取りの道具だけです。返す JSON は `/api` と同じ形です。

| 道具                   | 返すもの                                               | 同じ形の API                                           |
| ---------------------- | ------------------------------------------------------ | ------------------------------------------------------ |
| `list_boards`          | ボードの一覧と、注釈の 3 状態の件数（`boards` で包む） | `GET /api/boards`                                      |
| `list_annotations`     | 注釈ごとの 3 状態と、GitHub に作った draft issue       | `GET /api/boards/{id}/annotations`                     |
| `list_annotation_runs` | 注釈 1 つの実行の履歴（`runs` で包む）                 | `GET /api/boards/{id}/annotations/{annotationId}/runs` |

**解釈と作成はできません。** 作るものを選んで手直しするのは、画面の前にいる
開発者の役目のままです（[ADR 0071](docs/adr/0071-mcp-read-only-entrance.md)）。

**認証なしの構成では、許可なしで開きます。** 認証なしで動くのはループバックで
待ち受けるときだけなので（[上](#設定)）、届くのは同じ端末からだけです。

#### GitHub App でログインを要求している構成

**etoki が発行するトークンで `/mcp` を開きます**
（[ADR 0076](docs/adr/0076-mcp-oauth-authorization-server.md)）。登録は上と
同じです。初めて道具を使うときに、クライアントがブラウザで etoki の同意の画面を
開きます。

1. ログインしていなければ、いつもの GitHub のログインを通ります。
2. 同意の画面に、接続しようとしているクライアントの名前と戻り先が出ます。
   **名前はクライアントの自称です。** 自分で始めた接続でなければ許可しないで
   ください。
3. 「許可する」を押すと、クライアントに戻ってトークンが渡ります。

見えるボードは画面と同じです。自分がメンバーのボードだけが読めます。

**接続はいつでも取り消せます。** 画面ではボード一覧の利用者のメニューにある
「MCP の接続」から、サーバーを動かしている人は CLI から取り消します。
取り消したクライアントは許可からやり直しになります。

```sh
etoki grants                     # 全員の接続を一覧する
etoki grants --login alice       # alice の接続だけ
etoki revoke <grant-id>          # 1 件を取り消す（対象を見せて確かめる）
etoki revoke --login alice       # alice の接続をすべて取り消す
```

ログアウトしても接続は残ります。ブラウザを閉じる操作が、手元のクライアントを
切ることにならないようにするためです。

クライアントの登録は、動的登録（RFC 7591）と Client ID Metadata Document の
両方を受けます。後者は同意の画面を開いたときに etoki がクライアントの URL を
取りに行くので、**外へ出ていく通信が 1 種類増えます。** 行き先は公開アドレスの
https だけに絞り、プロキシは使いません。

### 別の基盤に載せ替える

差し替えの継ぎ目は 2 段あります。詳細は
[ADR 0008](docs/adr/0008-llm-swap-seams.md) を参照してください。

| 何が違うか   | 継ぎ目                                             |
| ------------ | -------------------------------------------------- |
| 向き先だけ   | `ETOKI_LLM_BASE_URL`                               |
| 認証・ヘッダ | `llm.Config.HTTPClient` に `RoundTripper` を差す   |
| wire format  | `port.LLMClient` を自前実装して `etoki.New` に渡す |

## ドキュメント

設計判断の記録は [`docs/adr/`](docs/adr/) にあります。

変更を送るときの約束（ブランチ・コミット・PR 本文・レビュー対応）は
[`CONTRIBUTING.md`](CONTRIBUTING.md) にあります。

AI コーディングエージェントに読ませている規約の量と、その測り方は
[`docs/token-budget.md`](docs/token-budget.md) にあります。

## ライセンス

[MIT](LICENSE)
