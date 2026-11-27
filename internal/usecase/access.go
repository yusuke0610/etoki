package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// ErrBoardNotFound は対象のボードが存在しないことを表す。
//
// **メンバーでないボードもこれになる。** 区別すると、ID を総当たりして他人の
// ボードの存在を確かめられる（ADR 0016）。
var ErrBoardNotFound = errors.New("etoki: board not found")

// actorOf は ctx から操作者を返す。
//
// 認証を設定していなければ空文字になる。空文字は無効値ではなく「認証なしの
// 操作者」1 人を表すので、その構成では全ボードがその 1 人のものになり、
// これまでどおり全部が見える（ADR 0016）。
func actorOf(ctx context.Context) string {
	actor, _ := port.UserIDFromContext(ctx)
	return actor
}

// boardGuard はボードを引き当て、操作者がメンバーかを確かめる。
//
// ボードに触るユースケースはすべてこれを埋め込み、**`boards.Find` を直に
// 呼ばない。** 判定を 1 箇所に閉じておかないと、足した操作だけ引き当てられ
// なかったボードを「無い」と返し損ねる、という取りこぼしが起きる。
//
// **見るのはメンバーかどうかだけ。** メンバーなら、どの操作も通す（ADR 0107）。
type boardGuard struct {
	boards port.BoardRepository
}

// access は操作者から見たボードを、メンバーであることを確かめてから返す。
//
// メンバーでなければ ErrBoardNotFound。
func (g boardGuard) access(ctx context.Context, boardID string) (*port.BoardAccess, error) {
	a, err := g.boards.Find(ctx, actorOf(ctx), boardID)
	if err != nil {
		return nil, err
	}
	return found(a, boardID)
}

// accessWithFiles は access と同じ判定のうえで、ボードを画像ごと返す。
//
// 画像まで要るのはボードを開くときだけ（ADR 0074）。**判定は access と同じ
// found を通す。** 口が 2 つになっても、判定は 1 つのまま保つ。
func (g boardGuard) accessWithFiles(
	ctx context.Context, boardID string,
) (*port.BoardAccess, []port.BoardFile, error) {
	a, files, err := g.boards.FindWithFiles(ctx, actorOf(ctx), boardID)
	if err != nil {
		return nil, nil, err
	}
	if a, err = found(a, boardID); err != nil {
		return nil, nil, err
	}
	return a, files, nil
}

// found は引き当てられたかを確かめる。引き当てられなければ ErrBoardNotFound。
func found(a *port.BoardAccess, boardID string) (*port.BoardAccess, error) {
	if a == nil {
		return nil, fmt.Errorf("%w: %s", ErrBoardNotFound, boardID)
	}
	return a, nil
}
