package port

import "context"

// ProjectFieldOption は単一選択フィールドの選択肢。
type ProjectFieldOption struct {
	// ID は GraphQL のオプション ID。
	ID string
	// Name は表示名。
	Name string
}

// ProjectField は Projects v2 のカスタムフィールド。
type ProjectField struct {
	// ID は GraphQL のフィールド ID。
	ID string
	// Name は表示名。
	Name string
	// DataType は "TEXT" や "SINGLE_SELECT" など。
	DataType string
	// Options は DataType が単一選択のときの選択肢。
	Options []ProjectFieldOption
}

// DraftIssue は新規に作成する draft issue の内容。
//
// draft issue にはラベルを付けられないため、種別や親子関係はここには含めず、
// 作成後に SetItemFieldValue でカスタムフィールドとして設定する。
type DraftIssue struct {
	// Title は draft issue のタイトル。
	Title string
	// Body は draft issue の本文。
	Body string
}

// FieldValue は 1 つのカスタムフィールドに設定する値。
// Text と OptionID はどちらか一方だけを設定する。
type FieldValue struct {
	// FieldID は対象フィールドの ID。
	FieldID string
	// Text はテキストフィールドに設定する値。
	Text *string
	// OptionID は単一選択フィールドに設定する選択肢の ID。
	OptionID *string
}

// Repository は draft issue の作成先を選ぶときに見せるリポジトリ。
type Repository struct {
	// Owner は所有者の login。
	Owner string
	// Name はリポジトリ名。
	Name string
	// Description は説明文。無ければ空。
	Description string
}

// Project はリポジトリに紐づく Projects v2。
type Project struct {
	// ID は GraphQL の node ID。作成先として保存するのはこれ。
	ID string
	// Number はリポジトリ内での番号。URL に出るので画面に添える。
	Number int
	// Title は表示名。
	Title string
}

// GitHubClient は GitHub Projects v2 を操作する。
type GitHubClient interface {
	// ListRepositories は利用者が書き込めるリポジトリを返す。
	//
	// トークンに repo の read が無いと 0 件になる。権限不足と
	// 「本当に 1 つも無い」は区別できないため、呼び出し側で案内する。
	ListRepositories(ctx context.Context) ([]Repository, error)

	// ListRepositoryProjects はリポジトリに紐づく Projects v2 を返す。
	//
	// draft issue はリポジトリではなく Project に属する。利用者が選ぶのは
	// リポジトリだが、保存するのはここで選ばれた Project（ADR 0014）。
	ListRepositoryProjects(ctx context.Context, owner, name string) ([]Project, error)

	// ListProjectFields はプロジェクトのカスタムフィールド定義を返す。
	// 種別や親子関係を設定するにはフィールド ID の解決が必要になる。
	ListProjectFields(ctx context.Context, projectID string) ([]ProjectField, error)

	// CreateDraftIssue は draft issue を作成し、その ProjectV2Item ID を返す。
	//
	// 返すのは ProjectV2Item の ID であり、DraftIssue content の ID ではない。
	// 後続の SetItemFieldValue が前者を要求するため。
	CreateDraftIssue(ctx context.Context, projectID string, item DraftIssue) (itemID string, err error)

	// SetItemFieldValue はアイテムのカスタムフィールドに値を設定する。
	SetItemFieldValue(ctx context.Context, projectID, itemID string, v FieldValue) error
}
