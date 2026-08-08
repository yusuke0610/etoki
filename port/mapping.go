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
type BoardRepository interface {
	// Create は新しいボードを保存する。ID が既存なら誤りとして扱う。
	Create(ctx context.Context, b Board) error
	// UpdateScene はシーンと更新時刻だけを更新する。CreatedAt は変えない。
	UpdateScene(ctx context.Context, id, scene string, updatedAt time.Time) error
	// UpdateTarget は作成先と更新時刻だけを更新する。Scene は変えない。
	//
	// 固定済みかどうかはここでは見ない。判断に sync_runs が要るため、
	// ユースケース層が担う（ADR 0014）。
	UpdateTarget(ctx context.Context, id string, t BoardTarget, updatedAt time.Time) error
	// Find は ID でボードを引く。存在しなければ (nil, nil) を返す。
	Find(ctx context.Context, id string) (*Board, error)
	// List は全ボードを UpdatedAt の降順で返す。
	List(ctx context.Context) ([]Board, error)
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
