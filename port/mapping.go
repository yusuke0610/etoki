package port

import (
	"context"
	"time"
)

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
	// ItemDatabaseID は ProjectV2Item の数値の識別子。0 は「知らない」。
	//
	// Title / Body と同じく作成時に控えるが、**意味は違う。** あちらは「その
	// とき書いたものの写し」で、これは item の不変の属性。畳み込みで採る行が
	// 違うのはそのため（ADR 0057）。記録していなかった頃の run では 0。
	ItemDatabaseID int64
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
	// Confirmed は書き込みが GitHub に届いたことを確かめられたかどうか。
	//
	// **ゼロ値は false、つまり「分からない」。** 確定を既定にすると、詰め替えの
	// 経路で 1 つ写し忘れただけの行が「確かに作った」として並ぶ。分からない側に
	// 倒れるほうが、読む側の打ち手（GitHub を見にいく）を間違えさせない
	// （ADR 0043 が結末のゼロ値を成功に倒さないのと同じ）。
	//
	// false になるのは、GitHub が受理したかどうかを etoki が知りえないとき。
	// 作成では ItemID が空になり（ID が返ってこなかった）、更新では相手の
	// ItemID は分かるが、書き換えが届いたかが分からない（ADR 0056）。
	//
	// **畳み込み（ADR 0026）には入らない。** 在るかどうかが分からないものを
	// 「いま GitHub に在るもの」に混ぜない。
	Confirmed bool
	// CreatedAt は作成時刻。更新のときは書き換えた時刻。
	CreatedAt time.Time
}

// RunOutcome はその run が最後まで進んだかどうか（ADR 0043）。
type RunOutcome string

// RunOutcome の取りうる値。
const (
	// OutcomeUnknown は記録していなかった頃の run。**「成功した」ではない。**
	//
	// ゼロ値。この項目を足す前にも途中失敗は起きていたので、読む側は
	// OutcomeComplete と同じに扱ってはいけない。
	OutcomeUnknown RunOutcome = ""
	// OutcomeComplete は解釈にあったものを最後まで作りきった run。
	OutcomeComplete RunOutcome = "complete"
	// OutcomeIncomplete は途中で失敗した run。作れたぶんは Items に入る。
	OutcomeIncomplete RunOutcome = "incomplete"
)

// Valid は o が記録として書ける値かどうかを返す。
//
// **OutcomeUnknown は false。** 読むときには現れるが、これから保存する run は
// 必ずどちらかに決まっている。ゼロ値を通すと、書き忘れが「記録していない
// 古い run」として静かに混ざる。
func (o RunOutcome) Valid() bool {
	return o == OutcomeComplete || o == OutcomeIncomplete
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
	// Outcome は最後まで進んだかどうか（ADR 0043）。
	//
	// 記録していなかった頃の run では OutcomeUnknown。**成功として扱わない。**
	Outcome RunOutcome
	// Error は途中で失敗した理由。完走した run と、記録していなかった頃の
	// run では空。
	//
	// GitHub が返した文字列をそのまま持つ。**利用者向けの文言ではなく手掛かり**
	// なので、画面に出すときは畳む（ADR 0034）。
	Error string
	// Items はこの実行で作成した draft issue。
	Items []SyncItem
}

// MappingRepository は注釈と draft issue の対応を永続化する。
type MappingRepository interface {
	// SaveRun は run とその Items を 1 トランザクションで保存し、発番された
	// run の ID を返す。途中で失敗した場合は run ごと保存されない。
	//
	// **Outcome は必須**（`Valid()` が真であること）。Error は
	// OutcomeIncomplete のときだけ入れる。どちらも満たさなければ誤りとして
	// 弾く。ゼロ値を通すと、書き忘れが「記録していなかった頃の run」に
	// 化ける（ADR 0043）。
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

	// ListUnconfirmedItemsByBoard は届いたか分からない書き込みを注釈ごとに返す
	// （ADR 0056）。
	//
	// **畳み込まない。** 未確定の作成は ItemID を持たないので、畳む鍵が無い。
	// 1 回ずつが別々の「届いたかもしれない書き込み」であり、まとめる根拠も無い。
	//
	// **あとで同じ item に確定の記録が付いたものは返さない。** 未確定の更新の
	// あとにその item へ書けたなら、いま GitHub に何があるかは分かっている。
	// 解けた不確かさを残すと、打ち手の無い警告が積み上がる。ItemID を持たない
	// 未確定の作成は、後から確定させる手段が無いので残り続ける。
	//
	// **自動では消さない**（中核思想 3）。GitHub を見て確かめるのは開発者。
	// 並びは記録された順。
	ListUnconfirmedItemsByBoard(ctx context.Context, boardID string) (map[string][]SyncItem, error)
}
