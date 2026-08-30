package port

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound は更新対象が存在しないことを表す。
//
// 取得系（Find 系）は「無い」を異常として扱わず nil を返すため、このエラーは
// 更新系でのみ返る。
var ErrNotFound = errors.New("etoki: not found")

// ErrAlreadyExists は作ろうとしたものがすでにあることを表す。
//
// 同じ利用者を 2 回招待した場合に返る。既存の行を黙って書き換えると、
// 「招待した」と「ロールを変えた」を呼び出し側が区別できなくなる。
var ErrAlreadyExists = errors.New("etoki: already exists")

// ErrConflict は更新の基準にした版が、いまの版と食い違うことを表す。
//
// ErrNotFound と分ける。「書けなかった」は同じでも、直し方が違う。無いものは
// 待っても現れないが、食い違いは相手の変更を取り込めば進める（ADR 0020）。
var ErrConflict = errors.New("etoki: stale base version")

// BoardTarget は draft issue の作成先。
//
// ボードごとに持つ。3 つとも空なら未選択（ADR 0014）。
type BoardTarget struct {
	// RepositoryOwner は対象リポジトリの所有者。
	RepositoryOwner string
	// RepositoryName は対象リポジトリの名前。
	RepositoryName string
	// ProjectID は draft issue を作る Projects v2 の node ID。
	ProjectID string
	// ProjectNumber は Project の番号。表示用のスナップショット（ADR 0019）。
	ProjectNumber int
	// ProjectTitle は Project の名前。表示用のスナップショット（ADR 0019）。
	ProjectTitle string
	// ProjectURL は Project の URL。表示用のスナップショット（ADR 0025）。
	//
	// 組み立てずに GitHub が返したものを控える。Projects v2 の URL は owner が
	// user か org かで形が変わるが、etoki はどちらなのかを知らない。空文字は
	// 「URL を知らない」で、そのときフロントはリポジトリの Projects タブへ落とす。
	ProjectURL string
}

// BoardTargetDisplay は作成先の表示用スナップショット（ADR 0019 / 0025）。
//
// **作成先そのもの（owner / name / projectId）を含まない。** 固定後に更新
// できるのはここに挙がっているものだけ、を型で示すため（ADR 0037）。
type BoardTargetDisplay struct {
	// ProjectNumber は Project の番号。取得していなければ 0。
	ProjectNumber int
	// ProjectTitle は Project の名前。取得していなければ空文字。
	ProjectTitle string
	// ProjectURL は Project の URL。組み立てず GitHub が返したものを控える。
	ProjectURL string
}

// Selected は作成先が選ばれているかどうかを返す。
//
// **見るのは 3 つだけ。** ProjectNumber と ProjectTitle は表示用であり、
// 取得できていなくても作成先は決まっている（ADR 0019）。ここに足すと、
// 名前を送らずに設定した正しい作成先が「未選択」に落ちる。
func (t BoardTarget) Selected() bool {
	return t.RepositoryOwner != "" && t.RepositoryName != "" && t.ProjectID != ""
}

// Board は 1 枚の Excalidraw ボード。
//
// スナップショットとバージョニングは行わないため、Scene は常に最新状態のみを
// 保持する。過去の状態が必要な場合、利用者がボードを複製する運用に委ねる。
//
// 所有者は持たない。誰がどう関われるかはボードの属性ではなくメンバーシップ
// なので、BoardMember が持つ（ADR 0017）。
type Board struct {
	// ID はサーバーが発番する UUID。
	ID string
	// Name は表示名。
	Name string
	// Scene は Excalidraw のシーン JSON。
	Scene string
	// Target は draft issue の作成先。未選択ならゼロ値。
	Target BoardTarget
	// CreatedAt は作成時刻。
	CreatedAt time.Time
	// UpdatedAt は最終更新時刻。
	UpdatedAt time.Time
}

// BoardRole はボードに対する権限の強さ（ADR 0017）。
type BoardRole string

// BoardRole の取りうる値。
const (
	// RoleOwner は招待とロール変更、作成先の変更ができる。
	RoleOwner BoardRole = "owner"
	// RoleEditor はブレストと解釈と draft issue の作成ができる。
	//
	// 作成できるかを最終的に決めるのは GitHub。etoki は実行者のトークンで
	// 叩くので、リポジトリへのアクセス権が無ければ GitHub 側が拒む。
	RoleEditor BoardRole = "editor"
	// RoleViewer は読むだけ。
	//
	// 解釈も許さない。解釈は LLM を叩く外部呼び出しであり課金も伴うため、
	// 閲覧者に許すのは「閲覧」ではない（ADR 0017）。
	RoleViewer BoardRole = "viewer"
)

// rank はロールの強さを数にする。定義済みでなければ 0。
//
// **ロールの上下を知っているのはここだけ。** SQL にロールの集合を書くと
// 判定が 2 箇所になり、片方だけ変わる。永続化層が見るのは「メンバーかどうか」
// までにとどめる（ADR 0017）。
func (r BoardRole) rank() int {
	switch r {
	case RoleOwner:
		return 3
	case RoleEditor:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// Valid は r が定義済みのロールかどうかを返す。
func (r BoardRole) Valid() bool { return r.rank() > 0 }

// AtLeast は r が min 以上の権限を持つかどうかを返す。
//
// どちらかが未定義なら false。未知のロールを「とりあえず通す」方に倒さない。
func (r BoardRole) AtLeast(min BoardRole) bool {
	if !r.Valid() || !min.Valid() {
		return false
	}
	return r.rank() >= min.rank()
}

// BoardMember は 1 人のメンバー。
type BoardMember struct {
	// BoardID は対象ボードの ID。
	BoardID string
	// UserID は利用者の ID。
	//
	// 空文字は無効値ではなく「認証なしの所有者」1 人を表す（ADR 0016）。
	UserID string
	// Role は権限。
	Role BoardRole
	// CreatedAt はメンバーになった時刻。
	CreatedAt time.Time
}

// BoardAccess は操作者から見たボード 1 枚。
//
// ボードとロールを別々に引くと、引き当てと権限判定で 2 往復する。ロールは
// ボードの属性ではないが、「誰が見ているか」が決まれば 1 つに定まる。
type BoardAccess struct {
	// Board はボード本体。
	Board Board
	// Role は操作者のロール。
	Role BoardRole
}

// ItemKind は draft issue の種別。
//
// etoki が GitHub に作成するのは epic と issue の 2 階層のみである。
// 判断の経緯は docs/adr/0006-two-level-hierarchy.md を参照。
type ItemKind string

// ItemKind の取りうる値。
const (
	KindEpic  ItemKind = "epic"
	KindIssue ItemKind = "issue"
)

// Valid は k が定義済みの種別かどうかを返す。
func (k ItemKind) Valid() bool {
	return k == KindEpic || k == KindIssue
}

// SyncAction は 1 つの run がその item に対して何をしたかを表す。
type SyncAction string

// SyncAction の取りうる値。
const (
	// ActionCreated は draft issue を新しく作ったことを表す。
	ActionCreated SyncAction = "created"
	// ActionUpdated は既存の draft issue を書き換えたことを表す。
	ActionUpdated SyncAction = "updated"
)

// Valid は a が定義済みの操作かどうかを返す。
func (a SyncAction) Valid() bool {
	return a == ActionCreated || a == ActionUpdated
}

// SyncItem は 1 回の実行で作成または更新した draft issue 1 件。
type SyncItem struct {
	// ID は永続化層が発番する ID。保存前は 0。
	ID int64
	// RunID は所属する SyncRun の ID。保存前は 0。
	RunID int64
	// ItemID は GitHub の ProjectV2Item node ID。
	ItemID string
	// Kind は epic か issue。
	Kind ItemKind
	// Title は作成時のタイトル。
	Title string
	// Body は作成時の本文。
	//
	// Title と同じく作成時点のスナップショットである。GitHub からは取り
	// 直せないので、ここで控えなければ後から追えない（ADR 0023）。
	// 記録していなかった頃の run では空。
	Body string
	// LocalID は LLM 出力内の一時 ID。同一 run 内で一意。
	LocalID string
	// ParentLocalID は親の LocalID。トップレベルなら nil。
	ParentLocalID *string
	// Action はこの run がこの item に対して何をしたか（ADR 0026）。
	//
	// 記録していなかった頃の run では ActionCreated。当時は更新の経路が
	// 無かったので、すべて新規作成だった。
	Action SyncAction
	// CreatedAt は作成時刻。更新のときは書き換えた時刻。
	CreatedAt time.Time
}

// SyncRun は 1 つの注釈に対する 1 回分の issue 化実行。
//
// 再実行しても過去の run は残す。GitHub 側に残っている draft issue を
// 追跡できなくなるのを避けるためである。
type SyncRun struct {
	// ID は永続化層が発番する ID。保存前は 0。
	ID int64
	// BoardID は対象ボードの ID。
	BoardID string
	// AnnotationID は注釈にあたる Excalidraw 要素の ID。
	AnnotationID string
	// ContentHash は実行時点の注釈範囲のハッシュ。
	//
	// port は internal/ に依存しないため型は string にしている。
	// 算出は internal/domain が担う。
	ContentHash string
	// CreatedAt は実行時刻。
	CreatedAt time.Time
	// Items はこの実行で作成した draft issue。
	Items []SyncItem
}

// BoardRepository はボードとそのメンバーを永続化する。
//
// 時刻は呼び出し側が与える。実装が time.Now を握ると挙動が時計に依存し、
// テストが書きづらくなるため。
//
// **操作者は引数で受け取る。** ctx から実装が勝手に読む形にすると、絞り忘れても
// 動いてしまう。引数なら渡し忘れがコンパイルエラーになる（ADR 0016）。
// メンバーでないボードは「存在しない」ものとして扱い、権限エラーとは区別しない。
//
// **ここで見るのは「メンバーかどうか」まで。** ロールの上下は BoardRole.AtLeast が
// 1 箇所で持つ（ADR 0017）。SQL にロールの集合を書くと判定が 2 箇所になる。
type BoardRepository interface {
	// Create は新しいボードを保存し、owner をそのボードの RoleOwner にする。
	// ID が既存なら誤りとして扱う。
	//
	// ボードとメンバーは 1 トランザクションで入れる。片方だけ残ると、誰も
	// 開けないボードか、指す先の無いメンバーができる。
	Create(ctx context.Context, b Board, owner string) error
	// UpdateScene はシーンと更新時刻だけを更新する。CreatedAt は変えない。
	//
	// base は呼び出し側が編集の基準にした UpdatedAt。いまの版と違えば何も
	// 書かずに ErrConflict を返す（ADR 0020）。**照合と更新は 1 文で行う。**
	// 読んでから書く形にすると、その隙間に入った保存を上書きする。
	UpdateScene(ctx context.Context, actor, id, scene string, base, updatedAt time.Time) error
	// UpdateName は名前だけを更新する。
	//
	// **更新時刻は進めない。** `UpdatedAt` はシーンの版であり、保存の照合基準
	// として使われている（ADR 0020）。名前を直しただけで進めると、そのボードを
	// 開いている別のメンバーの次の保存が「他の人が保存した」で断られる。誰も
	// シーンを触っていないのに衝突として読まれることになるので、基準を取らない
	// 書き込みで版を前に出さない（ClaimUnowned と同じ考え方）。
	UpdateName(ctx context.Context, actor, id, name string) error
	// UpdateTarget は作成先と更新時刻だけを更新する。Scene は変えない。
	//
	// 固定済みかどうかはここでは見ない。判断に sync_runs が要るため、
	// ユースケース層が担う（ADR 0014）。
	UpdateTarget(ctx context.Context, actor, id string, t BoardTarget, updatedAt time.Time) error
	// UpdateTargetDisplay は作成先の表示用スナップショットと更新時刻だけを
	// 更新する。**作成先そのものは変えない。**
	//
	// 固定済みのボードでも通る唯一の経路（ADR 0037）。作成先の 3 列を書ける
	// ようにしないこと。書けるようにすると、固定が意味を失う。
	//
	// 送られた ID が保存されている作成先と同じかどうかはここでは見ない。
	// 引き当ててから比べる必要があるので、ユースケース層が担う。
	UpdateTargetDisplay(
		ctx context.Context, actor, id string, d BoardTargetDisplay, updatedAt time.Time,
	) error
	// Delete はボードを消す。存在しない、または操作者がメンバーでなければ
	// ErrNotFound。
	//
	// **ロールは見ない。** owner だけかはユースケース層が決める（他と同じ）。
	//
	// board_members / sync_runs / sync_items は一緒に消える。GitHub に作った
	// draft issue は消さない（消せない）ので、**残った draft issue の出どころは
	// 辿れなくなる**（ADR 0007 / 0042）。押す前に何が残るかを見せるのは
	// 呼び出し側の責務。
	Delete(ctx context.Context, actor, id string) error
	// Find は ID でボードを操作者のロールつきで引く。
	//
	// 存在しない、または操作者がメンバーでなければ (nil, nil)。
	Find(ctx context.Context, actor, id string) (*BoardAccess, error)
	// List は操作者がメンバーであるボードを UpdatedAt の降順で返す。
	//
	// **Board.Scene は空文字で返る。** 一覧はシーンを返さないので読まない。
	// シーンには画像が base64 で入りうるため、捨てるために全ボードぶんを
	// メモリへ載せることになる。中身が要るなら Find で 1 枚ずつ引く。
	List(ctx context.Context, actor string) ([]BoardAccess, error)

	// ListMembers はボードのメンバーを返す。
	//
	// **操作者では絞らない。呼ぶ前に Find で確かめること。** メンバー一覧は
	// board_id でしか引けず、その board を取れるのはメンバーだけなので、
	// 二重に絞ると、絞り忘れたときにどちらが効いているのか分からなくなる
	// （sync_runs と同じ理由、ADR 0016）。
	ListMembers(ctx context.Context, boardID string) ([]BoardMember, error)
	// AddMember はメンバーを 1 人足す。すでにメンバーなら ErrAlreadyExists。
	//
	// **操作者では絞らない。呼ぶ前に Find で owner であることを確かめること。**
	// ここに「owner だけ」を書くとロールの上下が SQL と Go の 2 箇所になる
	// （ADR 0017）。以下の 2 つも同じ。
	AddMember(ctx context.Context, m BoardMember) error
	// UpdateMemberRole はメンバーのロールを変える。
	// メンバーでなければ ErrNotFound。
	UpdateMemberRole(ctx context.Context, boardID, userID string, role BoardRole) error
	// RemoveMember はメンバーを外す。メンバーでなければ ErrNotFound。
	RemoveMember(ctx context.Context, boardID, userID string) error

	// CountUnowned は所有者の無いボードの数を返す。
	//
	// 認証を有効にした直後に、見えなくなったボードがあることを起動時に
	// 知らせるために使う。黙って消さない（ADR 0016）。
	CountUnowned(ctx context.Context) (int, error)
	// ClaimUnowned は所有者の無いボードをすべて owner のものにし、件数を返す。
	//
	// 更新時刻は変えない。引き受けはボードの中身を変えていないので、一覧の
	// 並びが入れ替わる理由が無い。
	ClaimUnowned(ctx context.Context, owner string) (int64, error)
}

// MappingRepository は注釈と draft issue の対応を永続化する。
type MappingRepository interface {
	// SaveRun は run とその Items を 1 トランザクションで保存し、発番された
	// run の ID を返す。途中で失敗した場合は run ごと保存されない。
	SaveRun(ctx context.Context, run SyncRun) (int64, error)
	// FindLatestRun は注釈の最新の run を Items 込みで返す。
	// 一度も実行されていなければ (nil, nil) を返す。
	FindLatestRun(ctx context.Context, boardID, annotationID string) (*SyncRun, error)
	// ListLatestRunsByBoard はボード内の注釈ごとに最新の run を返す。
	ListLatestRunsByBoard(ctx context.Context, boardID string) ([]SyncRun, error)

	// ListItemsByAnnotation はその注釈が GitHub に在らしめているものを返す。
	//
	// **最新 run の Items ではない。** run 履歴を ItemID で畳み、item ごとに
	// いちばん新しい記録を採る（ADR 0026）。更新は同じ ItemID に吸収され、
	// 今回触らなかった item も残り続ける。
	//
	// 最新 run だけを見ると、更新の run のあとに「前回作ったが今回は触らなかった」
	// item が画面から消える。GitHub 側には残っているのに etoki が見せなくなるのは、
	// 状態を見せるという方針に反する（中核思想 3）。
	//
	// 一度も実行していなければ空を返す。並びは記録された順。
	ListItemsByAnnotation(ctx context.Context, boardID, annotationID string) ([]SyncItem, error)

	// ListRunsByAnnotation はその注釈の run を新しい順で Items 込みに返す。
	//
	// **畳み込まない。** ListItemsByAnnotation が返すのは「いま GitHub に在る
	// もの」で、こちらは「1 回ずつ何をしたか」（ADR 0007 / 0026）。畳み込みは
	// 更新を同じ item に吸収するので、何回に分けて作ったのかは残らない。
	//
	// **新しさは CreatedAt ではなく ID で決める。** 時刻は呼び出し側が与える
	// ので、同じ時刻の run がありうる（FindLatestRun と同じ理由）。
	//
	// limit 件まで返す。0 以下なら 1 件も返さない。
	ListRunsByAnnotation(
		ctx context.Context, boardID, annotationID string, limit int,
	) ([]SyncRun, error)

	// ListItemsByBoard は同じ畳み込みをボード全体で行い、注釈ごとに束ねて返す。
	//
	// 注釈ごとに ListItemsByAnnotation を呼ぶと、注釈の数だけ問い合わせが増える。
	// 一覧は全注釈を一度に描くので、まとめて引く経路を分けてある。
	ListItemsByBoard(ctx context.Context, boardID string) (map[string][]SyncItem, error)
}
