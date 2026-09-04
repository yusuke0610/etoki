package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// newLimiter は既定の上限（同時実行 1・回数は無制限）の limiter を返す。
//
// 上限そのものを見ないテストが使う。逐次に呼ぶ限り同時実行 1 には当たらない。
func newLimiter() *usecase.LLMLimiter {
	return usecase.NewLLMLimiter(usecase.LLMLimits{})
}

// clock は差し替えた時計。Acquire は now() を自分のロックの中で呼ぶので、
// 進めるのがテストだけならここに排他は要らない。
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// acquire は 1 つ取り、失敗したら落とす。
func acquire(t *testing.T, l *usecase.LLMLimiter, actor string) func() {
	t.Helper()

	release, err := l.Acquire(port.ContextWithUserID(t.Context(), actor))
	if err != nil {
		t.Fatalf("Acquire(%q) = %v", actor, err)
	}
	return release
}

func TestLLMLimiter_RejectsBeyondMaxConcurrent(t *testing.T) {
	t.Parallel()

	l := usecase.NewLLMLimiter(usecase.LLMLimits{MaxConcurrent: 2})

	first := acquire(t, l, "u1")
	second := acquire(t, l, "u1")

	// **待たずに断る。** 待たせると、画面からは止まったのか並んでいるのかが
	// 見えない（ADR 0044）。
	if _, err := l.Acquire(port.ContextWithUserID(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrConcurrencyLimited) {
		t.Fatalf("3 つめの Acquire() = %v, want ErrConcurrencyLimited", err)
	}

	first()
	// 解放すれば通る。走行数は返る。
	acquire(t, l, "u1")()
	second()
}

// 空文字は無効値ではなく「認証なしの操作者」1 人（ADR 0016）。認証を設定して
// いない構成では、この 1 人だけが数えられる。
func TestLLMLimiter_CountsPerUser(t *testing.T) {
	t.Parallel()

	l := usecase.NewLLMLimiter(usecase.LLMLimits{MaxConcurrent: 1})

	defer acquire(t, l, "")()

	// 別の利用者は自分の枠を持つ。誰かの使いすぎで他人が止まらない。
	defer acquire(t, l, "u1")()

	if _, err := l.Acquire(t.Context()); !errors.Is(err, usecase.ErrConcurrencyLimited) {
		t.Fatalf("空文字の 2 つめ = %v, want ErrConcurrencyLimited", err)
	}
}

func TestLLMLimiter_RejectsBeyondRateLimitWithinWindow(t *testing.T) {
	t.Parallel()

	c := &clock{t: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)}
	l := usecase.NewLLMLimiter(
		usecase.LLMLimits{MaxConcurrent: 10, RateLimit: 2, RateWindow: time.Hour},
		usecase.WithLLMLimiterClock(c.now),
	)

	// **解放しても回数は戻らない。** 実行が終わっても呼んだ事実は消えない。
	acquire(t, l, "u1")()
	c.advance(30 * time.Minute)
	acquire(t, l, "u1")()

	if _, err := l.Acquire(port.ContextWithUserID(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrRateLimited) {
		t.Fatalf("3 回目 = %v, want ErrRateLimited", err)
	}

	// 1 回目が窓から出れば 1 つ空く。2 回目（30 分前）はまだ窓の中。
	c.advance(31 * time.Minute)
	acquire(t, l, "u1")()

	if _, err := l.Acquire(port.ContextWithUserID(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrRateLimited) {
		t.Fatalf("窓の中で 3 回目 = %v, want ErrRateLimited", err)
	}
}

// 同時実行で断ったリクエストが回数を消費すると、待って押し直した人が理由も
// なく上限に近づく。見る順を同時実行 → 回数にしてあるのはこのため（ADR 0044）。
func TestLLMLimiter_RejectedCallDoesNotConsumeRate(t *testing.T) {
	t.Parallel()

	l := usecase.NewLLMLimiter(usecase.LLMLimits{MaxConcurrent: 1, RateLimit: 2})

	release := acquire(t, l, "u1")

	// 走っている間は同時実行で断られる。ここで回数を食っていたら、次の 2 回で
	// 上限に達してしまう。
	for range 3 {
		if _, err := l.Acquire(port.ContextWithUserID(t.Context(), "u1")); !errors.Is(
			err, usecase.ErrConcurrencyLimited) {
			t.Fatalf("走行中の Acquire() = %v, want ErrConcurrencyLimited", err)
		}
	}
	release()

	// 消費されているのは 1 回だけ。あと 1 回通ってから回数の上限に当たる。
	acquire(t, l, "u1")()

	if _, err := l.Acquire(port.ContextWithUserID(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrRateLimited) {
		t.Fatalf("3 回目 = %v, want ErrRateLimited", err)
	}
}

// 回数の上限には既定値を置かない（ADR 0044）。同時実行だけが既定で効く。
func TestNewLLMLimiter_Defaults(t *testing.T) {
	t.Parallel()

	l := usecase.NewLLMLimiter(usecase.LLMLimits{})

	defer acquire(t, l, "u1")()

	if _, err := l.Acquire(port.ContextWithUserID(t.Context(), "u1")); !errors.Is(
		err, usecase.ErrConcurrencyLimited) {
		t.Fatalf("既定の 2 つめ = %v, want ErrConcurrencyLimited", err)
	}

	// 回数のほうは既定では効かない。走らせては解放するぶんには何度でも通る。
	for range 20 {
		acquire(t, l, "u2")()
	}
}

// 上限に当たったら **LLM を 1 回も呼ばない。** 呼んでから結果を捨てると、
// 課金だけが発生する（ADR 0044、validateImages を手前に置いてあるのと同じ理由）。
func TestInterpret_RateLimitedDoesNotCallLLM(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm,
		usecase.NewLLMLimiter(usecase.LLMLimits{RateLimit: 1}),
	)

	if _, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("1 回目の Interpret() = %v", err)
	}

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrRateLimited) {
		t.Fatalf("2 回目の Interpret() = %v, want ErrRateLimited", err)
	}
	if len(llm.requests) != 1 {
		t.Errorf("LLM 呼び出し回数 = %d, want 1", len(llm.requests))
	}
}

// 走っている解釈があるあいだは同時実行で断る。枠は外から取っておく。
// 実際に並行させなくても、サービスが同じ limiter を見ていることは確かめられる。
func TestInterpret_ConcurrencyLimitedDoesNotCallLLM(t *testing.T) {
	t.Parallel()

	limits := usecase.NewLLMLimiter(usecase.LLMLimits{MaxConcurrent: 1})
	defer acquire(t, limits, "")()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := usecase.NewInterpretationService(
		&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm, limits)

	_, err := svc.Interpret(t.Context(), "board-1", "annot-1", nil)
	if !errors.Is(err, usecase.ErrConcurrencyLimited) {
		t.Fatalf("Interpret() = %v, want ErrConcurrencyLimited", err)
	}
	if len(llm.requests) != 0 {
		t.Errorf("LLM 呼び出し回数 = %d, want 0", len(llm.requests))
	}
}

// **解釈と図のドラフト生成は同じ枠**（ADR 0044）。片方だけ絞ると、絞っていない
// ほうが抜け道として残る。ここが切れると、合計では上限の倍まで叩ける。
func TestInterpretAndDiagram_ShareOneLimit(t *testing.T) {
	t.Parallel()

	limits := usecase.NewLLMLimiter(usecase.LLMLimits{RateLimit: 1})
	boards := &fakeBoards{board: newBoard(interpretScene)}

	interpretLLM := &fakeLLM{responses: []string{validLLMOutput}}
	interpretations := usecase.NewInterpretationService(
		boards, &fakeMappings{}, interpretLLM, limits)

	diagramLLM := &fakeLLM{responses: []string{validMermaid}}
	diagrams := usecase.NewDiagramService(boards, diagramLLM, limits)

	if _, err := interpretations.Interpret(t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("Interpret() = %v", err)
	}

	_, err := diagrams.Generate(t.Context(), "board-1", req("段取り"))
	if !errors.Is(err, usecase.ErrRateLimited) {
		t.Fatalf("Generate() = %v, want ErrRateLimited", err)
	}
	if len(diagramLLM.requests) != 0 {
		t.Errorf("図の LLM 呼び出し回数 = %d, want 0", len(diagramLLM.requests))
	}
}
