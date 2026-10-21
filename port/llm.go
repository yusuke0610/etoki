package port

import (
	"context"
	"errors"
)

// LLM の呼び出しが失敗した原因のうち、打ち手が違うもの（ADR 0087）。
//
// **外部の LLMClient 実装は返さなくてよい。** 返さなければ、ユースケース層は
// 原因の分からない失敗として扱い、画面には `llm_unavailable` の文が出る。
// それで動かなくなるものは無く、案内が「設定と接続を確かめる」に寄るだけ。
// 返すなら fmt.Errorf("%w: ...", port.ErrLLMTruncated) のように包み、原因の
// 手掛かりは続く文に載せる（ADR 0005 / 0008）。
//
// **どれも再送で直る問題ではない、とユースケース層は扱う。** 修正指示を
// 添えて投げ直すのはスキーマ違反のときだけで、ここに挙げた失敗では投げ直さない。
var (
	// ErrLLMTruncated は、応答が出力の上限で打ち切られたことを表す。
	//
	// 切ったのは向こうのコンテキスト長か出力の上限で、頼み方では直らない。
	// ローカル LLM では既定のコンテキスト長が小さいと起きる。
	ErrLLMTruncated = errors.New("etoki: llm response truncated")

	// ErrLLMRefused は、モデルが応答を拒んだことを表す。
	//
	// 設定の誤りではない。頼み方か、描いた内容を変える話になる。
	ErrLLMRefused = errors.New("etoki: llm refused the request")

	// ErrLLMOverloaded は、向こうが混んでいるか一時的に応答できないことを表す。
	//
	// レート制限・過負荷・向こう側のエラーが該当する。**etoki 自身の実行の
	// 上限（ADR 0044）とは別物。** 時間をおけば通る見込みがある点は同じだが、
	// 待つ相手が違う。
	ErrLLMOverloaded = errors.New("etoki: llm upstream is busy or failing")
)

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
//
// 失敗の原因は ErrLLMTruncated / ErrLLMRefused / ErrLLMOverloaded で名乗れる。
// **名乗るかどうかは実装の任意**で、名乗らない失敗も同じように扱われる
// （画面の案内が原因に合わせて変わらないだけ。ADR 0087）。
type LLMClient interface {
	Complete(ctx context.Context, req VisionRequest) (VisionResponse, error)
}
