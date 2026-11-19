package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// usageDayLayout は llm_usage_days.day の書き方。固定幅なので、文字列の範囲で
// 引くと日付の範囲になる（マイグレーション 0019）。
const usageDayLayout = "2006-01-02"

// LLMUsageRepository は port.LLMUsageRepository の SQLite 実装（ADR 0102）。
type LLMUsageRepository struct {
	db *sql.DB
}

// NewLLMUsageRepository は LLMUsageRepository を作る。
func NewLLMUsageRepository(db *sql.DB) *LLMUsageRepository {
	return &LLMUsageRepository{db: db}
}

var _ port.LLMUsageRepository = (*LLMUsageRepository)(nil)

// usageDay は時刻を UTC の日付に丸めて書く。
func usageDay(t time.Time) string { return t.UTC().Format(usageDayLayout) }

// AddLLMUsage は at の日（UTC）の合計に足す。**読んでから書かない。** 1 文で
// 足すので、同時に終わった実行のぶんが互いを上書きしない。
func (r *LLMUsageRepository) AddLLMUsage(
	ctx context.Context, at time.Time, u port.LLMUsageTotal,
) error {
	if _, err := r.db.ExecContext(ctx,
		`INSERT INTO llm_usage_days (day, executions, input_tokens, output_tokens)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (day) DO UPDATE SET
		   executions    = executions    + excluded.executions,
		   input_tokens  = input_tokens  + excluded.input_tokens,
		   output_tokens = output_tokens + excluded.output_tokens`,
		usageDay(at), u.Executions, u.InputTokens, u.OutputTokens); err != nil {
		return fmt.Errorf("add llm usage for %s: %w", usageDay(at), err)
	}
	return nil
}

// SumLLMUsage は [from, to) の日の合計を返す。
func (r *LLMUsageRepository) SumLLMUsage(
	ctx context.Context, from, to time.Time,
) (port.LLMUsageTotal, error) {
	var out port.LLMUsageTotal
	if err := r.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(executions), 0), COALESCE(SUM(input_tokens), 0),
		        COALESCE(SUM(output_tokens), 0)
		 FROM llm_usage_days WHERE day >= ? AND day < ?`,
		usageDay(from), usageDay(to),
	).Scan(&out.Executions, &out.InputTokens, &out.OutputTokens); err != nil {
		return port.LLMUsageTotal{}, fmt.Errorf(
			"sum llm usage from %s to %s: %w", usageDay(from), usageDay(to), err)
	}
	return out, nil
}
