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

// BoardService はボードの作成・取得・更新を担う。
type BoardService struct {
	boards port.BoardRepository
	now    func() time.Time
	newID  func() string
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
func NewBoardService(boards port.BoardRepository, opts ...BoardServiceOption) *BoardService {
	s := &BoardService{
		boards: boards,
		now:    time.Now,
		newID:  uuid.NewString,
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
		ID:        s.newID(),
		Name:      name,
		Scene:     scene,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.boards.Create(ctx, b); err != nil {
		return port.Board{}, err
	}

	return b, nil
}

// Find は ID でボードを引く。存在しなければ (nil, nil) を返す。
func (s *BoardService) Find(ctx context.Context, id string) (*port.Board, error) {
	return s.boards.Find(ctx, id)
}

// List は全ボードを更新時刻の降順で返す。
func (s *BoardService) List(ctx context.Context) ([]port.Board, error) {
	return s.boards.List(ctx)
}

// SaveScene はボードのシーンを更新する。
func (s *BoardService) SaveScene(ctx context.Context, id, scene string) error {
	if err := validateScene(scene); err != nil {
		return err
	}
	return s.boards.UpdateScene(ctx, id, scene, s.now())
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
