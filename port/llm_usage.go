package port

import (
	"context"
	"time"
)

// LLMUsageTotal は、ある期間に LLM に払った量の合計（ADR 0102）。
//
// **金額にしない。** モデルごとの単価は外の世界の値で、抱えると差し替える
// たびに古くなる（ADR 0031）。持つのはトークン数と実行の回数まで。
type LLMUsageTotal struct {
	// Executions は実行（解釈 1 回、図のドラフト生成 1 回）の数。1 回の実行の
	// なかの再送は数えない（ADR 0044 の「1 回の解釈は 1 回」と同じ単位）。
	Executions int
	// InputTokens は入力に使ったトークン数。
	InputTokens int
	// OutputTokens は出力に使ったトークン数。
	OutputTokens int
}

// LLMUsageRepository は LLM に払った量を、UTC の日ごとの合計で持つ（ADR 0102）。
//
// 全員の合計の費用の上限（日・月）を見るための数え。**誰が使ったかは持たない。**
// 利用者ごとの台帳ではない。再起動やアイドルの停止で消えないよう、プロセスの外に
// 置く。
type LLMUsageRepository interface {
	// AddLLMUsage は at を UTC に直した日付の合計に、1 回の実行で払ったぶんを
	// 足す。時刻は見ない。
	AddLLMUsage(ctx context.Context, at time.Time, usage LLMUsageTotal) error

	// SumLLMUsage は from の日から to の前日まで（どちらも UTC に直した日付で
	// [from, to)）の合計を返す。積んだ日が無ければゼロ値。
	SumLLMUsage(ctx context.Context, from, to time.Time) (LLMUsageTotal, error)
}
