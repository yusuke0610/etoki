package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// llmCallers は LLM を叩く 2 つの経路。**写し替えはどちらも同じ**であることを、
// 同じ表で回して確かめる。片方だけ写すと、同じ打ち切りが経路によって別の
// code になる（ADR 0087）。
var llmCallers = map[string]func(t *testing.T, llm *fakeLLM) error{
	"解釈": func(t *testing.T, llm *fakeLLM) error {
		svc, _ := newInterpretService(t, newBoard(interpretScene), llm)
		_, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
		return err
	},
	"図の生成": func(t *testing.T, llm *fakeLLM) error {
		svc, _ := newDiagramService(t, llm)
		_, err := generate(t, svc, t.Context(), "board-1", req("段取り"))
		return err
	},
}

// LLM の呼び出しの失敗に関わる sentinel。どれが真になるかを全部見る。
var llmFailureSentinels = map[string]error{
	"usecase.ErrLLMUnavailable":       usecase.ErrLLMUnavailable,
	"usecase.ErrLLMTruncated":         usecase.ErrLLMTruncated,
	"usecase.ErrLLMRefused":           usecase.ErrLLMRefused,
	"usecase.ErrLLMOverloaded":        usecase.ErrLLMOverloaded,
	"usecase.ErrInterpretationFailed": usecase.ErrInterpretationFailed,
	"usecase.ErrDiagramFailed":        usecase.ErrDiagramFailed,
	"port.ErrLLMTruncated":            port.ErrLLMTruncated,
	"port.ErrLLMRefused":              port.ErrLLMRefused,
	"port.ErrLLMOverloaded":           port.ErrLLMOverloaded,
}

// port の sentinel はユースケース層の sentinel に写し替える。**入れ子にしない。**
// port の sentinel まで errors.Is で見えると、httpapi の表は並び順で写し先を
// 決めることになる。ErrLLMUnavailable と同時に真になると、llm_unavailable と
// 新しい code のどちらに写るかが表の並び順しだいになる。
//
// どれも再送しない。打ち切りも拒否も混雑も、修正指示で直る問題ではない。
func TestLLMCallError_MapsCauseWithoutNesting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
		want  string
	}{
		{
			name:  "打ち切り",
			cause: fmt.Errorf("%w: stopped at 1504 output tokens, before our limit of 16000", port.ErrLLMTruncated),
			want:  "usecase.ErrLLMTruncated",
		},
		{
			name:  "拒否",
			cause: fmt.Errorf("%w: model refused the request: cyber", port.ErrLLMRefused),
			want:  "usecase.ErrLLMRefused",
		},
		{
			name:  "上流の混雑",
			cause: fmt.Errorf("%w: messages api: 529 overloaded_error: Overloaded", port.ErrLLMOverloaded),
			want:  "usecase.ErrLLMOverloaded",
		},
		{
			// 外部の LLMClient 実装は sentinel を返さなくてよい。返さなければ
			// いままでどおり llm_unavailable に落ちる。
			name:  "名乗らない失敗",
			cause: errors.New("api key is invalid"),
			want:  "usecase.ErrLLMUnavailable",
		},
	}

	for caller, call := range llmCallers {
		for _, tt := range tests {
			t.Run(caller+"/"+tt.name, func(t *testing.T) {
				t.Parallel()

				llm := &fakeLLM{err: tt.cause}
				err := call(t, llm)
				if err == nil {
					t.Fatal("err = nil, want error")
				}

				for name, sentinel := range llmFailureSentinels {
					if got := errors.Is(err, sentinel); got != (name == tt.want) {
						t.Errorf("errors.Is(%v, %s) = %v, want %v", err, name, got, name == tt.want)
					}
				}
				// 原因の手掛かり（誰が切ったか等）は文言に残す。
				if !strings.Contains(err.Error(), tt.cause.Error()) {
					t.Errorf("err = %q, want to contain %q", err, tt.cause)
				}
				if len(llm.requests) != 1 {
					t.Errorf("LLM 呼び出し回数 = %d, want 1（再送しない）", len(llm.requests))
				}
			})
		}
	}
}

// 名乗らない失敗は元のエラーを包んだまま返す。httpapi は context.Canceled を
// 辿って「待っていた相手がいなくなった」を障害と分けている（#253）。%v に
// 揃えると、リロードのたびに 502 と Error が記録に残る。
func TestLLMCallError_KeepsCancellationReachable(t *testing.T) {
	t.Parallel()

	for caller, call := range llmCallers {
		t.Run(caller, func(t *testing.T) {
			t.Parallel()

			err := call(t, &fakeLLM{err: fmt.Errorf("call messages api: %w", context.Canceled)})
			if !errors.Is(err, usecase.ErrLLMUnavailable) {
				t.Errorf("err = %v, want ErrLLMUnavailable", err)
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("err = %v, want to reach context.Canceled", err)
			}
		})
	}
}
