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
| `ETOKI_DB_PATH` | `etoki.db` | SQLite ファイルのパス |
| `ETOKI_LLM_BASE_URL` | `https://api.anthropic.com` | LLM のエンドポイント |
| `ETOKI_LLM_API_KEY` | （なし） | LLM の API キー。認証不要なら未設定でよい |
| `ETOKI_LLM_MODEL` | `claude-opus-5` | モデル ID |

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
