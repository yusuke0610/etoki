package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// annotationRulePath は Go と TypeScript が共有する判定対象の置き場所。
//
// **web/src/excalidraw/annotation.test.ts が同じファイルを読む。** 動かすなら
// 両方を直す。
const annotationRulePath = "../../testdata/annotation-rule.json"

type annotationRule struct {
	Cases []struct {
		Name         string          `json:"name"`
		IsAnnotation bool            `json:"isAnnotation"`
		Element      json.RawMessage `json:"element"`
	} `json:"cases"`
}

// TestIsAnnotation_MatchesSharedRule は注釈の判定規則を共有のテストデータで
// 固定する。
//
// **回帰止め。切れると何が起きるか。** 判定規則は Go（ここ）と TypeScript
// （web/src/excalidraw/annotation.ts）の 2 箇所にあり、port が internal に
// 依存できない事情と同じで構造上まとめられない。片方だけ変えると、画面が
// 注釈として出すものとサーバーが解釈の対象にするものがずれる。**どちらも
// 同じ規則のつもりなので、ずれに気づくのは開発者ではなく利用者になる。**
//
// 判定そのものは非公開なので、Annotations が拾うかどうかで見る。外から
// 観測できる形が同じであれば、規則が一致していると言える。
func TestIsAnnotation_MatchesSharedRule(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Clean(annotationRulePath))
	if err != nil {
		t.Fatalf("共有のテストデータを読めない: %v", err)
	}

	var rule annotationRule
	if err := json.Unmarshal(raw, &rule); err != nil {
		t.Fatalf("unmarshal %s: %v", annotationRulePath, err)
	}
	if len(rule.Cases) == 0 {
		t.Fatal("cases が空。読む先を間違えている")
	}

	for _, c := range rule.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			sceneJSON := `{"elements":[` + string(c.Element) + `]}`

			s, err := domain.ParseScene([]byte(sceneJSON))
			if err != nil {
				t.Fatalf("ParseScene: %v", err)
			}

			got := len(s.Annotations()) == 1
			if got != c.IsAnnotation {
				t.Errorf("注釈として拾うか = %v, want %v (%s)",
					got, c.IsAnnotation, sceneJSON)
			}
		})
	}
}
