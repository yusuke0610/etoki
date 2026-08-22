package port

import "context"

// Image は LLM に渡す画像 1 枚。
type Image struct {
	// MediaType は画像の MIME タイプ。例: "image/png"
	MediaType string
	// Data は画像のバイト列。base64 エンコードは実装側が行う。
	Data []byte
}

// VisionRequest は LLM への 1 回分の入力。
type VisionRequest struct {
	// System はシステム指示。
	System string
	// Text はユーザーメッセージ本文。
	Text string
	// Images は Text に添える画像。空でもよい。
	Images []Image
}

// VisionResponse は LLM からの 1 回分の出力。
type VisionResponse struct {
	// Text はモデルの出力本文。
	Text string
	// Raw は生のレスポンスボディ。デバッグ用であり、永続化はしない。
	Raw []byte
	// Usage はこの呼び出しで使ったトークン数。埋めなくてよい（Usage を参照）。
	Usage Usage
}

// Usage は 1 回の呼び出しで使ったトークン数。
//
// **埋めるかどうかは実装の任意。** ゼロ値のまま返してよく、その場合でも
// 呼び出し側は動く。必須にすると LLMClient を自前実装する負担が増える
// （ADR 0001 / 0008）。トークン数の名前も数え方も基盤ごとに違い、そもそも
// 返さない基盤もあるため。
//
// **読む側は 0 を「報告が無かった」として扱う**（ADR 0031）。0 トークンで
// 返る呼び出しは無いので、実測として 0 と報告なしを区別する必要が無い。
type Usage struct {
	// InputTokens は入力に使ったトークン数。
	InputTokens int
	// OutputTokens は出力に使ったトークン数。
	OutputTokens int
}

// LLMClient は Vision 対応 LLM を 1 回呼び出す。
//
// プロンプト構築・JSON スキーマ検証・修正指示つき再送はユースケース層が持ち、
// この実装には含めない。外部リポジトリが独自実装を差し込む際のコストを
// 最小化するための切り方である。判断の経緯は
// docs/adr/0005-llm-client-abstraction.md を参照。
type LLMClient interface {
	Complete(ctx context.Context, req VisionRequest) (VisionResponse, error)
}
