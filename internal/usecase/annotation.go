package usecase

import (
	"context"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// AnnotationState は注釈 1 つの現在の状態。
type AnnotationState struct {
	// Annotation はシーンから読み取った注釈。
	Annotation domain.Annotation
	// State は 3 状態の判定結果。
	State domain.SyncState
	// LatestRun は最後に issue 化したときの記録。未実行なら nil。
	//
	// 判定に使うのはハッシュと実行時刻だけ。**中身を見せるのに使わない。**
	// 何が GitHub に在るかは Items が持つ。
	LatestRun *port.SyncRun
	// Items はこの注釈が GitHub に在らしめているもの（ADR 0026）。
	//
	// **最新 run の Items ではなく、run 履歴を畳んだもの。** 最新 run だけを
	// 見せると、更新の run のあとに「前回作ったが今回は触らなかった」item が
	// 画面から消える。GitHub 側には残っているのに etoki が見せなくなるのは、
	// 状態を見せるという方針に反する（中核思想 3）。
	Items []port.SyncItem
}

// AnnotationService は注釈の状態を組み立てる。
//
// この層は状態を返すだけで、作成や更新は一切行わない。何をするかは
// 開発者が明示的にトリガーする。
type AnnotationService struct {
	boardGuard
	mappings port.MappingRepository
}

// NewAnnotationService は AnnotationService を作る。
func NewAnnotationService(boards port.BoardRepository, mappings port.MappingRepository) *AnnotationService {
	return &AnnotationService{boardGuard: boardGuard{boards: boards}, mappings: mappings}
}

// ListStates はボード上の全注釈の状態を返す。
//
// ボードを引き当てられなければ ErrBoardNotFound。注釈が 0 件の場合と区別が
// つく必要があるので、空スライスに丸めない。
func (s *AnnotationService) ListStates(ctx context.Context, boardID string) ([]AnnotationState, error) {
	// 状態を見るだけなので viewer でよい。
	acc, err := s.access(ctx, boardID, port.RoleViewer)
	if err != nil {
		return nil, err
	}

	scene, err := domain.ParseScene([]byte(acc.Board.Scene))
	if err != nil {
		return nil, err
	}

	runs, err := s.mappings.ListLatestRunsByBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}

	// 見せるものは畳み込みから取る。判定に使う最新 run とは出どころを分ける
	// （ADR 0026）。
	itemsByAnnotation, err := s.mappings.ListItemsByBoard(ctx, boardID)
	if err != nil {
		return nil, err
	}

	latestByAnnotation := make(map[string]port.SyncRun, len(runs))
	for _, r := range runs {
		latestByAnnotation[r.AnnotationID] = r
	}

	annotations := scene.Annotations()
	states := make([]AnnotationState, 0, len(annotations))

	for _, a := range annotations {
		current := scene.AnnotationHash(a)

		var (
			latestHash *domain.ContentHash
			latestRun  *port.SyncRun
		)
		if run, ok := latestByAnnotation[a.ID]; ok {
			// ループ変数のアドレスを取らないよう、明示的に複製する。
			r := run
			latestRun = &r
			h := domain.ContentHash(run.ContentHash)
			latestHash = &h
		}

		states = append(states, AnnotationState{
			Annotation: a,
			State:      domain.DecideState(latestHash, current),
			LatestRun:  latestRun,
			Items:      itemsByAnnotation[a.ID],
		})
	}

	return states, nil
}
