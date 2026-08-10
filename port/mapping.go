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
}

// Selected は作成先が選ばれているかどうかを返す。
func (t BoardTarget) Selected() bool {
	return t.RepositoryOwner != "" && t.RepositoryName != "" && t.ProjectID != ""
}

// Board は 1 枚の Excalidraw ボード。
//
// スナップショットとバージョニングは行わないため、Scene は常に最新状態のみを
// 保持する。過去の状態が必要な場合、利用者がボードを複製する運用に委ねる。
type Board struct {
	// ID はサーバーが発番する UUID。
	ID string
	// Name は表示名。
	Name string
	// Scene は Excalidraw のシーン JSON。
	Scene string
	// OwnerUserID は所有者。
	//
	// 空文字は無効値ではなく「認証なしの所有者」1 人を表す（ADR 0016）。
	OwnerUserID string
	// Target は draft issue の作成先。未選択ならゼロ値。
	Target BoardTarget
	// CreatedAt は作成時刻。
	CreatedAt time.Time
	// UpdatedAt は最終更新時刻。
	UpdatedAt time.Time
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

// SyncItem は 1 回の実行で作成した draft issue 1 件。
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
	// LocalID は LLM 出力内の一時 ID。同一 run 内で一意。
	LocalID string
	// ParentLocalID は親の LocalID。トップレベルなら nil。
	ParentLocalID *string
	// CreatedAt は作成時刻。
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

// BoardRepository はボードを永続化する。
//
// 時刻は呼び出し側が与える。実装が time.Now を握ると挙動が時計に依存し、
// テストが書きづらくなるため。
//
// **所有者は引数で受け取る。** ctx から実装が勝手に読む形にすると、絞り忘れても
// 動いてしまう。引数なら渡し忘れがコンパイルエラーになる（ADR 0016）。
// 他人のボードは「存在しない」ものとして扱い、権限エラーとは区別しない。
type BoardRepository interface {
	// Create は新しいボードを保存する。ID が既存なら誤りとして扱う。
	// 所有者は Board.OwnerUserID から取る。
	Create(ctx context.Context, b Board) error
	// UpdateScene はシーンと更新時刻だけを更新する。CreatedAt は変えない。
	UpdateScene(ctx context.Context, owner, id, scene string, updatedAt time.Time) error
	// UpdateTarget は作成先と更新時刻だけを更新する。Scene は変えない。
	//
	// 固定済みかどうかはここでは見ない。判断に sync_runs が要るため、
	// ユースケース層が担う（ADR 0014）。
	UpdateTarget(ctx context.Context, owner, id string, t BoardTarget, updatedAt time.Time) error
	// Find は ID でボードを引く。存在しない、または他人のものなら (nil, nil)。
	Find(ctx context.Context, owner, id string) (*Board, error)
	// List は所有者のボードを UpdatedAt の降順で返す。
	List(ctx context.Context, owner string) ([]Board, error)

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
}
