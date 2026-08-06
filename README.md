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
make help        # ターゲット一覧
```

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
| `ETOKI_GITHUB_TOKEN` | （なし） | GitHub のトークン |
| `ETOKI_GITHUB_PROJECT_ID` | （なし） | draft issue を作る Projects v2 の node ID |
| `ETOKI_GITHUB_KIND_FIELD` | `Kind` | 種別のカスタムフィールド名 |
| `ETOKI_GITHUB_PARENT_FIELD` | `Parent` | 親のカスタムフィールド名 |

認証は持ちません。代わりに、ループバック以外から来たブラウザのリクエストを
拒否します（ADR 0013）。`curl` やスクリプトからの利用には影響しません。
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

draft issue の作成には、トークンと作成先のプロジェクトが要ります。

```sh
export ETOKI_GITHUB_TOKEN=ghp_...
export ETOKI_GITHUB_PROJECT_ID=PVT_...
```

トークンに必要な権限は **Projects の read/write** です（fine-grained PAT なら
`Projects: Read and write`）。

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
