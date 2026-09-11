package usecase

import (
	"context"
	"slices"
	"strings"

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

// DetachedAnnotation はシーンから消えた注釈が GitHub に残しているもの。
//
// **注釈にした frame をキャンバスから消して保存すると、その注釈は
// `Scene.Annotations()` に現れなくなる。** `sync_runs` / `sync_items` は残り、
// GitHub 側の draft issue も残る。**サーバーは読めるように作ってある**
// （`ListRuns` はシーンに残っているかを見ない、ADR 0007）のに、画面から辿る
// 導線だけが無かった。記録があるのに読めないのは、ADR 0009 が避けたかった
// 「etoki からは存在しないのと同じ」状態と同じことになる。
//
// **自動で消さない・自動で作り直さない。見せるだけにする**（中核思想 3）。
type DetachedAnnotation struct {
	// ID は消えた frame の要素 ID。
	//
	// **名前は取れない。** シーンから消えているので、`Annotation.Name` の
	// 出どころが無い。何で見分けるかは Items と LatestRun が担う。
	ID string

	// LatestRun は最後に issue 化したときの記録。**必ずある。**
	//
	// Items は run 履歴の畳み込みなので、Items があって run が無いことは
	// 起こらない。それでもポインタなのは、起こらないことを型で言い切ると
	// 永続化層の不整合が nil 参照になるため。
	LatestRun *port.SyncRun

	// Items はこの注釈が GitHub に在らしめているもの（ADR 0026）。
	//
	// **見分ける材料はここにしか無い。** frame の名前が取れない以上、
	// 「何を作った囲みだったのか」はここから読むしかない。
	Items []port.SyncItem
}

// MaxRunHistory は 1 回の問い合わせで返す run の件数。
//
// **範囲指定は持たない。** この口は「この注釈に対して直前まで何をしたか」を
// 辿るためのもので（ADR 0007）、数え上げのためではない。遡り続ける導線を
// 作ると、GitHub 側に在るものを数える手段として読まれる。そちらは畳み込み
// （ListItemsByAnnotation）の担当。
const MaxRunHistory = 20

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

// ListRuns はその注釈の実行履歴を新しい順で返す。
//
// **シーンに注釈が残っているかは見ない。** run は GitHub 側に在るものの追跡表
// なので、キャンバスから frame を消しても記録は残る（ADR 0007）。消えたことを
// 理由に読めなくすると、いちばん辿りたい場面で辿れない。
//
// 認可は状態の一覧と同じ viewer 以上。sync_runs をメンバーで絞らないのも同じ
// （board を取れるのはメンバーだけなので、二重に絞らない）。
func (s *AnnotationService) ListRuns(
	ctx context.Context, boardID, annotationID string,
) ([]port.SyncRun, error) {
	if _, err := s.access(ctx, boardID, port.RoleViewer); err != nil {
		return nil, err
	}

	return s.mappings.ListRunsByAnnotation(ctx, boardID, annotationID, MaxRunHistory)
}

// ListStates はボード上の全注釈の状態と、シーンから消えた注釈を返す。
//
// ボードを引き当てられなければ ErrBoardNotFound。注釈が 0 件の場合と区別が
// つく必要があるので、空スライスに丸めない。
//
// **2 つを 1 つのリストに混ぜない。** 消えた注釈には 3 状態の判定が無い
// （比べる相手のテキストがシーンに無い）し、名前も粒度も取れない。混ぜると、
// 決められない値を埋めるために架空の既定を置くことになる。
func (s *AnnotationService) ListStates(
	ctx context.Context, boardID string,
) ([]AnnotationState, []DetachedAnnotation, error) {
	// 状態を見るだけなので viewer でよい。
	acc, err := s.access(ctx, boardID, port.RoleViewer)
	if err != nil {
		return nil, nil, err
	}

	scene, err := domain.ParseScene([]byte(acc.Board.Scene))
	if err != nil {
		return nil, nil, err
	}

	runs, err := s.mappings.ListLatestRunsByBoard(ctx, boardID)
	if err != nil {
		return nil, nil, err
	}

	// 見せるものは畳み込みから取る。判定に使う最新 run とは出どころを分ける
	// （ADR 0026）。
	itemsByAnnotation, err := s.mappings.ListItemsByBoard(ctx, boardID)
	if err != nil {
		return nil, nil, err
	}

	latestByAnnotation := make(map[string]port.SyncRun, len(runs))
	for _, r := range runs {
		latestByAnnotation[r.AnnotationID] = r
	}

	annotations := scene.Annotations()
	states := make([]AnnotationState, 0, len(annotations))
	// シーンに残っている注釈を控える。畳み込みに在ってここに無いものが、
	// frame を消したまま保存された注釈。
	inScene := make(map[string]struct{}, len(annotations))

	for _, a := range annotations {
		inScene[a.ID] = struct{}{}
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

	return states, detachedAnnotations(itemsByAnnotation, latestByAnnotation, inScene), nil
}

// detachedAnnotations は畳み込みに在ってシーンに無い注釈を組み立てる。
//
// **材料はすでに取れている。** ListStates は畳み込みをボード全体で引いておき
// ながら、シーンに残っている注釈のぶんだけ配って残りを捨てていた。落として
// いたものを返すだけで、問い合わせは増えない。
//
// **1 件も作っていない注釈は出さない。** 消したこと自体を知らせるのが目的では
// なく、GitHub に残っているものへ辿れるようにするのが目的（ADR 0009）。
// 作っていないなら辿る先が無く、消した frame の残骸が並ぶだけになる。
//
// 並びは ID 順に固定する。map の反復順は実行ごとに変わるので、揃えないと
// 開き直すたびに並びが入れ替わる。
func detachedAnnotations(
	itemsByAnnotation map[string][]port.SyncItem,
	latestByAnnotation map[string]port.SyncRun,
	inScene map[string]struct{},
) []DetachedAnnotation {
	out := make([]DetachedAnnotation, 0)

	for id, items := range itemsByAnnotation {
		if _, ok := inScene[id]; ok {
			continue
		}
		if len(items) == 0 {
			continue
		}

		d := DetachedAnnotation{ID: id, Items: items}
		if run, ok := latestByAnnotation[id]; ok {
			r := run
			d.LatestRun = &r
		}
		out = append(out, d)
	}

	slices.SortFunc(out, func(a, b DetachedAnnotation) int {
		return strings.Compare(a.ID, b.ID)
	})

	return out
}
