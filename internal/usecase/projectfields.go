package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// ProjectFieldNames は種別・親を表すカスタムフィールドの名前。
//
// **名前は設定で変わる**（`etoki.Options.KindFieldName` / `ParentFieldName`）。
// 作成と確認で同じ名前を見ないと、確認が「ある」と言ったフィールドで作成が
// 止まる。どちらにも `etoki.New` が同じ値を渡す。
type ProjectFieldNames struct {
	Kind   string
	Parent string
}

// withDefaults は空の名前を既定に落とす。
func (n ProjectFieldNames) withDefaults() ProjectFieldNames {
	if n.Kind == "" {
		n.Kind = DefaultKindFieldName
	}
	if n.Parent == "" {
		n.Parent = DefaultParentFieldName
	}
	return n
}

// ProjectFieldRequirement は作成に要るフィールドのうち、足りなかったものの 1 つ。
//
// **名前ではなく役割で表す。** 名前は設定で変わるので、境界に出すのは役割だけに
// する（#292）。名前は作成時のエラー本文が持つ。
type ProjectFieldRequirement string

// ProjectFieldRequirement の取りうる値。並びは画面に出す順。
const (
	// FieldKindEpic は種別のフィールドの選択肢 epic。
	FieldKindEpic ProjectFieldRequirement = "kind_epic"
	// FieldKindIssue は種別のフィールドの選択肢 issue。
	FieldKindIssue ProjectFieldRequirement = "kind_issue"
	// FieldParent は親を表すテキストのフィールド。
	FieldParent ProjectFieldRequirement = "parent"
)

// projectFields は作成に必要なカスタムフィールドの ID。
type projectFields struct {
	kindID        string
	epicOptionID  string
	issueOptionID string
	parentID      string
}

// matchProjectFields はフィールドの定義から、作成に要るものを名前で引き当てる。
//
// **足りないかどうかの判定はここだけ。** 作成（`CreationService.resolveFields`）と
// 押す前の確認（`GitHubCatalogService.CheckProjectFields`）が同じものを呼ぶ。
// 2 箇所に書くと、確認が「揃っている」と言った Project で作成が止まる。
//
// 選択肢は大文字小文字を区別せずに見る。フィールドの型（単一選択かテキストか）は
// 見ていない。**見るならここに足す。** 片方にだけ足すと、上と同じ食い違いになる。
//
// missing は足りなかったものを ProjectFieldRequirement の並びで返す。揃っていれば
// 空。
func matchProjectFields(
	all []port.ProjectField, names ProjectFieldNames,
) (fields projectFields, missing []ProjectFieldRequirement) {
	for _, f := range all {
		switch f.Name {
		case names.Kind:
			for _, o := range f.Options {
				switch strings.ToLower(o.Name) {
				case string(domain.KindEpic):
					fields.kindID, fields.epicOptionID = f.ID, o.ID
				case string(domain.KindIssue):
					fields.kindID, fields.issueOptionID = f.ID, o.ID
				}
			}
		case names.Parent:
			fields.parentID = f.ID
		}
	}

	if fields.epicOptionID == "" {
		missing = append(missing, FieldKindEpic)
	}
	if fields.issueOptionID == "" {
		missing = append(missing, FieldKindIssue)
	}
	if fields.parentID == "" {
		missing = append(missing, FieldParent)
	}

	return fields, missing
}

// describeMissingFields は足りないものを、名前つきで言う。作成時のエラー本文。
//
// **名前を載せるのはここ。** 画面は役割（`ProjectFieldRequirement`）から日本語を
// 引き、名前はこの本文として「詳細」に出る（#292）。
func describeMissingFields(missing []ProjectFieldRequirement, names ProjectFieldNames) string {
	parts := make([]string, 0, len(missing))
	for _, m := range missing {
		switch m {
		case FieldKindEpic:
			parts = append(parts, fmt.Sprintf("single select %q with option %q", names.Kind, domain.KindEpic))
		case FieldKindIssue:
			parts = append(parts, fmt.Sprintf("single select %q with option %q", names.Kind, domain.KindIssue))
		case FieldParent:
			parts = append(parts, fmt.Sprintf("text %q", names.Parent))
		}
	}
	return strings.Join(parts, ", ")
}

// ProjectFieldsStatus は作成先の Project に、作成に要るフィールドが揃っているかの、
// いまの状態。
//
// **判定ではない。** 実際に作れるかは作成時に GitHub が返したものが正しい。これは
// 画面に出すための材料で、これを見て選択や作成を止めることはしない
// （`ProjectAccess` と同じ、ADR 0017 / 0088）。
type ProjectFieldsStatus string

// ProjectFieldsStatus の取りうる値。
const (
	// ProjectFieldsOK は揃っていることを表す。
	ProjectFieldsOK ProjectFieldsStatus = "ok"
	// ProjectFieldsMissing は足りないものがあることを表す。
	ProjectFieldsMissing ProjectFieldsStatus = "missing"
	// ProjectFieldsUnknown は確かめられなかったことを表す。
	//
	// **ok か missing のどちらかに倒さない。** 倒すと、確かめていないことを
	// 確かめたように見せることになる（中核思想 3）。
	ProjectFieldsUnknown ProjectFieldsStatus = "unknown"
)

// ProjectFieldsCheck は作成先の Project のフィールドを確かめた結果。
type ProjectFieldsCheck struct {
	Status ProjectFieldsStatus
	// Missing は Status が missing のときだけ入る。
	Missing []ProjectFieldRequirement
}

// CheckProjectFields は作成先の Project に、作成に要るフィールドが揃っているかを
// 確かめる。
//
// **確かめられなかったことをエラーにしない。** 表示のための材料なので、GitHub が
// 落ちているだけで選択の画面に失敗を出す理由が無い（`BoardAccessService` の
// `projectAccess` と同じ扱い）。分からないことは分からないとして返す。
// 入力の誤り（空の projectID）だけは 400 で返す。
func (s *GitHubCatalogService) CheckProjectFields(
	ctx context.Context, projectID string,
) (ProjectFieldsCheck, error) {
	if projectID == "" {
		return ProjectFieldsCheck{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}

	all, err := s.github.ListProjectFields(ctx, projectID)
	if err != nil {
		s.logger.WarnContext(ctx, "could not check project fields",
			slog.String("projectId", projectID), slog.Any("error", err))
		return ProjectFieldsCheck{Status: ProjectFieldsUnknown}, nil
	}

	_, missing := matchProjectFields(all, s.fieldNames)
	if len(missing) > 0 {
		return ProjectFieldsCheck{Status: ProjectFieldsMissing, Missing: missing}, nil
	}

	return ProjectFieldsCheck{Status: ProjectFieldsOK}, nil
}
