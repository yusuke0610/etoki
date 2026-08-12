# etoki（絵解き）

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

## 開発

[Nix](https://nixos.org/) があれば他に何もインストールする必要はありません。

```sh
nix develop      # 開発用シェルに入る（Go / Bun / SQLite / lint などが揃う）
make setup       # 依存関係の取得と DB 初期化
make dev         # バックエンドとフロントエンドを同時に起動
make test        # Go とフロントエンドのテスト
make test-e2e    # Playwright による E2E テスト
make help        # ターゲット一覧
```

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

環境変数で設定します。**LLM を設定しなくても起動します。** その場合、解釈の
エンドポイントだけが「設定されていない」と返し、ボードの編集と注釈の状態表示は
そのまま使えます。ブレストだけ先にやる、という使い方を潰さないためです。

| 変数 | 既定値 | 用途 |
| --- | --- | --- |
| `ETOKI_ADDR` | `127.0.0.1:8080` | リッスンアドレス |
| `ETOKI_ALLOWED_ORIGINS` | （なし） | 追加で許すオリジン（カンマ区切り）。ループバックは常に許す |
| `ETOKI_DB_PATH` | `etoki.db` | SQLite ファイルのパス |
| `ETOKI_LLM_BASE_URL` | `https://api.anthropic.com` | LLM のエンドポイント |
| `ETOKI_LLM_API_KEY` | （なし） | LLM の API キー。認証不要なら未設定でよい |
| `ETOKI_LLM_MODEL` | `claude-opus-5` | モデル ID |
| `ETOKI_GITHUB_TOKEN` | （なし） | GitHub のトークン。認証を設定した場合は使わない |
| `ETOKI_GITHUB_APP_CLIENT_ID` | （なし） | GitHub App の client ID。設定するとログインを要求する |
| `ETOKI_GITHUB_APP_CLIENT_SECRET` | （なし） | 同 client secret |
| `ETOKI_TOKEN_ENCRYPTION_KEY` | （なし） | 保存するトークンの暗号化鍵（base64 の 32 バイト） |
| `ETOKI_PUBLIC_URL` | （なし） | 認可から戻る先。空ならリクエストの Host から組む |
| `ETOKI_GITHUB_KIND_FIELD` | `Kind` | 種別のカスタムフィールド名 |
| `ETOKI_GITHUB_PARENT_FIELD` | `Parent` | 親のカスタムフィールド名 |

認証は既定では持ちません。GitHub App を設定するとログインを要求します（後述）。
どちらの構成でも、許可していない Host / Origin を持つブラウザからのリクエストは
拒否します（ADR 0013）。ループバックは常に許可されるので、通常の
使い方では意識する必要はありません。`curl` やスクリプトからの利用にも影響しません。
`ETOKI_ADDR` で公開インターフェースにバインドする場合は、
`ETOKI_ALLOWED_ORIGINS` にそのオリジンを足さないと自分のブラウザからも
届かなくなります。

### Anthropic API を使う

```sh
export ETOKI_LLM_API_KEY=sk-ant-...
make dev
```

### ローカル LLM を使う

etoki が話すのは **Anthropic Messages API 形状**です（`POST {BASE_URL}/v1/messages`）。
ローカルのモデルに繋ぐには、その形状で受けられるプロキシを間に置き、そちらへ
向けます。認証を要求しないなら API キーは未設定のままで構いません。

```sh
export ETOKI_LLM_BASE_URL=http://localhost:4000
export ETOKI_LLM_MODEL=<プロキシ側のモデル名>
make dev
```

Ollama や LM Studio が直接公開するのは OpenAI Chat Completions 形状なので、
`ETOKI_LLM_BASE_URL` をそこへ向けても動きません。形状を変換するプロキシを
挟むか、`port.LLMClient` を自前実装して差し込んでください。

### GitHub Projects v2 を使う

draft issue の作成にはトークンが要ります。

```sh
export ETOKI_GITHUB_TOKEN=ghp_...
```

トークンに必要な権限は **repo の read** と **Projects の read/write** です
（fine-grained PAT なら `Metadata: Read-only` と `Projects: Read and write`）。
repo の read はリポジトリの一覧に使います。無いと選択肢が 1 件も出ません。

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

| フィールド | 種類 | 内容 |
| --- | --- | --- |
| `Kind` | 単一選択 | 選択肢に `epic` と `issue` |
| `Parent` | テキスト | 親 epic のタイトルが入る |

名前は `ETOKI_GITHUB_KIND_FIELD` / `ETOKI_GITHUB_PARENT_FIELD` で変えられます。
足りない場合、作成は実行せずに何を作ればよいかを返します。黙って作ると種別も
親子も無い draft issue が並ぶだけになるためです。

### GitHub App でログインする

PAT を手で貼る代わりに、GitHub の画面でログインさせられます。設定すると
**PAT は使わなくなり**、未ログインのリクエストは 401 になります
（[ADR 0015](docs/adr/0015-pluggable-auth-seams.md)）。

OAuth App ではなく **GitHub App** を使います。PAT に求めている粒度をそのまま
要求できるのは GitHub App だけで、OAuth App では全リポジトリの読み書きを
預かることになるためです。

1. [GitHub App を作る](https://github.com/settings/apps/new)
   - **Callback URL**: `http://127.0.0.1:5173/api/auth/callback`
     （`make dev` の場合。ブラウザがいるポートに合わせる）
   - **Permissions**: `Metadata: Read-only` と `Projects: Read and write`
   - Webhook は要りません（Active のチェックを外す）
2. 使いたいリポジトリにインストールする。**候補に出るのはここで許可した
   リポジトリだけ**です。
3. 環境変数を設定する。

```sh
export ETOKI_GITHUB_APP_CLIENT_ID=Iv23li...
export ETOKI_GITHUB_APP_CLIENT_SECRET=...
export ETOKI_TOKEN_ENCRYPTION_KEY=$(head -c 32 /dev/urandom | base64)
export ETOKI_PUBLIC_URL=http://127.0.0.1:5173   # make dev のとき
```

鍵は保存するトークンの暗号化に使います。**未設定だと起動時に落ちます。**
黙って平文で保存しないためです。鍵を変えると保存済みのトークンは開けなくなり、
再ログインが必要になります。

トークンは既定で 8 時間で失効しますが、`refresh_token` で自動更新するので
再ログインは要りません。App の設定で「Expire user authorization tokens」を
切っている場合も動きます。

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

#### ボードを共有する

ボードは作った人（オーナー）が招待した相手にだけ見えます。メンバーでないボードは
ID を知っていても 404 になります（[ADR 0017](docs/adr/0017-board-sharing-and-permissions.md)）。

ロールは 3 つです。**招待される側にリポジトリへのアクセス権は要りません。**
ブレストに呼ぶ相手と、GitHub に書ける相手は同じではないためです。

| | オーナー | 編集できる | 読むだけ |
| --- | --- | --- | --- |
| 閲覧・注釈の状態・メンバー一覧 | ✓ | ✓ | ✓ |
| シーンの保存 | ✓ | ✓ | |
| 注釈の解釈 | ✓ | ✓ | |
| draft issue の作成 | ✓ ※ | ✓ ※ | |
| 作成先の変更 | ✓ | | |
| 招待・解除・ロール変更 | ✓ | | |

※ **作成できるかを最終的に決めるのは GitHub です。** etoki は実行者のトークンで
叩くので、その Project に書けない人の作成は GitHub 側が拒みます。画面には
「この Project に書き込む権限がありません」と理由が出て、ブレストと解釈は
そのまま続けられます。

招待できるのは **一度 etoki にログインしたことがある相手だけ**です。login は
改名で変わるため、未ログインの login 宛に招待を積むと、空いた login を取った
別人に権限が渡ります。

**ボードを作るには、書き込める Projects v2 が 1 つ以上必要です。** 作成時に
作成先を選ぶので、選択肢が無いとボードを作れません。

### 別の基盤に載せ替える

差し替えの継ぎ目は 2 段あります。詳細は
[ADR 0008](docs/adr/0008-llm-swap-seams.md) を参照してください。

| 何が違うか | 継ぎ目 |
| --- | --- |
| 向き先だけ | `ETOKI_LLM_BASE_URL` |
| 認証・ヘッダ | `llm.Config.HTTPClient` に `RoundTripper` を差す |
| wire format | `port.LLMClient` を自前実装して `etoki.New` に渡す |

## ドキュメント

設計判断の記録は [`docs/adr/`](docs/adr/) にあります。

## ライセンス

[MIT](LICENSE)
