//go:build eval

package usecase_test

import (
	"cmp"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/llm"
)

// TestEval は評価セットを本物の LLM で絵解き、採点用の報告を書く（ADR 0098）。
//
// **`make eval` から回す。** 課金されるので、`make test` にも CI にも入れない
// （ビルドタグ eval）。向け先はサーバーと同じ ETOKI_LLM_* で、本番と同じ
// アダプタ（internal/adapter/llm）を通す。ユースケースのテストがアダプタを
// 引くのはここだけで、測りたいのが「本番の向け先での絵解き」だから。
//
// **ケースが失敗しても落とさない。** 失敗も結果の 1 つで、報告に載せる。落とす
// のは、報告を書けなかったときと、回す前に分かる誤り（ケースが読めない）だけ。
//
// 環境変数:
//   - ETOKI_EVAL_CASE: 1 ケースだけ回すときの、そのディレクトリ名
//   - ETOKI_EVAL_OUT: 報告を書く場所（既定はリポジトリの tmp/eval）
//   - ETOKI_EVAL_COMMIT: 報告に載せるコミット（make eval が渡す）
func TestEval(t *testing.T) {
	cfg := llm.ConfigFromEnv()
	client, err := llm.New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	cases, err := loadEvalCases(evalRoot)
	if err != nil {
		t.Fatal(err)
	}
	if only := os.Getenv("ETOKI_EVAL_CASE"); only != "" {
		cases = slices.DeleteFunc(cases, func(c evalCase) bool { return c.name != only })
		if len(cases) == 0 {
			t.Fatalf("ケース %q が %s に無い", only, evalRoot)
		}
	}

	report := evalReport{
		startedAt: time.Now(),
		model:     cmp.Or(cfg.Model, llm.DefaultModel),
		endpoint:  endpointOf(cmp.Or(cfg.BaseURL, llm.DefaultBaseURL)),
		commit:    os.Getenv("ETOKI_EVAL_COMMIT"),
	}
	// 何に向けて回すかを最初に出す。.env が別の向け先を指していたら、そこで
	// 止められる（1 ケース目の呼び出しは、出した直後に始まる）。
	t.Logf("向け先 %s のモデル %s で、%d ケースを絵解きます", report.endpoint, report.model, len(cases))
	// 1 ケースずつ順に回す。並べると、時間の欄が上流の混み具合を測ることになる。
	for _, c := range cases {
		t.Logf("%s を絵解いています", c.name)
		run := runEvalCase(t.Context(), c, client, slog.Default())
		t.Logf("%s: %s（%d 回、%s）", c.name, run.outcome(), len(run.calls), seconds(run.elapsed))
		report.runs = append(report.runs, run)
	}

	// go test はパッケージのディレクトリで走るので、既定はそこからリポジトリの
	// tmp/（gitignore 済み）を指す。結果はリポジトリに残さない（ADR 0098）。
	out := cmp.Or(os.Getenv("ETOKI_EVAL_OUT"), filepath.Join("..", "..", "tmp", "eval"))
	name := report.startedAt.UTC().Format("20060102-150405") + "-" +
		unsafePathChars.ReplaceAllString(report.model, "-")
	path, err := writeEvalReport(filepath.Join(out, name), report)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("報告: %s", path)
}

// unsafePathChars はディレクトリ名に使わない文字。モデル ID には / や : が入りうる。
var unsafePathChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
