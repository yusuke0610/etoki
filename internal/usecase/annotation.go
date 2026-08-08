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
	// 「変更あり」のときに前回何を作ったかを見せるために返す。判定に使うのは
	// ハッシュだけだが、開発者が判断するには前回の作成物が要る。
	LatestRun *port.SyncRun
}

// AnnotationService は注釈の状態を組み立てる。
//
// この層は状態を返すだけで、作成や更新は一切行わない。何をするかは
// 開発者が明示的にトリガーする。
type AnnotationService struct {
	boards   port.BoardRepository
	mappings port.MappingRepository
}

// NewAnnotationService は AnnotationService を作る。
func NewAnnotationService(boards port.BoardRepository, mappings port.MappingRepository) *AnnotationService {
	return &AnnotationService{boards: boards, mappings: mappings}
}

// ListStates はボード上の全注釈の状態を返す。
// ボードが存在しなければ (nil, nil) を返す。
func (s *AnnotationService) ListStates(ctx context.Context, boardID string) ([]AnnotationState, error) {
	board, err := s.boards.Find(ctx, ownerOf(ctx), boardID)
	if err != nil {
		return nil, err
	}
	if board == nil {
		return nil, nil
	}

	scene, err := domain.ParseScene([]byte(board.Scene))
	if err != nil {
		return nil, err
	}

	runs, err := s.mappings.ListLatestRunsByBoard(ctx, boardID)
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
		})
	}

	return states, nil
}
