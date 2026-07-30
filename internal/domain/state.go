package domain

// SyncState は注釈 1 つが取りうる 3 状態。
//
// この値は API で返してフロントに表示するためだけに使う。判定結果をもとに
// サーバーが更新や再作成を自動実行することはない。
type SyncState string

// SyncState の取りうる値。
const (
	// StateUncreated は一度も issue 化していないことを表す。
	StateUncreated SyncState = "uncreated"
	// StateCreated は issue 化済みで、その後ボードが変わっていないことを表す。
	StateCreated SyncState = "created"
	// StateChanged は issue 化済みだが、その後ボードが変わったことを表す。
	StateChanged SyncState = "changed"
)

// DecideState は最新の実行時ハッシュと現在のハッシュから 3 状態を決める。
//
// latest が nil のときは、その注釈が一度も実行されていないことを表す。
// 引数をポインタにしているのは domain が永続化層に依存しないためで、
// 「レコードなし」を呼び出し側が表現できるようにしている。
func DecideState(latest *ContentHash, current ContentHash) SyncState {
	switch {
	case latest == nil:
		return StateUncreated
	case *latest == current:
		return StateCreated
	default:
		return StateChanged
	}
}
