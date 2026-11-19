package sqlite_test

import (
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// 日ごとに積み、範囲で引く（ADR 0102）。**範囲は [from, to)。** 月の合計を
// 「翌月 1 日の前日まで」で引くので、to の日を含めると翌月 1 日のぶんが混ざる。
func TestLLMUsage_AddsPerDayAndSumsAHalfOpenRange(t *testing.T) {
	t.Parallel()

	repo := sqlite.NewLLMUsageRepository(newDB(t))
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }

	for _, c := range []struct {
		at time.Time
		u  port.LLMUsageTotal
	}{
		{day(1), port.LLMUsageTotal{Executions: 1, InputTokens: 100, OutputTokens: 10}},
		// 同じ日は足す。時刻は見ない。
		{day(1).Add(23 * time.Hour), port.LLMUsageTotal{Executions: 1, InputTokens: 200, OutputTokens: 20}},
		{day(2), port.LLMUsageTotal{Executions: 1, InputTokens: 400, OutputTokens: 40}},
		{day(3), port.LLMUsageTotal{Executions: 2, InputTokens: 800, OutputTokens: 80}},
	} {
		if err := repo.AddLLMUsage(t.Context(), c.at, c.u); err != nil {
			t.Fatalf("AddLLMUsage(%s) = %v", c.at, err)
		}
	}

	for name, tc := range map[string]struct {
		from, to time.Time
		want     port.LLMUsageTotal
	}{
		"1 日だけ":      {day(1), day(2), port.LLMUsageTotal{Executions: 2, InputTokens: 300, OutputTokens: 30}},
		"to の日は含めない": {day(1), day(3), port.LLMUsageTotal{Executions: 3, InputTokens: 700, OutputTokens: 70}},
		"積んだ日が無い":    {day(10), day(11), port.LLMUsageTotal{}},
	} {
		got, err := repo.SumLLMUsage(t.Context(), tc.from, tc.to)
		if err != nil {
			t.Fatalf("%s: SumLLMUsage = %v", name, err)
		}
		if got != tc.want {
			t.Errorf("%s: SumLLMUsage = %+v, want %+v", name, got, tc.want)
		}
	}
}

// 日付は UTC で決める。日本時間の 11 月 2 日 8 時は、UTC ではまだ 11 月 1 日。
func TestLLMUsage_UsesTheUTCDay(t *testing.T) {
	t.Parallel()

	repo := sqlite.NewLLMUsageRepository(newDB(t))
	jst := time.FixedZone("JST", 9*60*60)
	if err := repo.AddLLMUsage(t.Context(),
		time.Date(2026, 11, 2, 8, 0, 0, 0, jst), port.LLMUsageTotal{Executions: 1, InputTokens: 5}); err != nil {
		t.Fatalf("AddLLMUsage = %v", err)
	}

	got, err := repo.SumLLMUsage(t.Context(),
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("SumLLMUsage = %v", err)
	}
	if got.InputTokens != 5 {
		t.Errorf("11/1（UTC）の入力 = %d, want 5", got.InputTokens)
	}
}

// 同時に終わった実行のぶんが、互いを上書きしない。読んでから書く形だと、
// 片方の足し算が消える。
func TestLLMUsage_ConcurrentAddsDoNotLoseUsage(t *testing.T) {
	t.Parallel()

	repo := sqlite.NewLLMUsageRepository(newDB(t))
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Go(func() {
			errs <- repo.AddLLMUsage(t.Context(), day,
				port.LLMUsageTotal{Executions: 1, InputTokens: 3, OutputTokens: 1})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AddLLMUsage = %v", err)
		}
	}

	got, err := repo.SumLLMUsage(t.Context(), day, day.AddDate(0, 0, 1))
	if err != nil {
		t.Fatalf("SumLLMUsage = %v", err)
	}
	if want := (port.LLMUsageTotal{Executions: n, InputTokens: 3 * n, OutputTokens: n}); got != want {
		t.Errorf("SumLLMUsage = %+v, want %+v", got, want)
	}
}
