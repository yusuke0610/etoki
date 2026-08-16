// Package usecase は etoki のユースケース層。
//
// ここから外部に触れるときは必ず port のインターフェースを経由する。
// SQLite / GitHub / LLM の具体的な型をこの層が知ることはない。
package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// ErrInvalidInput は入力が要件を満たしていないことを表す。
var ErrInvalidInput = errors.New("etoki: invalid input")

// ErrTargetLocked は作成先を変えられないことを表す。
//
// そのボードで draft issue を 1 件でも作ったら固定する。sync_runs は GitHub 側に
// 残っている item の追跡表であり、作成先が変わると記録が指す先を見失う（ADR 0014）。
var ErrTargetLocked = errors.New("etoki: board target is locked")

// ErrSceneConflict は保存の基準にした版が古いことを表す。
//
// ボードは共有できるので（ADR 0017）、2 人が同時に描くのは例外ではない。保存は
// シーン全体を書くため、後勝ちで通すと消えるのは「相手が触った要素」ではなく
// 相手の作業すべてになる。黙って一方を採るのではなく、食い違っていることを
// 呼び出し側に返す（ADR 0020）。
var ErrSceneConflict = errors.New("etoki: board scene was updated by someone else")

// BoardService はボードの作成・取得・更新を担う。
type BoardService struct {
	boardGuard
	mappings port.MappingRepository
	locks    *BoardLocks
	now      func() time.Time
	newID    func() string
}

// BoardServiceOption は BoardService の依存を差し替える。
type BoardServiceOption func(*BoardService)

// WithClock は時刻の取得方法を差し替える。テストで時刻を固定するために使う。
func WithClock(f func() time.Time) BoardServiceOption {
	return func(s *BoardService) { s.now = f }
}

// WithIDGenerator は ID の採番方法を差し替える。
func WithIDGenerator(f func() string) BoardServiceOption {
	return func(s *BoardService) { s.newID = f }
}

// NewBoardService は BoardService を作る。
//
// mappings を取るのは作成先の固定判定に使うため。ボードの読み書きだけなら
// 要らないが、固定はユースケース層で守ると決めた（ADR 0014）。
//
// locks は CreationService と同じものを渡す。固定の判定と、判定の前提を崩す
// 作成とを直列化するため（BoardLocks を参照）。
func NewBoardService(
	boards port.BoardRepository, mappings port.MappingRepository, locks *BoardLocks,
	opts ...BoardServiceOption,
) *BoardService {
	s := &BoardService{
		boardGuard: boardGuard{boards: boards},
		mappings:   mappings,
		locks:      locks,
		now:        time.Now,
		newID:      uuid.NewString,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create は新しいボードを作る。scene が空なら空のシーンで初期化する。
//
// **作成先は必須。** 候補は書ける Project だけに絞ってあるので（ADR 0014）、
// 書ける先を 1 つも持たない人はボードを作れない。「ボードの作成にはリポジトリ
// へのアクセス権が要る」はこの形で満たす（ADR 0017）。
//
// GitHub にここで問い合わせて確かめはしない。「どこかに書ける」は「その Project
// に書ける」ではないし、GitHub 側の権限を etoki が判定に使わないという方針とも
// 食い違う。
func (s *BoardService) Create(
	ctx context.Context, name, scene string, target port.BoardTarget,
) (port.Board, error) {
	if name == "" {
		return port.Board{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if !target.Selected() {
		return port.Board{}, fmt.Errorf(
			"%w: repository and project are required", ErrInvalidInput)
	}
	if scene == "" {
		scene = emptyScene
	}
	if err := validateScene(scene); err != nil {
		return port.Board{}, err
	}

	now := s.now()
	b := port.Board{
		ID:        s.newID(),
		Name:      name,
		Scene:     scene,
		Target:    target,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// 作った本人が owner になる。ロールの初期値をここ以外で決めさせない。
	if err := s.boards.Create(ctx, b, actorOf(ctx)); err != nil {
		return port.Board{}, err
	}

	return b, nil
}

// Find は ID でボードを操作者のロールつきで引く。
//
// メンバーでなければ ErrBoardNotFound。
func (s *BoardService) Find(ctx context.Context, id string) (*port.BoardAccess, error) {
	return s.access(ctx, id, port.RoleViewer)
}

// List は操作者がメンバーであるボードを更新時刻の降順で返す。
func (s *BoardService) List(ctx context.Context) ([]port.BoardAccess, error) {
	return s.boards.List(ctx, actorOf(ctx))
}

// SaveScene はボードのシーンを更新し、保存後の版を返す。
//
// base は編集の基準にした更新時刻。いまの版と違えば何も書かずに
// ErrSceneConflict を返す（ADR 0020）。返した時刻が次の保存の基準になる。
func (s *BoardService) SaveScene(
	ctx context.Context, id, scene string, base time.Time,
) (time.Time, error) {
	if err := validateScene(scene); err != nil {
		return time.Time{}, err
	}
	// 未指定を「照合しない」に倒さない。倒すと API を直接叩く経路で照合を
	// 素通りでき、防ぎたい後勝ちがそのまま残る（ADR 0010 と同じ理由）。
	if base.IsZero() {
		return time.Time{}, fmt.Errorf("%w: baseUpdatedAt is required", ErrInvalidInput)
	}
	if _, err := s.access(ctx, id, port.RoleEditor); err != nil {
		return time.Time{}, err
	}

	// 引き当てた Board の UpdatedAt とはここで比べない。比べてから書くまでの
	// 隙間に入った保存を上書きするので、照合は UPDATE と同じ 1 文に任せる。
	//
	// **時計が進まなくても版は必ず進める。** 同じ時刻を書くと、次に来た古い
	// 基準がそのまま一致してしまい、照合を置いた意味が無くなる（ADR 0020）。
	// 書けた保存は base = そのときの版なので、base より後にすれば必ず進む。
	// 時計が巻き戻ったときも同じ理由でここを通る。
	now := s.now()
	if !now.After(base) {
		now = base.Add(time.Nanosecond)
	}

	if err := s.boards.UpdateScene(ctx, actorOf(ctx), id, scene, base, now); err != nil {
		if errors.Is(err, port.ErrConflict) {
			// 「保存に失敗した」ではなく「他の人が保存している」という状態。
			// 呼び出し側が上書きせずに見せられるよう、専用のエラーに写す。
			return time.Time{}, fmt.Errorf("%w: %s", ErrSceneConflict, id)
		}
		return time.Time{}, err
	}

	return now, nil
}

// SetTarget は draft issue の作成先をボードに設定する。
//
// すでに draft issue を作っているボードでは ErrTargetLocked を返す。
func (s *BoardService) SetTarget(ctx context.Context, id string, t port.BoardTarget) error {
	if !t.Selected() {
		return fmt.Errorf("%w: repository and project are required", ErrInvalidInput)
	}

	// 作成中のボードは待つ。作成が終われば run が残るので、待った先で
	// ErrTargetLocked になる。「作成が始まっていたなら変えられない」が
	// 判定の取りこぼしではなく順序として出る。
	release, err := s.locks.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer release()

	// 作成先はそのボードで作られる draft issue の行き先そのものなので、
	// 変えられるのは owner だけ（ADR 0017）。
	if _, err := s.access(ctx, id, port.RoleOwner); err != nil {
		return err
	}

	locked, err := s.TargetLocked(ctx, id)
	if err != nil {
		return err
	}
	if locked {
		// 同じ値なら通してもよさそうだが、通さない。「変更できた」と
		// 「たまたま同じだった」を呼び出し側が区別できなくなる。
		return fmt.Errorf("%w: %s", ErrTargetLocked, id)
	}

	return s.boards.UpdateTarget(ctx, actorOf(ctx), id, t, s.now())
}

// TargetLocked はそのボードの作成先が固定済みかどうかを返す。
//
// 判定は「run が 1 件でもあるか」。フロントは sync_runs を数えられないので、
// 状態としてサーバーが返す必要がある。
//
// **所有者は見ない。呼ぶ前に Find で確かめること。** run は board_id でしか
// 引けないので、ここで二重に絞ると、絞り忘れたときにどちらが効いているのか
// 分からなくなる（ADR 0016）。
func (s *BoardService) TargetLocked(ctx context.Context, id string) (bool, error) {
	runs, err := s.mappings.ListLatestRunsByBoard(ctx, id)
	if err != nil {
		return false, err
	}
	return len(runs) > 0, nil
}

// emptyScene は Excalidraw が読み込める最小のシーン。
const emptyScene = `{"type":"excalidraw","version":2,"source":"etoki","elements":[],"appState":{}}`

// validateScene は保存前にシーンが読めることを確かめる。
//
// 壊れた JSON を保存すると、次に読み込んだときにボードごと開けなくなる。
// 入口で弾いておく。
func validateScene(scene string) error {
	if _, err := domain.ParseScene([]byte(scene)); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	return nil
}
