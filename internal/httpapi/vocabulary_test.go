package httpapi

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// 列挙の語彙が、domain / port と契約（api/openapi.yaml の enum）で一致していること（#275）。
//
// **2 つ持つこと自体は避けられない。** port/ は internal/ に依存せず（ADR 0001）、
// domain は境界の型を参照しない（依存の方向）。ハンドラは string のキャストで
// 写し合うだけなので、片方にだけ値を足してもコンパイルは通る。契約にだけ足すと
// domain の Valid() が 400 で弾き、domain にだけ足すと契約に無い値が応答に出る。
//
// **どちらの側もソースから数え直す。** 手で並べた一覧と比べると、両方に同じ
// 書き忘れをしたときに緑のまま通る（test-effectiveness.md「写しと正本を照合する
// テストは、両方の側から引く」）。
func TestVocabulary_MatchesContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		// name は型の名前。domain / port と契約（apitypes）で同じ名前を使っている。
		name string
		dir  string // domain / port の側のパッケージ（リポジトリのルートから）
		// onlyInDomain は契約に出ない domain の値と、出ない理由。
		onlyInDomain map[string]string
	}{
		{
			name: "DiagramKind", dir: "internal/domain",
			// 契約では「指定なし」をキーの省略で表す（ADR 0047、AnnotationStatus.kind）。
			onlyInDomain: map[string]string{"": "指定なしは省略で表す"},
		},
		{
			// 粒度は「指定なし」を空文字のまま契約に載せている。
			name: "Granularity", dir: "internal/domain",
		},
		{
			name: "ItemKind", dir: "internal/domain",
		},
		{
			name: "SyncState", dir: "internal/domain",
		},
		{
			name: "BoardRole", dir: "port",
		},
		{
			// 作成先の Project のフィールドの確認（ADR 0088）。境界に出るのは
			// usecase の型で、domain / port には無い。
			name: "ProjectFieldRequirement", dir: "internal/usecase",
		},
		{
			name: "ProjectFieldsStatus", dir: "internal/usecase",
		},
		{
			name: "RunOutcome", dir: "port",
			// 記録していなかった頃の run。契約では lastRunOutcome の省略で表す（ADR 0043）。
			onlyInDomain: map[string]string{"": "不明は省略で表す"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			domain := stringConsts(t, filepath.Join("..", "..", tc.dir), tc.name)
			api := stringConsts(t, "apitypes", tc.name)

			// 例外が実在すること。消えた値の例外を残すと、表が嘘をつく。
			for v := range tc.onlyInDomain {
				if !slices.Contains(domain, v) {
					t.Errorf("例外 %q が %s.%s に無い。表から外す", v, tc.dir, tc.name)
				}
			}

			var want []string
			for _, v := range domain {
				if _, ok := tc.onlyInDomain[v]; !ok {
					want = append(want, v)
				}
			}

			if !slices.Equal(want, api) {
				t.Errorf("%s.%s = %q, 契約 = %q（例外を除く）。片方にだけ値がある",
					tc.dir, tc.name, domain, api)
			}
		})
	}
}

// stringConsts は dir のパッケージで、型 typeName を明示した文字列の定数の値を
// 並べ替えて返す。テストのファイルは数えない。
//
// 1 つも見つからなければ落とす。型名を打ち間違えたときに、空どうしの比較で
// 通るのを避ける。
func stringConsts(t *testing.T, dir, typeName string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	var values []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") ||
			strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		file, err := parser.ParseFile(
			token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}

		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				typ, ok := value.Type.(*ast.Ident)
				if !ok || typ.Name != typeName {
					continue
				}
				for _, v := range value.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: %s の定数が文字列のリテラルでない", e.Name(), typeName)
					}
					s, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Fatalf("unquote %s: %v", lit.Value, err)
					}
					values = append(values, s)
				}
			}
		}
	}

	if len(values) == 0 {
		t.Fatalf("%s に %s の定数が無い", dir, typeName)
	}
	slices.Sort(values)
	return slices.Compact(values)
}
