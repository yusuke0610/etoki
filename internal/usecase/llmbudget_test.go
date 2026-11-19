package usecase_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 全員の合計の費用の上限（ADR 0102）。**守りたいのは 4 つ。** 日か月の枠が尽きたら
// LLM を呼ぶ前に断ること、境目は UTC であること、断ったリクエストはどの枠も
// 消費しないこと、払ったぶんは失敗した実行や取り消された実行のぶんも積むこと。

// usageStore は日ごとの合計を持つ LLMUsageRepository。
//
// **範囲で絞る。** 実装と同じく [from, to) の日だけを足す。全部返すフェイクだと、
// 前の日や前の月のぶんで断る実装でも緑になる。
type usageStore struct {
	mu   sync.Mutex
	days map[string]port.LLMUsageTotal
	// sumErr は SumLLMUsage が返す誤り。数えを読めないときに呼ばないことを見る。
	sumErr error
	// addErr は AddLLMUsage が返す誤り。積めなくても結果を返すことを見る。
	addErr error
	// sums は SumLLMUsage が呼ばれた回数。上限を持たない構成で読まないことを見る。
	sums int
	// added は AddLLMUsage に来たもの。ctxErr は、そのときの ctx の Err。
	added []addedUsage
}

type addedUsage struct {
	day    string
	usage  port.LLMUsageTotal
	ctxErr error
}

func newUsageStore() *usageStore {
	return &usageStore{days: map[string]port.LLMUsageTotal{}}
}

func utcDay(t time.Time) string { return t.UTC().Format("2006-01-02") }

// put は at の日に積んであることにする。
func (s *usageStore) put(at time.Time, u port.LLMUsageTotal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.days[utcDay(at)] = u
}

func (s *usageStore) AddLLMUsage(ctx context.Context, at time.Time, u port.LLMUsageTotal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.added = append(s.added, addedUsage{day: utcDay(at), usage: u, ctxErr: ctx.Err()})
	if s.addErr != nil {
		return s.addErr
	}
	// 実物の DB と同じく、切れた ctx では書けない。
	if err := ctx.Err(); err != nil {
		return err
	}
	d := s.days[utcDay(at)]
	d.Executions += u.Executions
	d.InputTokens += u.InputTokens
	d.OutputTokens += u.OutputTokens
	s.days[utcDay(at)] = d
	return nil
}

func (s *usageStore) SumLLMUsage(_ context.Context, from, to time.Time) (port.LLMUsageTotal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sums++
	if s.sumErr != nil {
		return port.LLMUsageTotal{}, s.sumErr
	}
	var out port.LLMUsageTotal
	for day, u := range s.days {
		if day >= utcDay(from) && day < utcDay(to) {
			out.Executions += u.Executions
			out.InputTokens += u.InputTokens
			out.OutputTokens += u.OutputTokens
		}
	}
	return out, nil
}

// addedSoFar は AddLLMUsage に来たものを写して返す。
func (s *usageStore) addedSoFar() []addedUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]addedUsage(nil), s.added...)
}

// budgetLimiter は時計を c に、数えを store にした limiter を返す。
func budgetLimiter(limits usecase.LLMLimits, store *usageStore, c *clock) *usecase.LLMLimiter {
	return usecase.NewLLMLimiter(limits,
		usecase.WithLLMUsageRepository(store), usecase.WithLLMLimiterClock(c.now))
}

// 4 つの上限は、それぞれ届いたら断り、届く前は通す。**届く前には断らない。**
// 次の 1 回がどれだけ使うかは呼ぶまで分からない（ADR 0102）。
func TestLLMBudget_RejectsOnlyOnceTheLimitIsReached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		budget usecase.LLMBudget
		// at は積んであるぶんの日付。月の上限は、今日でない日（月初）に積んで、
		// 日ではなく月を数えていることを見る。
		at   time.Time
		used func(n int) port.LLMUsageTotal
		want error
	}{
		"日の入力": {
			budget: usecase.LLMBudget{DailyInputTokens: 100}, at: now,
			used: func(n int) port.LLMUsageTotal { return port.LLMUsageTotal{InputTokens: n} },
			want: usecase.ErrLLMDailyBudgetExhausted,
		},
		"日の出力": {
			budget: usecase.LLMBudget{DailyOutputTokens: 100}, at: now,
			used: func(n int) port.LLMUsageTotal { return port.LLMUsageTotal{OutputTokens: n} },
			want: usecase.ErrLLMDailyBudgetExhausted,
		},
		"月の入力": {
			budget: usecase.LLMBudget{MonthlyInputTokens: 100}, at: now.AddDate(0, 0, -18),
			used: func(n int) port.LLMUsageTotal { return port.LLMUsageTotal{InputTokens: n} },
			want: usecase.ErrLLMMonthlyBudgetExhausted,
		},
		"月の出力": {
			budget: usecase.LLMBudget{MonthlyOutputTokens: 100}, at: now.AddDate(0, 0, -18),
			used: func(n int) port.LLMUsageTotal { return port.LLMUsageTotal{OutputTokens: n} },
			want: usecase.ErrLLMMonthlyBudgetExhausted,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newUsageStore()
			l := budgetLimiter(usecase.LLMLimits{Budget: tc.budget}, store, &clock{t: now})

			store.put(tc.at, tc.used(99))
			acquire(t, l, "u1")()

			store.put(tc.at, tc.used(100))
			if _, err := l.Acquire(asUser(t.Context(), "u1")); !errors.Is(err, tc.want) {
				t.Fatalf("届いたあとの Acquire() = %v, want %v", err, tc.want)
			}
		})
	}
}

// 日も月も UTC で切り替わる。日本時間の 0 時（UTC の 15 時）では切り替わらない。
func TestLLMBudget_TurnsOverAtUTCMidnight(t *testing.T) {
	t.Parallel()

	lastDay := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)
	for name, budget := range map[string]usecase.LLMBudget{
		"日": {DailyOutputTokens: 100},
		"月": {MonthlyOutputTokens: 100},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newUsageStore()
			store.put(lastDay, port.LLMUsageTotal{OutputTokens: 100})
			// 日本時間の 12 月 1 日 0 時。**時計は日本時間で返す。** 手元の時刻の
			// まま日付を切ると、ここで 12 月に入ってしまう。
			jst := time.FixedZone("JST", 9*60*60)
			c := &clock{t: lastDay.Add(15 * time.Hour).In(jst)}
			l := budgetLimiter(usecase.LLMLimits{Budget: budget}, store, c)

			if _, err := l.Acquire(asUser(t.Context(), "u1")); err == nil {
				t.Fatal("日本時間の 0 時に枠が戻った（UTC ではまだ 11 月 30 日）")
			}

			c.advance(9 * time.Hour) // UTC の 12 月 1 日 0 時
			acquire(t, l, "u1")()
		})
	}
}

// 日の枠に数えるのは今日（UTC）のぶんだけ。昨日までのぶんは、月の枠にだけ数える。
func TestLLMBudget_DailyCountsOnlyToday(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store := newUsageStore()
	store.put(now.AddDate(0, 0, -1), port.LLMUsageTotal{OutputTokens: 100})
	l := budgetLimiter(usecase.LLMLimits{Budget: usecase.LLMBudget{DailyOutputTokens: 100}},
		store, &clock{t: now})

	acquire(t, l, "u1")()
}

// 両方尽きているなら、待つ長い方（月）を言う。日の枠が戻っても使えない。
func TestLLMBudget_ReportsTheMonthBeforeTheDay(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store := newUsageStore()
	store.put(now, port.LLMUsageTotal{InputTokens: 100})
	l := budgetLimiter(usecase.LLMLimits{Budget: usecase.LLMBudget{
		DailyInputTokens: 100, MonthlyInputTokens: 100,
	}}, store, &clock{t: now})

	if _, err := l.Acquire(asUser(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrLLMMonthlyBudgetExhausted) {
		t.Fatalf("Acquire() = %v, want ErrLLMMonthlyBudgetExhausted", err)
	}
}

// **合計の枠で断ったリクエストは、利用者ごとの枠を消費しない**（ADR 0044 / 0102）。
// 回数の枠で見る（同時実行は返せば戻るので、順番を崩しても気づけない）。
func TestLLMBudget_RejectedCallDoesNotConsumeRate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store := newUsageStore()
	store.put(now, port.LLMUsageTotal{OutputTokens: 100})
	l := budgetLimiter(usecase.LLMLimits{
		RateLimit: 1,
		Budget:    usecase.LLMBudget{DailyOutputTokens: 100},
	}, store, &clock{t: now})

	if _, err := l.Acquire(asUser(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrLLMDailyBudgetExhausted) {
		t.Fatalf("Acquire() = %v, want ErrLLMDailyBudgetExhausted", err)
	}

	// 枠が戻ったら（運用者が上限を上げたのと同じ）、回数の枠はまるごと残っている。
	store.put(now, port.LLMUsageTotal{})
	acquire(t, l, "u1")()
}

// 数えを読めなければ断る。読めないまま通すと、予算を超えて払う。
func TestLLMBudget_RefusesWhenUsageCannotBeRead(t *testing.T) {
	t.Parallel()

	store := newUsageStore()
	store.sumErr = errors.New("database is locked")
	l := budgetLimiter(usecase.LLMLimits{Budget: usecase.LLMBudget{MonthlyInputTokens: 100}},
		store, &clock{t: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)})

	if _, err := l.Acquire(asUser(t.Context(), "u1")); err == nil {
		t.Fatal("数えを読めないのに通した")
	}
}

// 上限を持たない構成では読まない。既定の構成に、実行ごとの読み込みを足さない。
func TestLLMBudget_UnsetBudgetDoesNotRead(t *testing.T) {
	t.Parallel()

	store := newUsageStore()
	store.sumErr = errors.New("database is locked")
	l := budgetLimiter(usecase.LLMLimits{}, store, &clock{t: time.Now()})

	acquire(t, l, "u1")()
	if store.sums != 0 {
		t.Errorf("SumLLMUsage を %d 回呼んだ, want 0", store.sums)
	}
}

// 枠が尽きていたら、**LLM を 1 回も呼ばず、ジョブも作らない**（ADR 0044 と同じ）。
// 本番が通る口（InterpretationJobs.Start）で見る。
func TestJobs_BudgetExhaustedDoesNotCallLLM(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	store := newUsageStore()
	store.put(now, port.LLMUsageTotal{OutputTokens: 100})
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	jobs := usecase.NewInterpretationJobs(usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm,
		budgetLimiter(usecase.LLMLimits{Budget: usecase.LLMBudget{DailyOutputTokens: 100}},
			store, &clock{t: now})))

	_, err := jobs.Start(t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrLLMDailyBudgetExhausted) {
		t.Fatalf("Start() = %v, want ErrLLMDailyBudgetExhausted", err)
	}
	if len(llm.requests) != 0 {
		t.Errorf("LLM 呼び出し回数 = %d, want 0", len(llm.requests))
	}
	if list, err := jobs.List(t.Context(), "board-1"); err != nil || len(list) != 0 {
		t.Errorf("List() = %v, %v, want 空", list, err)
	}
}

// 1 回の実行で払ったぶんを積む。**再送のぶんも、失敗した実行のぶんも。** どちらも
// 課金されている（ADR 0031）。解釈と図のドラフト生成のどちらも積む（片方だけ
// 積むと、合計の上限をもう片方が素通りする）。
func TestJobs_SpendWhatEachExecutionUsed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	usages := []port.Usage{
		{InputTokens: 100, OutputTokens: 10},
		{InputTokens: 120, OutputTokens: 12},
		{InputTokens: 140, OutputTokens: 14},
	}

	for name, tc := range map[string]struct {
		run  func(t *testing.T, limits *usecase.LLMLimiter)
		want port.LLMUsageTotal
	}{
		"解釈（1 回目が直されて 2 回目で通る）": {
			run: func(t *testing.T, limits *usecase.LLMLimiter) {
				llm := &fakeLLM{responses: []string{invalidLLMOutput, validLLMOutput}, usages: usages}
				svc := usecase.NewInterpretationService(
					&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm, limits)
				if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
					t.Fatalf("interpret() = %v", err)
				}
			},
			want: port.LLMUsageTotal{Executions: 1, InputTokens: 220, OutputTokens: 22},
		},
		"解釈（上限まで直らずに失敗する）": {
			run: func(t *testing.T, limits *usecase.LLMLimiter) {
				llm := &fakeLLM{responses: []string{invalidLLMOutput}, usages: usages}
				svc := usecase.NewInterpretationService(
					&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm, limits,
					usecase.WithMaxAttempts(3))
				if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err == nil {
					t.Fatal("interpret() = nil, want error")
				}
			},
			want: port.LLMUsageTotal{Executions: 1, InputTokens: 360, OutputTokens: 36},
		},
		"図の生成": {
			run: func(t *testing.T, limits *usecase.LLMLimiter) {
				jobs := usecase.NewDiagramJobs(usecase.NewDiagramService(
					&fakeBoards{board: newBoard(interpretScene)},
					&fakeLLM{responses: []string{validMermaid}, usages: usages}, limits))
				job, err := jobs.Start(t.Context(), "board-1", req("段取り"))
				if err != nil {
					t.Fatalf("Start() = %v", err)
				}
				if job = waitDone(t, func() (usecase.DiagramJob, error) {
					return jobs.Get(t.Context(), "board-1", job.ID)
				}); job.Status != usecase.JobSucceeded {
					t.Fatalf("図の生成 = %v, %v", job.Status, job.Err)
				}
			},
			want: port.LLMUsageTotal{Executions: 1, InputTokens: 100, OutputTokens: 10},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := newUsageStore()
			tc.run(t, budgetLimiter(usecase.LLMLimits{}, store, &clock{t: now}))

			added := store.addedSoFar()
			if len(added) != 1 {
				t.Fatalf("積んだ回数 = %d, want 1（%+v）", len(added), added)
			}
			if added[0].usage != tc.want || added[0].day != "2026-11-19" {
				t.Errorf("積んだもの = %s %+v, want 2026-11-19 %+v",
					added[0].day, added[0].usage, tc.want)
			}
		})
	}
}

// 積めなくても結果は返す。払ったものは戻らないので、結果まで捨てると払ったぶんが
// 丸ごと無駄になる（ADR 0102）。
func TestJobs_ResultSurvivesWhenUsageCannotBeRecorded(t *testing.T) {
	t.Parallel()

	store := newUsageStore()
	store.addErr = errors.New("database is locked")
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{},
		&fakeLLM{responses: []string{validLLMOutput}, usages: []port.Usage{{InputTokens: 10}}},
		budgetLimiter(usecase.LLMLimits{}, store, &clock{t: time.Now()}))

	got, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v, want 積めなくても結果を返す", err)
	}
	if len(got.Interpretation.Items) == 0 {
		t.Error("解釈の結果が空")
	}
	if len(store.addedSoFar()) != 1 {
		t.Errorf("積もうとした回数 = %d, want 1", len(store.addedSoFar()))
	}
}

// 取り消された実行のぶんも積む。**払ったぶんは戻らない。** 取り消しで切れた ctx の
// まま書くと、DB に届かずに数えから消える（run の記録と同じ、ADR 0051）。
func TestJobs_SpendEvenWhenTheJobIsDiscarded(t *testing.T) {
	t.Parallel()

	store := newUsageStore()
	llm := newGatedLLM(validLLMOutput)
	jobs := usecase.NewInterpretationJobs(usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene), owner: "u1"}, &fakeMappings{}, llm,
		budgetLimiter(usecase.LLMLimits{}, store, &clock{t: time.Now()}),
		usecase.WithMaxAttempts(1)))
	ctx := asUser(t.Context(), "u1")

	job, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	<-llm.started
	if err := jobs.Discard(ctx, "board-1", job.ID); err != nil {
		t.Fatalf("Discard() = %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for len(store.addedSoFar()) == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	added := store.addedSoFar()
	if len(added) != 1 {
		t.Fatalf("積んだ回数 = %d, want 1", len(added))
	}
	if added[0].ctxErr != nil {
		t.Errorf("取り消しで切れた ctx のまま積もうとした: %v", added[0].ctxErr)
	}
	if added[0].usage.Executions != 1 {
		t.Errorf("積んだ実行 = %d, want 1", added[0].usage.Executions)
	}
}
