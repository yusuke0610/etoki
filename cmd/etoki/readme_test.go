package main

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki"
	"github.com/yusuke0610/etoki/internal/adapter/llm"
)

// readmeEnvRow は README の「設定」の表の 1 行。
type readmeEnvRow struct {
	name string
	// def は既定値の欄。バッククォートを外した値で、無ければ空文字。
	def string
}

// readmeNoDefault は README で「既定値が無い」を表す書き方。
const readmeNoDefault = "（なし）"

// readmeEnvRows は README の表から環境変数の行だけを取り出す。
//
// 見出しが `| 変数 |` で始まる表だけを読む。README には `ETOKI_PUBLIC_URL` を
// 行に持つ別の表（`make dev` と `make start` の比較）もあり、そちらは既定値の
// 表ではない。形を変えたらここも直す。**読めた行が 0 件なら落とす**ので、形が
// 変わって黙って何も照合しなくなることは無い。
func readmeEnvRows(t *testing.T) []readmeEnvRow {
	t.Helper()

	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}

	header := regexp.MustCompile(`^\|\s*変数\s*\|`)
	row := regexp.MustCompile("^\\|\\s*`(ETOKI_[A-Z0-9_]+)`\\s*\\|\\s*([^|]*?)\\s*\\|")
	var rows []readmeEnvRow
	inTable := false
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case header.MatchString(line):
			inTable = true
			continue
		case !strings.HasPrefix(line, "|"):
			inTable = false
		}
		if !inTable {
			continue
		}
		m := row.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		def := m[2]
		if def == readmeNoDefault {
			def = ""
		} else {
			def = strings.Trim(def, "`")
		}
		rows = append(rows, readmeEnvRow{name: m[1], def: def})
	}
	if len(rows) == 0 {
		t.Fatal("README から環境変数の表を 1 行も読めなかった")
	}
	return rows
}

// README の「設定」の表は人が読む一覧で、既定値の正本は Go の定数にある。
// **写しなので、ずれたら落とす**（#155）。落ちたら README を定数に合わせる。
//
// 既定値の無い変数は「（なし）」と書く。既定値を足したのに README を
// 「（なし）」のままにした場合も、ここに無い変数に既定値を書いた場合も落ちる。
func TestREADME_EnvDefaultsMatchConstants(t *testing.T) {
	want := map[string]string{
		"ETOKI_ADDR":                etoki.DefaultAddr,
		"ETOKI_DB_PATH":             defaultDBPath,
		"ETOKI_LLM_BASE_URL":        llm.DefaultBaseURL,
		"ETOKI_LLM_MODEL":           llm.DefaultModel,
		"ETOKI_LLM_MAX_CONCURRENT":  strconv.Itoa(etoki.DefaultLLMMaxConcurrent),
		"ETOKI_LLM_RATE_WINDOW":     etoki.DefaultLLMRateWindow.String(),
		"ETOKI_GITHUB_KIND_FIELD":   etoki.DefaultKindFieldName,
		"ETOKI_GITHUB_PARENT_FIELD": etoki.DefaultParentFieldName,
	}

	for _, r := range readmeEnvRows(t) {
		expected := want[r.name]

		// 期間は書き方が揃わない（README は 1h、Duration.String は 1h0m0s）
		// ので、値として比べる。
		if r.name == "ETOKI_LLM_RATE_WINDOW" {
			got, err := time.ParseDuration(r.def)
			if err != nil || got != etoki.DefaultLLMRateWindow {
				t.Errorf("%s: README の既定値 %q が %s と一致しない", r.name, r.def, expected)
			}
			continue
		}

		if r.def != expected {
			t.Errorf("%s: README の既定値 %q, 定数 %q", r.name, r.def, expected)
		}
	}
}

// README の表と `etoki` の usage は、同じ環境変数を並べる。どちらかに足し忘れると
// 落ちる。usage は既定値を定数から組み立てているので、並びの照合だけでよい。
func TestREADME_EnvNamesMatchUsage(t *testing.T) {
	var readme []string
	for _, r := range readmeEnvRows(t) {
		readme = append(readme, r.name)
	}

	env := regexp.MustCompile(`(?m)^\s+(ETOKI_[A-Z0-9_]+)\s`)
	var fromUsage []string
	for _, m := range env.FindAllStringSubmatch(usage, -1) {
		fromUsage = append(fromUsage, m[1])
	}

	slices.Sort(readme)
	slices.Sort(fromUsage)
	if !slices.Equal(readme, fromUsage) {
		t.Errorf("README と usage の環境変数が食い違う\nREADME: %v\nusage:  %v", readme, fromUsage)
	}
}
