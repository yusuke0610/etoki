// Package usecase は etoki のユースケース層。
//
// ここから外部に触れるときは必ず port のインターフェースを経由する。
// SQLite / GitHub / LLM の具体的な型をこの層が知ることはない。
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/yusuke0610/etoki/port"
)

// ErrInvalidInput は入力が要件を満たしていないことを表す。
var ErrInvalidInput = errors.New("etoki: invalid input")

// BoardService はボードの作成・取得・更新を担う。
type BoardService struct {
	boardGuard
	mappings port.MappingRepository
	locks    *BoardLocks
	now      func() time.Time
	newID    func() string
	// targetOptional は作成先を選ばずにボードを作れるか（ADR 0090）。
	targetOptional bool
}

// BoardServiceOption は BoardService の依存を差し替える。
type BoardServiceOption func(*BoardService)

// WithClock は時刻の取得方法を差し替える。テストで時刻を固定するために使う。
func WithClock(f func() time.Time) BoardServiceOption {
	return func(s *BoardService) { s.now = f }
}

// WithoutRequiredTarget は、作成先を選ばずにボードを作れるようにする
// （ADR 0090）。**GitHub を組み立てていない構成でだけ渡す。**
//
// その構成では作成先の候補を引けないので、作成先を必須にするとボードを
// 1 枚も作れず、ブレストを始められない。作成先が守っていたもの（draft issue を
// 作る先へのアクセス権、ADR 0017 の要件 1）は、draft issue を作れない構成では
// 守る対象が無い。GitHub を設定したあとは、未選択のボードを開くと作成先の選択に
// 入り（ADR 0014）、候補は書ける Project に絞ってある。
//
// 渡すかどうかを決めるのは配線（`etoki.New`）で、判定はここ 1 か所に置く。
func WithoutRequiredTarget() BoardServiceOption {
	return func(s *BoardService) { s.targetOptional = true }
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
// **例外は GitHub を組み立てていない構成**（`WithoutRequiredTarget`、ADR 0090）。
// そこでも作成先を省けるのは丸ごと空のときだけで、一部だけ送られた作成先は
// 弾く。一部を残すと、選んでいないのに選びかけの値を持つボードができる。
//
// GitHub にここで問い合わせて確かめはしない。「どこかに書ける」は「その Project
// に書ける」ではないし、GitHub 側の権限を etoki が判定に使わないという方針とも
// 食い違う。
func (s *BoardService) Create(
	ctx context.Context, name, scene string, target port.BoardTarget,
) (port.Board, error) {
	name, err := normalizeBoardName(name)
	if err != nil {
		return port.Board{}, err
	}
	if !target.Selected() && (!s.targetOptional || target != (port.BoardTarget{})) {
		return port.Board{}, fmt.Errorf(
			"%w: repository and project are required", ErrInvalidInput)
	}
	if err := validateProjectURL(target.ProjectURL); err != nil {
		return port.Board{}, err
	}
	if scene == "" {
		scene = emptyScene
	}
	if _, err := validateScene(scene); err != nil {
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

// OpenedBoard は開いたボード。シーンに加えて、貼った画像をすべて持つ。
type OpenedBoard struct {
	port.BoardAccess
	// Files は貼った画像。シーンと同じ時点のもの（ADR 0074）。
	Files []port.BoardFile
	// OverLimit は保存済みのボードが上限を超えていて、このままでは保存し直せ
	// ないこと（ADR 0048 / 0074）。
	OverLimit bool
}

// Open はボードを画像ごと返す。ボードを開くときだけに使う（ADR 0074）。
//
// **画像を返すのはここだけ。** 改名や作成先の設定の応答は Find で引き直すので
// 画像を運ばない。開いたあとのキャンバスはすでに画像を持っている。
func (s *BoardService) Open(ctx context.Context, id string) (OpenedBoard, error) {
	a, files, err := s.accessWithFiles(ctx, id, port.RoleViewer)
	if err != nil {
		return OpenedBoard{}, err
	}

	var total int64
	for _, f := range files {
		total += int64(len(f.Data))
	}

	return OpenedBoard{
		BoardAccess: *a,
		Files:       files,
		// 引き当てた画像そのもので数える。大きさを別に読み直すと、間に
		// 入った保存のぶんだけシーンと食い違う。
		OverLimit: boardExceedsLimit(a.Board.Scene, total),
	}, nil
}

// OverLimit は引き当て済みのボードが上限を超えていて、このままでは保存し直せ
// ないことを返す（ADR 0048 / 0074）。
//
// 画像の中身は読まず、大きさだけを引く。**呼ぶ前に access で確かめること。**
// 改名や作成先の設定の応答にも同じ値を載せるため、Open とは別に置く。
func (s *BoardService) OverLimit(ctx context.Context, a port.BoardAccess) (bool, error) {
	sizes, err := s.boards.FileSizes(ctx, a.Board.ID)
	if err != nil {
		return false, err
	}

	var total int64
	for _, n := range sizes {
		total += n
	}
	return boardExceedsLimit(a.Board.Scene, total), nil
}

// Rename はボードの名前を変える。
//
// **変えられるのは editor 以上。** 作成先の変更（owner だけ、ADR 0017）と
// 揃えないのは、名前が取り消せない作成の行き先を決めるものではないため。
// シーンを書き換えられる相手に、表示名だけ直させない理由が無い。
//
// **更新時刻は進めない**（port.BoardRepository.UpdateName）。あれはシーンの版
// なので、名前を直しただけで進めると、開いている別のメンバーの次の保存が
// 理由なく 409 になる。
func (s *BoardService) Rename(ctx context.Context, id, name string) error {
	name, err := normalizeBoardName(name)
	if err != nil {
		return err
	}

	if _, err := s.access(ctx, id, port.RoleEditor); err != nil {
		return err
	}

	return s.boards.UpdateName(ctx, actorOf(ctx), id, name)
}

// emptyScene は Excalidraw が読み込める最小のシーン。
const emptyScene = `{"type":"excalidraw","version":2,"source":"etoki","elements":[],"appState":{}}`

// normalizeBoardName はボードの名前の前後の空白を落とし、落としたうえで空なら
// 弾く。空白だけの名前を通すと、一覧に見出しの無い行が並び、開くまでどの
// ボードか分からなくなる。
//
// **作成と改名の両方がこれを通す**（#276）。片方だけだと、空白だけの名前の
// ボードがもう片方の口からだけ作れる。
func normalizeBoardName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	return name, nil
}
