package usecase

import (
	"context"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// AnnotationCounts は注釈の 3 状態ごとの件数。
type AnnotationCounts struct {
	Uncreated int
	Created   int
	Changed   int
}

// BoardListEntry は一覧に載せるボード 1 枚。
type BoardListEntry struct {
	port.BoardAccess
	// Counts は注釈の 3 状態ごとの件数。**シーンを読めなかったら nil。**
	// 注釈が 1 つも無いボード（0 件）と区別する。
	Counts *AnnotationCounts
}

// List は操作者がメンバーであるボードを、注釈の件数つきで更新時刻の降順に
// 返す（#200）。開く前に、どのボードに手を打つものがあるかを見せるため
// （中核思想 3）。
//
// **シーンは 1 枚ずつ読み、数えたら手放す。** 一覧の問い合わせ
// （BoardRepository.List）はシーンを読まない。全ボードぶんを一度に読むと、
// 一覧を出すだけでその合計がメモリに載る。**読む量そのものは減らない**
// （ADR 0068）。貼った画像はシーンとは別に持つので、ここでは読まない
// （ADR 0074）。
//
// **1 枚が読めなくても一覧は返す。** そのボードだけ Counts を nil にする。run を
// 引けないのは DB の失敗なので、一覧ごと失敗にする。
func (s *BoardService) List(ctx context.Context) ([]BoardListEntry, error) {
	actor := actorOf(ctx)
	listed, err := s.boards.List(ctx, actor)
	if err != nil {
		return nil, err
	}

	out := make([]BoardListEntry, 0, len(listed))
	for _, a := range listed {
		full, err := s.boards.Find(ctx, actor, a.Board.ID)
		if err != nil {
			return nil, err
		}
		// 一覧を引いてから数えるまでに消えた（かメンバーから外れた）。押しても
		// not_found が返るだけなので並べない。
		if full == nil {
			continue
		}

		scene, err := domain.ParseScene([]byte(full.Board.Scene))
		if err != nil {
			// 保存時に検証しているので、ふつうは来ない。来たら件数だけを諦める。
			// 開けば、読めなかったことはボードの画面に出る。
			out = append(out, BoardListEntry{BoardAccess: a})
			continue
		}

		counts, err := s.countAnnotations(ctx, a.Board.ID, scene)
		if err != nil {
			return nil, err
		}
		out = append(out, BoardListEntry{BoardAccess: a, Counts: &counts})
	}
	return out, nil
}

// countAnnotations はシーンに残っている注釈を 3 状態ごとに数える。判定は注釈の
// 状態の一覧と同じ関数を通す（judgeAnnotations）。
func (s *BoardService) countAnnotations(
	ctx context.Context, boardID string, scene domain.Scene,
) (AnnotationCounts, error) {
	runs, err := s.mappings.ListLatestRunsByBoard(ctx, boardID)
	if err != nil {
		return AnnotationCounts{}, err
	}

	var c AnnotationCounts
	for _, j := range judgeAnnotations(scene, latestRunsByAnnotation(runs)) {
		switch j.state {
		case domain.StateUncreated:
			c.Uncreated++
		case domain.StateCreated:
			c.Created++
		case domain.StateChanged:
			c.Changed++
		}
	}
	return c, nil
}
