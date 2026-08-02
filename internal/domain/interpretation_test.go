package domain_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
)

// validInterpretation は制約をすべて満たす解釈結果を返す。
//
// i2 に親がないのは意図的。epic に属さない単独の issue は誤りではない。
func validInterpretation() domain.Interpretation {
	return domain.Interpretation{
		Summary: "決済まわりの課題出し。カード決済と返金の 2 系統に分かれている。",
		Items: []domain.InterpretedItem{
			{LocalID: "e1", Kind: domain.KindEpic, Title: "決済フローの見直し", Body: "全体の方針"},
			{LocalID: "i1", Kind: domain.KindIssue, Title: "カード決済のエラー処理", ParentLocalID: ptr("e1")},
			{LocalID: "i2", Kind: domain.KindIssue, Title: "返金導線の整理"},
		},
	}
}

// errorFields は検証エラーの Field を出た順に返す。
func errorFields(t *testing.T, err error) []string {
	t.Helper()

	var errs domain.ValidationErrors
	if !errors.As(err, &errs) {
		t.Fatalf("ValidationErrors ではない: %#v", err)
	}

	fields := make([]string, len(errs))
	for i, e := range errs {
		fields[i] = e.Field
	}
	return fields
}

func TestParseInterpretation(t *testing.T) {
	t.Parallel()

	t.Run("2 階層の出力を読める", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{
			"summary": "決済まわりの課題出し",
			"items": [
				{"localId": "e1", "kind": "epic", "title": "決済フローの見直し", "body": "全体の方針", "parentLocalId": null},
				{"localId": "i1", "kind": "issue", "title": "カード決済のエラー処理", "body": "", "parentLocalId": "e1"}
			]
		}`)

		in, err := domain.ParseInterpretation(raw)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}

		if in.Summary != "決済まわりの課題出し" {
			t.Errorf("Summary = %q", in.Summary)
		}
		if len(in.Items) != 2 {
			t.Fatalf("len(Items) = %d, want 2", len(in.Items))
		}
		if in.Items[0].ParentLocalID != nil {
			t.Errorf("Items[0].ParentLocalID = %q, want nil", *in.Items[0].ParentLocalID)
		}
		if in.Items[1].ParentLocalID == nil || *in.Items[1].ParentLocalID != "e1" {
			t.Errorf("Items[1].ParentLocalID = %v, want \"e1\"", in.Items[1].ParentLocalID)
		}
	})

	t.Run("parentLocalId を省いた出力は親なしとして読む", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{"summary": "s", "items": [{"localId": "e1", "kind": "epic", "title": "t", "body": ""}]}`)

		in, err := domain.ParseInterpretation(raw)
		if err != nil {
			t.Fatalf("ParseInterpretation() = %v", err)
		}
		if in.Items[0].ParentLocalID != nil {
			t.Errorf("ParentLocalID = %q, want nil", *in.Items[0].ParentLocalID)
		}
	})

	// 綴り間違いを素通しにすると、親子関係が黙って失われる。
	t.Run("未知のフィールドを誤りとして扱う", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{"summary": "s", "items": [{"localId": "i1", "kind": "issue", "title": "t", "parentId": "e1"}]}`)

		if _, err := domain.ParseInterpretation(raw); err == nil {
			t.Fatal("ParseInterpretation() = nil, want error")
		}
	})

	t.Run("JSON の後ろに続く出力を誤りとして扱う", func(t *testing.T) {
		t.Parallel()

		raw := []byte(`{"summary": "s", "items": []} 以上が解釈結果です。`)

		if _, err := domain.ParseInterpretation(raw); err == nil {
			t.Fatal("ParseInterpretation() = nil, want error")
		}
	})

	t.Run("壊れた JSON を誤りとして扱う", func(t *testing.T) {
		t.Parallel()

		if _, err := domain.ParseInterpretation([]byte(`{"summary":`)); err == nil {
			t.Fatal("ParseInterpretation() = nil, want error")
		}
	})
}

func TestInterpretation_Validate_Valid(t *testing.T) {
	t.Parallel()

	for _, g := range []domain.Granularity{domain.GranularityAuto, domain.GranularityEpic} {
		if err := validInterpretation().Validate(g); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", g, err)
		}
	}
}

func TestInterpretation_Validate_ItemRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// mutate は妥当な解釈結果を壊す。
		mutate func(*domain.Interpretation)
		// wantFields は報告されてほしい Field。順不同で照合する。
		wantFields []string
	}{
		{
			name:       "summary が空",
			mutate:     func(in *domain.Interpretation) { in.Summary = "  \n " },
			wantFields: []string{"summary"},
		},
		{
			name:       "項目が 1 件もない",
			mutate:     func(in *domain.Interpretation) { in.Items = nil },
			wantFields: []string{"items"},
		},
		{
			name:       "kind が epic でも issue でもない",
			mutate:     func(in *domain.Interpretation) { in.Items[0].Kind = "project" },
			wantFields: []string{"items[0].kind"},
		},
		{
			name:       "kind が空",
			mutate:     func(in *domain.Interpretation) { in.Items[0].Kind = "" },
			wantFields: []string{"items[0].kind"},
		},
		{
			name:       "title が空白だけ",
			mutate:     func(in *domain.Interpretation) { in.Items[1].Title = "   " },
			wantFields: []string{"items[1].title"},
		},
		{
			name:       "localId が空",
			mutate:     func(in *domain.Interpretation) { in.Items[2].LocalID = "" },
			wantFields: []string{"items[2].localId"},
		},
		{
			name:       "localId が重複している",
			mutate:     func(in *domain.Interpretation) { in.Items[2].LocalID = "i1" },
			wantFields: []string{"items[2].localId"},
		},
		{
			name:       "parentLocalId の指す localId がない",
			mutate:     func(in *domain.Interpretation) { in.Items[1].ParentLocalID = ptr("e9") },
			wantFields: []string{"items[1].parentLocalId"},
		},
		{
			name:       "自分自身を親にしている",
			mutate:     func(in *domain.Interpretation) { in.Items[1].ParentLocalID = ptr("i1") },
			wantFields: []string{"items[1].parentLocalId"},
		},
		{
			name:       "epic が親を持っている",
			mutate:     func(in *domain.Interpretation) { in.Items[0].ParentLocalID = ptr("i1") },
			wantFields: []string{"items[0].parentLocalId"},
		},
		{
			// 階層は epic ← issue の 1 本だけ（ADR 0006）。
			name:       "issue を親にしている",
			mutate:     func(in *domain.Interpretation) { in.Items[2].ParentLocalID = ptr("i1") },
			wantFields: []string{"items[2].parentLocalId"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := validInterpretation()
			tt.mutate(&in)

			err := in.Validate(domain.GranularityAuto)
			if err == nil {
				t.Fatal("Validate() = nil, want error")
			}

			got := errorFields(t, err)
			for _, want := range tt.wantFields {
				if !slices.Contains(got, want) {
					t.Errorf("Field %q が報告されていない: %v", want, got)
				}
			}
			if len(got) != len(tt.wantFields) {
				t.Errorf("報告された Field = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

// 粒度はプロンプトでも指示するが、LLM が従う保証はないので構造で守る。
func TestInterpretation_Validate_Granularity(t *testing.T) {
	t.Parallel()

	t.Run("issue 指定なら epic を作れない", func(t *testing.T) {
		t.Parallel()

		err := validInterpretation().Validate(domain.GranularityIssue)
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if got := errorFields(t, err); !slices.Contains(got, "items[0].kind") {
			t.Errorf("報告された Field = %v", got)
		}
	})

	t.Run("issue 指定で issue だけなら通る", func(t *testing.T) {
		t.Parallel()

		in := domain.Interpretation{
			Summary: "返金導線の整理",
			Items: []domain.InterpretedItem{
				{LocalID: "i1", Kind: domain.KindIssue, Title: "返金導線の整理"},
			},
		}
		if err := in.Validate(domain.GranularityIssue); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("epic 指定なのに epic がない", func(t *testing.T) {
		t.Parallel()

		in := domain.Interpretation{
			Summary: "返金導線の整理",
			Items: []domain.InterpretedItem{
				{LocalID: "i1", Kind: domain.KindIssue, Title: "返金導線の整理"},
			},
		}

		err := in.Validate(domain.GranularityEpic)
		if err == nil {
			t.Fatal("Validate() = nil, want error")
		}
		if got := errorFields(t, err); !slices.Contains(got, "items") {
			t.Errorf("報告された Field = %v", got)
		}
	})

	t.Run("指定なしなら粒度の制約を課さない", func(t *testing.T) {
		t.Parallel()

		if err := validInterpretation().Validate(domain.GranularityAuto); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})
}

// 最初の 1 件で打ち切ると、LLM は 1 往復で 1 箇所しか直せない（ADR 0005）。
func TestInterpretation_Validate_CollectsAllErrors(t *testing.T) {
	t.Parallel()

	in := domain.Interpretation{
		Summary: "",
		Items: []domain.InterpretedItem{
			{LocalID: "", Kind: "project", Title: ""},
			{LocalID: "i1", Kind: domain.KindIssue, Title: "返金導線の整理", ParentLocalID: ptr("e9")},
		},
	}

	err := in.Validate(domain.GranularityEpic)
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}

	want := []string{
		"summary",
		"items[0].kind",
		"items[0].title",
		"items[0].localId",
		"items[1].parentLocalId",
		"items", // epic 指定なのに epic がない
	}

	got := errorFields(t, err)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("Field %q が報告されていない: %v", w, got)
		}
	}
	if len(got) != len(want) {
		t.Errorf("報告件数 = %d, want %d (%v)", len(got), len(want), got)
	}
}

func TestValidationErrors_Error(t *testing.T) {
	t.Parallel()

	errs := domain.ValidationErrors{
		{Field: "summary", Message: "説明を入れてください"},
		{Field: "items[0].title", Message: "title を空にはできません"},
	}

	want := "summary: 説明を入れてください\nitems[0].title: title を空にはできません"
	if got := errs.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestItemKind_Valid(t *testing.T) {
	t.Parallel()

	tests := map[domain.ItemKind]bool{
		domain.KindEpic:  true,
		domain.KindIssue: true,
		"":               false,
		"project":        false,
		"Epic":           false,
	}

	for k, want := range tests {
		if got := k.Valid(); got != want {
			t.Errorf("ItemKind(%q).Valid() = %t, want %t", k, got, want)
		}
	}
}
