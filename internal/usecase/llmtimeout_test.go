package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// slowLLM は決められた時間をかけてから、決められた応答を順に返す LLMClient。
//
// 応答を使い切ったら最後のものを返し続ける（fakeLLM と同じ）。ctx が先に終われば
// そこで返す。実物の HTTP クライアントも、期限の切れた ctx では待つのをやめる。
type slowLLM struct {
	delay     time.Duration
	responses []string

	mu    sync.Mutex
	calls int
	// waits は呼ばれた時点で ctx の期限まで残っていた長さ。期限が無ければ 0。
	waits []time.Duration
}

func (s *slowLLM) Complete(ctx context.Context, _ port.VisionRequest) (port.VisionResponse, error) {
	var wait time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline)
	}

	s.mu.Lock()
	i := min(s.calls, len(s.responses)-1)
	s.calls++
	s.waits = append(s.waits, wait)
	s.mu.Unlock()

	select {
	case <-time.After(s.delay):
		return port.VisionResponse{Text: s.responses[i]}, nil
	case <-ctx.Done():
		return port.VisionResponse{}, ctx.Err()
	}
}

func (s *slowLLM) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *slowLLM) waitList() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.waits)
}

// timedCallers は LLM を叩く 2 つの経路を、呼ぶ回数と 1 回の上限を決めて走らせる。
// 上限の掛け方は llmRetry の 1 箇所にあるので、どちらでも同じでなければならない。
var timedCallers = map[string]struct {
	// invalid は読めずに指摘つきの再送へ回る出力。valid は読める出力。
	invalid, valid string
	run            func(t *testing.T, llm port.LLMClient, attempts int, timeout time.Duration) error
}{
	"解釈": {
		invalid: invalidLLMOutput,
		valid:   validLLMOutput,
		run: func(t *testing.T, llm port.LLMClient, attempts int, timeout time.Duration) error {
			t.Helper()

			svc := usecase.NewInterpretationService(
				&fakeBoards{board: newBoard(interpretScene)}, &fakeMappings{}, llm, newLimiter(),
				usecase.WithMaxAttempts(attempts), usecase.WithCallTimeout(timeout))
			_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
			return err
		},
	},
	"図の生成": {
		invalid: "これは図ではありません",
		valid:   validMermaid,
		run: func(t *testing.T, llm port.LLMClient, attempts int, timeout time.Duration) error {
			t.Helper()

			svc := usecase.NewDiagramService(
				&fakeBoards{board: newBoard(interpretScene)}, llm, newLimiter(),
				usecase.WithDiagramMaxAttempts(attempts), usecase.WithDiagramCallTimeout(timeout))
			_, err := generate(t, svc, t.Context(), "board-1", req("段取り"))
			return err
		},
	},
}

// causeLLM は ctx が終わるまで応えず、終わったら ctx の原因（context.Cause）を
// 包んで返す LLMClient。net/http は ctx が切れるとこちらを返す（ctx.Err() では
// ない）ので、既定のアダプタを通ったときの形になる。
type causeLLM struct {
	mu    sync.Mutex
	calls int
}

func (c *causeLLM) Complete(ctx context.Context, _ port.VisionRequest) (port.VisionResponse, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()

	<-ctx.Done()
	return port.VisionResponse{}, fmt.Errorf("call messages api: %w", context.Cause(ctx))
}

func (c *causeLLM) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// 応えない LLM は 1 回の上限で切れ、再送しない（ADR 0104）。上限はユースケースが
// 呼び出しごとに ctx で掛けるので、差し込んだ LLMClient でも効く。アダプタの
// 上限に頼ると、自前の実装では効かない。
//
// **切れたことと上限の値は文言に残す。** 待ちきれなかったのか、向こうが断った
// のかを、畳んだ詳細から見分けられるようにする。どれだけ延ばせばよいかの
// 手掛かりにもなる。**切れ方は 2 通りとも見る。** ctx.Err() を返す実装と、
// 原因を返す実装（net/http、つまり既定のアダプタ）のどちらでも
// context.DeadlineExceeded を辿れる必要がある。
func TestLLMCallTimeout_EndsUnresponsiveCallWithoutRetry(t *testing.T) {
	t.Parallel()

	type stuck interface {
		port.LLMClient
		callCount() int
	}
	for name, c := range timedCallers {
		for kind, newLLM := range map[string]func() stuck{
			"ctx.Err() を返す": func() stuck { return newGatedLLM(c.valid) }, // 放さないので、上限まで応えない
			"原因を返す":         func() stuck { return &causeLLM{} },
		} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				t.Parallel()

				llm := newLLM()
				err := c.run(t, llm, 3, 30*time.Millisecond)

				if !errors.Is(err, usecase.ErrLLMUnavailable) {
					t.Fatalf("err = %v, want ErrLLMUnavailable", err)
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("err = %v, want to reach context.DeadlineExceeded", err)
				}
				for _, want := range []string{"no response within 30ms", "the limit for one call", "context deadline exceeded"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("err = %q, want to contain %q", err, want)
					}
				}
				if got := llm.callCount(); got != 1 {
					t.Errorf("LLM 呼び出し回数 = %d, want 1（呼び出しの失敗は再送しない）", got)
				}
			})
		}
	}
}

// 上限に当たっていない失敗には、上限の文を足さない。足すと、鍵の誤りのような
// 失敗まで「待ちきれなかった」と読める。
func TestLLMCallTimeout_DoesNotBlameTheLimitForOtherFailures(t *testing.T) {
	t.Parallel()

	for name, c := range timedCallers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := c.run(t, &fakeLLM{err: errors.New("api key is invalid")}, 3, time.Minute)
			if !errors.Is(err, usecase.ErrLLMUnavailable) {
				t.Fatalf("err = %v, want ErrLLMUnavailable", err)
			}
			if strings.Contains(err.Error(), "the limit for one call") {
				t.Errorf("err = %q, blames the limit for a failure that did not wait", err)
			}
		})
	}
}

// 上限は呼び出し 1 回ごとに掛ける（ADR 0104）。実行全体に掛けると、1 回ずつは
// 上限に収まる遅い応答でも、再送を重ねたところで切れる。ジョブの上限も呼ぶ回数
// ぶんあるので、最後の呼び出しまで待つ。
func TestLLMCallTimeout_AppliesToEachCall(t *testing.T) {
	t.Parallel()

	for name, c := range timedCallers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// 1 回 200ms は上限（500ms）の内側だが、3 回で 600ms になり上限を超える。
			llm := &slowLLM{
				delay:     200 * time.Millisecond,
				responses: []string{c.invalid, c.invalid, c.valid},
			}
			if err := c.run(t, llm, 3, 500*time.Millisecond); err != nil {
				t.Fatalf("err = %v, want nil（1 回ずつは上限に収まっている）", err)
			}
			if got := llm.callCount(); got != 3 {
				t.Errorf("LLM 呼び出し回数 = %d, want 3", got)
			}
			// どの呼び出しも、1 回の上限ぶんの期限が付いた ctx で呼ばれる。ジョブの
			// ctx のまま渡すと、応えない相手をジョブの上限（3 倍）まで待つ。
			for i, w := range llm.waitList() {
				if w <= 0 || w > 500*time.Millisecond {
					t.Errorf("%d 回目の呼び出しが待てる長さ = %s, want 500ms 以内", i+1, w)
				}
			}
		})
	}
}

// ジョブの上限は「1 回の上限 × 呼ぶ回数」から導く（ADR 0104）。固定にすると、
// 1 回の上限を延ばしたときや、遅い応答が続いたときに、最後の呼び出しが 1 回の
// 上限より先にジョブの上限で切れる（5 分の上限に 10 分のジョブでは、解釈の
// 3 回目がそうなりえた）。
func TestJobs_TimeoutCoversEveryAttempt(t *testing.T) {
	t.Parallel()

	newBoards := func() *fakeBoards { return &fakeBoards{board: newBoard(interpretScene)} }
	interpretation := func(opts ...usecase.InterpretationServiceOption) time.Duration {
		return usecase.InterpretationJobTimeout(usecase.NewInterpretationJobs(
			usecase.NewInterpretationService(newBoards(), &fakeMappings{}, &fakeLLM{}, newLimiter(), opts...)))
	}
	diagram := func(opts ...usecase.DiagramServiceOption) time.Duration {
		return usecase.DiagramJobTimeout(usecase.NewDiagramJobs(
			usecase.NewDiagramService(newBoards(), &fakeLLM{}, newLimiter(), opts...)))
	}

	for name, tc := range map[string]struct {
		got, want time.Duration
	}{
		// 既定は 1 回 5 分。解釈は 3 回、図は 2 回まで呼ぶ。
		"解釈の既定": {got: interpretation(), want: 15 * time.Minute},
		"図の既定":  {got: diagram(), want: 10 * time.Minute},
		"解釈": {
			got:  interpretation(usecase.WithMaxAttempts(4), usecase.WithCallTimeout(2*time.Minute)),
			want: 8 * time.Minute,
		},
		"図": {
			got:  diagram(usecase.WithDiagramMaxAttempts(3), usecase.WithDiagramCallTimeout(3*time.Minute)),
			want: 9 * time.Minute,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.got != tc.want {
				t.Errorf("ジョブの上限 = %s, want %s", tc.got, tc.want)
			}
		})
	}
}
