package usecase

import (
	"context"

	"github.com/yusuke0610/etoki/port"
)

// BoardDeletion は削除で etoki から失われるもの（ADR 0042）。
//
// **GitHub 側で何が起きるかは含まれない。** etoki は draft issue を消さない
// ので、GitHub にはそのまま残る。ここが答えるのは「辿れなくなるのはどれだけか」
// という問いのほう。
type BoardDeletion struct {
	// RecordedItemCount はそのボードから作成したと記録している draft issue の
	// 件数。
	//
	// **GitHub 上でまだ在るかどうかは分からない**（ADR 0007）。記録の件数で
	// あって現況ではない。
	RecordedItemCount int
	// UnconfirmedWriteCount は GitHub に届いたか分からない書き込みの件数
	// （ADR 0056）。**RecordedItemCount に混ぜない。** draft issue の件数では
	// なく書き込みの回数で、作られたかどうかは分からない。
	UnconfirmedWriteCount int
}

// Deletion は削除で失われるものを返す。
//
// **押される前に見せるための口**（ADR 0042、中核思想 3）。削除は取り消せず、
// GitHub 側に作った draft issue も消せないので、何が残るのかを先に出す。
//
// **owner だけ。** 削除そのものと揃える。押せない相手に、押したときに何が
// 起きるかだけを見せる理由が無い。
//
// **引いた時点の件数であって、削除の前提条件ではない。** 引いてから削除するまで
// に作成が走れば増えるが、返した件数を Delete で照合しない。照合すると、増えた
// ぶんを見せ直すために削除が断られることになり、畳めないボードを残さないという
// 目的（ADR 0042）から遠のく。作成の直列化が守っているのは記録の整合であって、
// この件数ではない。
func (s *BoardService) Deletion(ctx context.Context, id string) (BoardDeletion, error) {
	if _, err := s.access(ctx, id, port.RoleOwner); err != nil {
		return BoardDeletion{}, err
	}

	// **注釈のカードに出している「いま GitHub に在る N 件」と同じ畳み込みから
	// 採る**（ADR 0026）。別に数えると、同じボードについて画面が 2 つの数を
	// 出すことになる。
	byAnnotation, err := s.mappings.ListItemsByBoard(ctx, id)
	if err != nil {
		return BoardDeletion{}, err
	}

	count := 0
	for _, items := range byAnnotation {
		count += len(items)
	}

	// **届いたか分からない書き込みも別に数える**（ADR 0042 の追記）。数えないと、
	// 未確定しか無いボードで「作成した記録はありません」と言って消させる。
	// 注釈の詳細の `unconfirmedItems` と同じ口から採る。
	unconfirmedByAnnotation, err := s.mappings.ListUnconfirmedItemsByBoard(ctx, id)
	if err != nil {
		return BoardDeletion{}, err
	}
	unconfirmed := 0
	for _, items := range unconfirmedByAnnotation {
		unconfirmed += len(items)
	}

	return BoardDeletion{RecordedItemCount: count, UnconfirmedWriteCount: unconfirmed}, nil
}

// Delete はボードを消す。
//
// **owner だけ**（ADR 0017）。ボードごと畳む操作なので、シーンを書き換え
// られる editor ではなく、招待も作成先の変更もできる相手に限る。
//
// **run があっても拒まない。** 拒むと、いちばん畳みたいボード（作成先を
// 間違えたまま 1 回作ってしまったボード）が永久に残る。失われるものは
// Deletion で見せ、決めるのは開発者にする（ADR 0042、中核思想 3）。
//
// **作成と直列化する。** 作成の途中で消すと、GitHub には draft issue が
// できたのに run を書けず、取り消せない作成が記録の無いまま残る（ADR 0009）。
// 作成先の変更と同じ鍵を取る。
func (s *BoardService) Delete(ctx context.Context, id string) error {
	release, err := s.locks.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer release()

	if _, err := s.access(ctx, id, port.RoleOwner); err != nil {
		return err
	}

	return s.boards.Delete(ctx, actorOf(ctx), id)
}
