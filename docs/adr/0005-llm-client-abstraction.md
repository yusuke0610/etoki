# 0005. LLMClient は 1 回の呼び出しだけを担う

- ステータス: 採用
- 日付: 2026-07-29

## 文脈

当初案のインターフェースは `Decompose(ctx, DecomposeRequest) (DecomposeResult, error)`
であり、「スキーマ検証 NG なら修正指示を添えて再送」というループも実装側に
含まれる読み方ができた。

しかしこのループを実装側に置くと、

- 外部リポジトリがアダプタを実装するたびに再送ロジックを再実装することになる。
- 再送は純粋ロジックでテストしやすいのに、外部サービス越しでしか検証できなくなる。

また「API キーで叩ける HTTP エンドポイントしか知らない」という方針は、リクエストと
レスポンスの形式（wire format）を決めていない。Anthropic Messages API と
OpenAI Chat Completions は非互換なので、既定実装はどちらかを選ぶ必要がある。

## 決定

インターフェースを 1 回の呼び出しまで降ろす。

```go
type LLMClient interface {
    Complete(ctx context.Context, req VisionRequest) (VisionResponse, error)
}
```

プロンプト構築・JSON スキーマ検証・修正指示つき再送はすべてユースケース層が持つ。

既定実装の wire format は **Anthropic Messages API 形状**とする。

- エンドポイント: `POST {ETOKI_LLM_BASE_URL}/v1/messages`
- ヘッダ: `x-api-key`, `anthropic-version: 2023-06-01`
- 画像: `{"type":"image","source":{"type":"base64","media_type":"image/png","data":...}}`
- 既定モデル: `claude-opus-5`（`ETOKI_LLM_MODEL` で差し替え）

SDK は `go.mod` に入れず、標準の `net/http` だけで実装する。

## 結果

- 外部実装者が書くのは「画像とテキストを送って文字列を受け取る」関数だけになる。
- 再送ループとスキーマ検証をフェイクの `LLMClient` でテストできる。
- OpenAI 互換など別の wire format が必要な場合は、既定実装を使わず `LLMClient` を
  自前実装して差し込む。コア側にプロバイダ分岐は入れない。
- 「HTTP エンドポイントしか知らない」という当初の抽象は、厳密には
  「Anthropic Messages API 互換のエンドポイントしか知らない」に狭まった。
  この差は明示しておく。
