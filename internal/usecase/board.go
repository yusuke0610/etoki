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

// ownerOf は ctx から所有者を返す。
//
// 認証を設定していなければ空文字になる。空文字は無効値ではなく「認証なしの
// 所有者」1 人を表すので、その構成では全ボードがその 1 人のものになり、
// これまでどおり全部が見える（ADR 0016）。
func ownerOf(ctx context.Context) string {
	owner, _ := port.UserIDFromContext(ctx)
	return owner
}

// ErrTargetLocked は作成先を変えられないことを表す。
//
// そのボードで draft issue を 1 件でも作ったら固定する。sync_runs は GitHub 側に
// 残っている item の追跡表であり、作成先が変わると記録が指す先を見失う（ADR 0014）。
var ErrTargetLocked = errors.New("etoki: board target is locked")

// BoardService はボードの作成・取得・更新を担う。
type BoardService struct {
	boards   port.BoardRepository
	mappings port.MappingRepository
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
func NewBoardService(
	boards port.BoardRepository, mappings port.MappingRepository, opts ...BoardServiceOption,
) *BoardService {
	s := &BoardService{
		boards:   boards,
		mappings: mappings,
		now:      time.Now,
		newID:    uuid.NewString,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create は新しいボードを作る。scene が空なら空のシーンで初期化する。
func (s *BoardService) Create(ctx context.Context, name, scene string) (port.Board, error) {
	if name == "" {
		return port.Board{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if scene == "" {
		scene = emptyScene
	}
	if err := validateScene(scene); err != nil {
		return port.Board{}, err
	}

	now := s.now()
	b := port.Board{
		ID:          s.newID(),
		Name:        name,
		Scene:       scene,
		OwnerUserID: ownerOf(ctx),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.boards.Create(ctx, b); err != nil {
		return port.Board{}, err
	}

	return b, nil
}

// Find は ID でボードを引く。存在しなければ (nil, nil) を返す。
func (s *BoardService) Find(ctx context.Context, id string) (*port.Board, error) {
	return s.boards.Find(ctx, ownerOf(ctx), id)
}

// List は全ボードを更新時刻の降順で返す。
func (s *BoardService) List(ctx context.Context) ([]port.Board, error) {
	return s.boards.List(ctx, ownerOf(ctx))
}

// SaveScene はボードのシーンを更新する。
func (s *BoardService) SaveScene(ctx context.Context, id, scene string) error {
	if err := validateScene(scene); err != nil {
		return err
	}
	return s.boards.UpdateScene(ctx, ownerOf(ctx), id, scene, s.now())
}

// SetTarget は draft issue の作成先をボードに設定する。
//
// すでに draft issue を作っているボードでは ErrTargetLocked を返す。
func (s *BoardService) SetTarget(ctx context.Context, id string, t port.BoardTarget) error {
	if !t.Selected() {
		return fmt.Errorf("%w: repository and project are required", ErrInvalidInput)
	}

	owner := ownerOf(ctx)

	b, err := s.boards.Find(ctx, owner, id)
	if err != nil {
		return err
	}
	if b == nil {
		return fmt.Errorf("board %s: %w", id, port.ErrNotFound)
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

	return s.boards.UpdateTarget(ctx, owner, id, t, s.now())
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
