package main

import (
	"testing"
	"time"

	"github.com/yusuke0610/etoki"
)

// 実行の上限は**未設定と 0 を区別する**（ADR 0043）。未設定は「既定のまま」、
// 0 は設定の誤り。0 を無制限と読ませると、未設定（既定 1）と 0（無制限）で
// 意味が逆向きになる。
//
// **読めない値を既定に倒さない。** 倒すと、設定したつもりの上限が黙って外れる。
func TestLLMLimits(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want etoki.LLMLimits
		err  bool
	}{
		"未設定はゼロ値（既定のまま）": {env: nil, want: etoki.LLMLimits{}},
		"設定した値をそのまま渡す": {
			env: map[string]string{
				"ETOKI_LLM_MAX_CONCURRENT": "2",
				"ETOKI_LLM_RATE_LIMIT":     "60",
				"ETOKI_LLM_RATE_WINDOW":    "30m",
			},
			want: etoki.LLMLimits{MaxConcurrent: 2, RateLimit: 60, RateWindow: 30 * time.Minute},
		},
		"同時実行の 0 は誤り": {env: map[string]string{"ETOKI_LLM_MAX_CONCURRENT": "0"}, err: true},
		"回数の 0 は誤り":   {env: map[string]string{"ETOKI_LLM_RATE_LIMIT": "0"}, err: true},
		"数値でない同時実行":   {env: map[string]string{"ETOKI_LLM_MAX_CONCURRENT": "たくさん"}, err: true},
		"負の回数":        {env: map[string]string{"ETOKI_LLM_RATE_LIMIT": "-1"}, err: true},
		"読めない窓":       {env: map[string]string{"ETOKI_LLM_RATE_WINDOW": "1 hour"}, err: true},
		"0 の窓":        {env: map[string]string{"ETOKI_LLM_RATE_WINDOW": "0s"}, err: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// t.Setenv は並行実行を禁じるので t.Parallel は呼ばない。
			// **3 つとも明示的に置く。** 実行環境に残っている値を拾うと、
			// 「未設定」のケースが手元と CI で違う結果になる。
			for _, k := range []string{
				"ETOKI_LLM_MAX_CONCURRENT", "ETOKI_LLM_RATE_LIMIT", "ETOKI_LLM_RATE_WINDOW",
			} {
				t.Setenv(k, tc.env[k])
			}

			got, err := llmLimits()
			if tc.err {
				if err == nil {
					t.Fatalf("llmLimits() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("llmLimits() = %v", err)
			}
			if got != tc.want {
				t.Errorf("llmLimits() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
