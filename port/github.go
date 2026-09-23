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

// ProjectItemRef は作成・更新した ProjectV2Item を指す手掛かり。
type ProjectItemRef struct {
	// ItemID は ProjectV2Item の node ID。以後の操作はこれで指す。
	ItemID string
	// DatabaseID は ProjectV2Item の数値の識別子（GraphQL の fullDatabaseId）。
	// 0 は「知らない」。
	//
	// **URL の `itemId` に使うためだけに持つ**（ADR 0057）。draft issue には固有の
	// URL が無いので、Project の URL にこれを添えて item のペインを開かせる。
	// GitHub は BigInt を文字列で返すが、**境界の内側では整数で持つ。** 文字列の
	// まま運ぶと、URL に差し込む値の検査がもう 1 つ要る。
	DatabaseID int64
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
	// URL は Project のページ。
	//
	// **番号から組み立てず、GitHub が返したものをそのまま運ぶ。** Projects v2 の
	// URL は owner が user か org かで形が変わり、etoki はどちらなのかを知らない
	// （ADR 0025）。
	URL string
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

	// CanWriteProject は現在の利用者がその Project に書けるかを返す。
	//
	// **判定に使わない。画面に状態として見せるためのもの**（ADR 0017）。
	// etoki は GitHub 側の権限を複製しないと決めたので、作成できるかを最終的に
	// 決めるのは作成時の GitHub の応答であり、ここではない。
	CanWriteProject(ctx context.Context, projectID string) (bool, error)

	// CreateDraftIssue は draft issue を作成し、その ProjectV2Item を指す手掛かりを返す。
	//
	// ItemID は ProjectV2Item の ID であり、DraftIssue content の ID ではない。
	// 後続の SetItemFieldValue が前者を要求するため。
	//
	// DatabaseID は取れなければ 0 でよい。**取れないことを理由に失敗させない。**
	// item ごとのリンクを組めなくなるだけで、作成は取り消せない（ADR 0057）。
	CreateDraftIssue(ctx context.Context, projectID string, item DraftIssue) (ProjectItemRef, error)

	// UpdateDraftIssue は既存の draft issue の title と body を書き換える。
	//
	// itemID は CreateDraftIssue が返した ProjectV2Item の ID。**GitHub の更新は
	// DraftIssue content の ID を要求するので、実装がそれを自分で引き直す。**
	// 呼び出し側に 2 種類の ID を持ち分けさせない。sync_items が控えているのは
	// ProjectV2Item の ID だけであり（ADR 0007）、どちらを控えたのかが run ごとに
	// 変わると、あとから更新できる run とできない run が混ざる。
	//
	// item が draft issue でなくなっていたら（Project に本物の issue が
	// 紐づけられた等）、何も書き換えずにエラーを返す。
	//
	// 返す ProjectItemRef の ItemID は引数と同じ。DatabaseID は CreateDraftIssue と
	// 同じく取れなければ 0 でよい。**更新のついでに返させているのは、列を足す前に
	// 作った item の手掛かりを、更新した時点で埋めるため**（ADR 0057）。
	UpdateDraftIssue(ctx context.Context, itemID string, item DraftIssue) (ProjectItemRef, error)

	// SetItemFieldValue はアイテムのカスタムフィールドに値を設定する。
	SetItemFieldValue(ctx context.Context, projectID, itemID string, v FieldValue) error
}
