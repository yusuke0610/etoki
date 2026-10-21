package usecase_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 判定の入力にする Project のフィールド。作成と確認の両方に食わせる。
func fieldCases() map[string]struct {
	fields []port.ProjectField
	want   []usecase.ProjectFieldRequirement
} {
	kind := func(options ...string) port.ProjectField {
		f := port.ProjectField{ID: "F_kind", Name: "Kind", DataType: "SINGLE_SELECT"}
		for _, o := range options {
			f.Options = append(f.Options, port.ProjectFieldOption{ID: "O_" + o, Name: o})
		}
		return f
	}
	parent := port.ProjectField{ID: "F_parent", Name: "Parent", DataType: "TEXT"}

	return map[string]struct {
		fields []port.ProjectField
		want   []usecase.ProjectFieldRequirement
	}{
		"揃っている": {
			fields: []port.ProjectField{kind("epic", "issue"), parent},
		},
		// 選択肢は大文字小文字を区別しない。GitHub の画面で付けた名前のままで通す。
		"選択肢の大文字小文字は問わない": {
			fields: []port.ProjectField{kind("Epic", "ISSUE"), parent},
		},
		"何も無い": {
			want: []usecase.ProjectFieldRequirement{
				usecase.FieldKindEpic, usecase.FieldKindIssue, usecase.FieldParent,
			},
		},
		"種別のフィールドが無い": {
			fields: []port.ProjectField{parent},
			want:   []usecase.ProjectFieldRequirement{usecase.FieldKindEpic, usecase.FieldKindIssue},
		},
		"選択肢 issue が無い": {
			fields: []port.ProjectField{kind("epic"), parent},
			want:   []usecase.ProjectFieldRequirement{usecase.FieldKindIssue},
		},
		"選択肢 epic が無い": {
			fields: []port.ProjectField{kind("issue"), parent},
			want:   []usecase.ProjectFieldRequirement{usecase.FieldKindEpic},
		},
		"親のフィールドが無い": {
			fields: []port.ProjectField{kind("epic", "issue")},
			want:   []usecase.ProjectFieldRequirement{usecase.FieldParent},
		},
		// 名前で引き当てる。同じ形でも名前が違えば無いのと同じ。
		"名前が違う": {
			fields: []port.ProjectField{
				{ID: "F_x", Name: "Type", Options: []port.ProjectFieldOption{
					{ID: "O_e", Name: "epic"}, {ID: "O_i", Name: "issue"},
				}},
				{ID: "F_y", Name: "Parent issue"},
			},
			want: []usecase.ProjectFieldRequirement{
				usecase.FieldKindEpic, usecase.FieldKindIssue, usecase.FieldParent,
			},
		},
	}
}

// 足りないものを役割で、決まった並びで返す。
//
// 切れると: 画面が足りないものだけを並べられない。並びが揺れると、同じ Project を
// 選び直すたびに文の順が変わる。
func TestCheckProjectFields_ReportsMissingRequirements(t *testing.T) {
	t.Parallel()

	for name, tc := range fieldCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := usecase.NewGitHubCatalogService(
				&fakeGitHub{fields: tc.fields}, usecase.ProjectFieldNames{}, nil)

			got, err := svc.CheckProjectFields(t.Context(), "PVT_1")
			if err != nil {
				t.Fatalf("CheckProjectFields() = %v", err)
			}

			wantStatus := usecase.ProjectFieldsMissing
			if len(tc.want) == 0 {
				wantStatus = usecase.ProjectFieldsOK
			}
			if got.Status != wantStatus {
				t.Errorf("status = %q, want %q", got.Status, wantStatus)
			}
			if !slices.Equal(got.Missing, tc.want) {
				t.Errorf("missing = %q, want %q", got.Missing, tc.want)
			}
		})
	}
}

// 押す前の確認と作成は同じ判定を通す（#292）。
//
// 切れると: 確認が「揃っている」と言った Project で作成が 422 で止まる、または
// その逆。判定を 2 箇所に書いた時点で、どちらかが古くなる。
func TestCheckProjectFields_AgreesWithCreate(t *testing.T) {
	t.Parallel()

	for name, tc := range fieldCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			check, err := usecase.NewGitHubCatalogService(
				&fakeGitHub{fields: tc.fields}, usecase.ProjectFieldNames{}, nil,
			).CheckProjectFields(t.Context(), "PVT_1")
			if err != nil {
				t.Fatalf("CheckProjectFields() = %v", err)
			}

			gh := &fakeGitHub{fields: tc.fields}
			_, createErr := newCreationService(t, gh, &fakeMappings{}).Create(
				t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation())
			stopped := errors.Is(createErr, usecase.ErrProjectFieldMissing)

			if (check.Status == usecase.ProjectFieldsMissing) != stopped {
				t.Errorf("確認 = %q, 作成で止まったか = %v (%v)", check.Status, stopped, createErr)
			}
		})
	}
}

// 名前を変えた構成では、変えた名前で確かめる。
//
// 切れると: 設定した名前を作成だけが使い、確認は既定の名前で探す。名前を変えた
// 構成でだけ、揃っている Project に「足りない」と出る。
func TestCheckProjectFields_UsesConfiguredNames(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: []port.ProjectField{
		{ID: "F_kind", Name: "種別", Options: []port.ProjectFieldOption{
			{ID: "O_e", Name: "epic"}, {ID: "O_i", Name: "issue"},
		}},
		{ID: "F_parent", Name: "親"},
	}}

	configured, err := usecase.NewGitHubCatalogService(
		gh, usecase.ProjectFieldNames{Kind: "種別", Parent: "親"}, nil,
	).CheckProjectFields(t.Context(), "PVT_1")
	if err != nil {
		t.Fatalf("CheckProjectFields() = %v", err)
	}
	if configured.Status != usecase.ProjectFieldsOK {
		t.Errorf("設定した名前で: status = %q, want ok (%q)", configured.Status, configured.Missing)
	}

	// 既定の名前（Kind / Parent）で探すと見つからない。上が通るのは名前を
	// 渡したからであって、名前を見ていないからではない。
	defaults, err := usecase.NewGitHubCatalogService(
		gh, usecase.ProjectFieldNames{}, nil,
	).CheckProjectFields(t.Context(), "PVT_1")
	if err != nil {
		t.Fatalf("CheckProjectFields() = %v", err)
	}
	if defaults.Status != usecase.ProjectFieldsMissing {
		t.Errorf("既定の名前で: status = %q, want missing", defaults.Status)
	}
}

// 確かめるのは指定された Project。
func TestCheckProjectFields_AsksAboutTheGivenProject(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{}
	svc := usecase.NewGitHubCatalogService(gh, usecase.ProjectFieldNames{}, nil)

	if _, err := svc.CheckProjectFields(t.Context(), "PVT_42"); err != nil {
		t.Fatalf("CheckProjectFields() = %v", err)
	}
	if !slices.Equal(gh.projectIDs, []string{"PVT_42"}) {
		t.Errorf("projectIDs = %q, want [PVT_42]", gh.projectIDs)
	}
}

// 問い合わせに失敗したら unknown。エラーにも ok / missing にも倒さない。
//
// 切れると: GitHub が落ちているだけで、選択の画面に失敗が出る（エラーに倒す）か、
// 確かめていないのに「足りない」「揃っている」と出る。
func TestCheckProjectFields_UnknownWhenGitHubFails(t *testing.T) {
	t.Parallel()

	for name, listErr := range map[string]error{
		"GitHub が拒んだ":    port.ErrForbidden,
		"GitHub に届かなかった": errors.New("github api: 502"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			svc := usecase.NewGitHubCatalogService(
				&fakeGitHub{listErr: listErr}, usecase.ProjectFieldNames{}, nil)

			got, err := svc.CheckProjectFields(t.Context(), "PVT_1")
			if err != nil {
				t.Fatalf("CheckProjectFields() = %v, want nil", err)
			}
			if got.Status != usecase.ProjectFieldsUnknown {
				t.Errorf("status = %q, want unknown", got.Status)
			}
			if len(got.Missing) != 0 {
				t.Errorf("missing = %q, want empty", got.Missing)
			}
		})
	}
}

// 入力の誤りは unknown に混ぜず 400 で返す。
func TestCheckProjectFields_RequiresProjectID(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{}
	_, err := usecase.NewGitHubCatalogService(gh, usecase.ProjectFieldNames{}, nil).
		CheckProjectFields(t.Context(), "")
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("CheckProjectFields(\"\") = %v, want ErrInvalidInput", err)
	}
	if len(gh.projectIDs) != 0 {
		t.Errorf("空の ID で GitHub に問い合わせている: %q", gh.projectIDs)
	}
}
