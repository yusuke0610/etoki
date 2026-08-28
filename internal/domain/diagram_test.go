package domain_test

import (
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// 語彙は 1 つ（#52 と #58 が同じ 5 種を指す）。**ここが緩むと、片方だけが
// 知っている種類ができる。**
func TestDiagramKind_Valid(t *testing.T) {
	t.Parallel()

	for _, k := range domain.DiagramKinds() {
		if !k.Valid() {
			t.Errorf("DiagramKinds() に入っている %q が Valid ではない", k)
		}
	}

	// ゼロ値は「指定なし」。無効値ではないので、注釈に種類を付けないことが
	// そのまま表せる（Granularity と同じ）。指定を要求するかは使う側が決める。
	if !domain.DiagramKindUnspecified.Valid() {
		t.Error("指定なしを無効値として扱っている")
	}

	// mermaid にはあるが etoki の語彙には無いもの。**mermaid で書けることと、
	// etoki が選ばせることは別。**
	for _, k := range []domain.DiagramKind{"gantt", "class", "state", "TODO"} {
		if domain.DiagramKind(k).Valid() {
			t.Errorf("%q を知っている種類として扱っている", k)
		}
	}
}

// 並びは表示にそのまま使う。数が合わないと、選択肢から漏れた種類が
// 「送れば通るが選べない」状態になる。
func TestDiagramKinds_ExcludesUnspecified(t *testing.T) {
	t.Parallel()

	kinds := domain.DiagramKinds()
	if len(kinds) != 5 {
		t.Fatalf("len(DiagramKinds()) = %d, want 5", len(kinds))
	}

	seen := map[domain.DiagramKind]bool{}
	for _, k := range kinds {
		if k == domain.DiagramKindUnspecified {
			t.Error("選択肢に「指定なし」が混ざっている")
		}
		if seen[k] {
			t.Errorf("%q が重複している", k)
		}
		seen[k] = true
	}
}
