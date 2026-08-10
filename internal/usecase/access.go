package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/yusuke0610/etoki/port"
)

// 認可に固有のエラー。
var (
	// ErrBoardNotFound は対象のボードが存在しないことを表す。
	//
	// **メンバーでないボードもこれになる。** 権限エラーと区別すると、ID を
	// 総当たりして他人のボードの存在を確かめられる（ADR 0016 / 0017）。
	ErrBoardNotFound = errors.New("etoki: board not found")

	// ErrForbidden はメンバーではあるが、その操作にロールが足りないことを表す。
	//
	// **ボードの存在をすでに知っている相手にだけ返る。** 何が足りないのかを
	// 隠す理由が無いので、ErrBoardNotFound に丸めない（ADR 0017）。
	ErrForbidden = errors.New("etoki: insufficient role")
)

// actorOf は ctx から操作者を返す。
//
// 認証を設定していなければ空文字になる。空文字は無効値ではなく「認証なしの
// 操作者」1 人を表すので、その構成では全ボードがその 1 人のものになり、
// これまでどおり全部が見える（ADR 0016）。
func actorOf(ctx context.Context) string {
	actor, _ := port.UserIDFromContext(ctx)
	return actor
}

// boardGuard はボードを引き当て、操作者のロールを確かめる。
//
// ボードに触るユースケースはすべてこれを埋め込み、**`boards.Find` を直に
// 呼ばない。** 判定を 1 箇所に閉じておかないと、足した操作だけロールを見ない、
// という取りこぼしが起きる。
type boardGuard struct {
	boards port.BoardRepository
}

// access は操作者から見たボードを、min 以上のロールを確かめてから返す。
//
// メンバーでなければ ErrBoardNotFound、ロールが足りなければ ErrForbidden。
func (g boardGuard) access(
	ctx context.Context, boardID string, min port.BoardRole,
) (*port.BoardAccess, error) {
	a, err := g.boards.Find(ctx, actorOf(ctx), boardID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("%w: %s", ErrBoardNotFound, boardID)
	}
	if !a.Role.AtLeast(min) {
		return nil, fmt.Errorf("%w: %s needs %s, actor is %s",
			ErrForbidden, boardID, min, a.Role)
	}

	return a, nil
}
