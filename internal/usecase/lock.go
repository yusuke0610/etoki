package usecase

import (
	"context"
	"sync"
)

// BoardLocks はボード単位の排他。
//
// 作成先の固定（ADR 0014）は「run が 1 件でもあるか」で判断する。だが判断してから
// 記録するまでの間には GitHub への書き込みが挟まり、その間はまだ run が無い。
// ここで作成先を変えられると、作成先 A に作った draft issue が、作成先 B を指す
// ボードの run として残る。sync_runs が指す item を見失うのは、固定がまさに
// 防ごうとしていた事故そのものなので、判断と記録をまたいで直列化する。
//
// **ボードの削除も同じ鍵を取る**（ADR 0042）。作成の途中で消すと、GitHub には
// draft issue ができたのに run を書けず、取り消せない作成が記録の無いまま残る。
//
// プロセス内の排他で足りる。SQLite のファイルを複数プロセスで共有する構成は
// 想定していない（ADR 0004）。
type BoardLocks struct {
	mu sync.Mutex
	// sem はボードごとの容量 1 のセマフォ。sync.Mutex ではなくチャネルなのは、
	// 待っている間に ctx の終了を拾うため。
	sem map[string]chan struct{}
}

// NewBoardLocks は BoardLocks を作る。
//
// **BoardService と CreationService には同じものを渡す。** 別々に持たせると、
// 直列化したいはずの 2 つが素通りする。だから任意の設定ではなく引数にしてある。
func NewBoardLocks() *BoardLocks {
	return &BoardLocks{sem: make(map[string]chan struct{})}
}

// Acquire は boardID の排他を取り、解放する関数を返す。
//
// 待っている間に ctx が終われば ctx.Err() を返す。作成は GitHub の応答を
// 何度も待つので、切断されたリクエストが列に並び続けないようにする。
func (l *BoardLocks) Acquire(ctx context.Context, boardID string) (func(), error) {
	l.mu.Lock()
	sem, ok := l.sem[boardID]
	if !ok {
		sem = make(chan struct{}, 1)
		// 使い終わっても消さない。増えるのはこのプロセスが触ったボードの数まで
		// （消したボードのぶんも残る）で、参照カウントを持つほうが取りこぼした
		// ときの壊れ方が分かりにくい。
		l.sem[boardID] = sem
	}
	l.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
