package sqlite_test

import (
	"errors"
	"testing"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// 作った時点の背景は空。Create は Premise を書かない（作成の入口は背景を
// 受け取らない）。渡されても書かないことを固定しておく。
func TestBoard_CreateLeavesPremiseEmpty(t *testing.T) {
	t.Parallel()

	repo := sqlite.NewBoardRepository(newDB(t))
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "ボード", Scene: `{"elements":[]}`,
		Premise:   "作成では書かれないはずの背景",
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Premise != "" {
		t.Errorf("Premise = %q, want 空（Create は書かない）", got.Board.Premise)
	}
}

// 背景（ADR 0093）は背景だけを書き、シーンの版を進めない。
func TestBoard_UpdatePremise(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	if err := repo.UpdatePremise(t.Context(), "", "board-1", "", "Go と React で作っている"); err != nil {
		t.Fatalf("UpdatePremise: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Premise != "Go と React で作っている" {
		t.Errorf("Premise = %q, want Go と React で作っている", got.Board.Premise)
	}
	// **背景で版を進めてはいけない**（ADR 0092）。updated_at はシーンの版で、
	// 保存の照合基準。SET に updated_at を足すと、そのボードを開いている別の
	// メンバーの次の保存が、誰もシーンを触っていないのに 409 で断られる。
	if !got.Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（背景で進めてはいけない）",
			got.Board.UpdatedAt, baseTime)
	}
	// 背景だけを書く。SET にシーンや名前が混ざると、背景を直しただけで
	// ブレストや名前が消える。
	if got.Board.Scene != `{"elements":[]}` {
		t.Errorf("Scene = %q（背景で書き換えてはいけない）", got.Board.Scene)
	}
	if got.Board.Name != "テストボード" {
		t.Errorf("Name = %q（背景で書き換えてはいけない）", got.Board.Name)
	}
}

// 編集を始めた時点の背景と食い違えば書かない（ADR 0093）。先に保存した人の
// 背景を、古い本文から編集していた人が黙って上書きしないため。
func TestBoard_UpdatePremiseRejectsStaleBase(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	// 先に保存した側。空から書いた。
	if err := repo.UpdatePremise(t.Context(), "", "board-1", "", "先に書いた背景"); err != nil {
		t.Fatalf("UpdatePremise（先に保存した側）: %v", err)
	}

	// 後から保存する側は、開いたときの空の本文を基準に送ってくる。
	err := repo.UpdatePremise(t.Context(), "", "board-1", "", "後から書いた背景")
	if !errors.Is(err, port.ErrConflict) {
		t.Fatalf("UpdatePremise = %v, want port.ErrConflict", err)
	}
	// 「無い」と混ぜない。無いものは待っても現れないが、食い違いは相手の
	// 背景を読み直せば進める。
	if errors.Is(err, port.ErrNotFound) {
		t.Error("背景の食い違いを ErrNotFound で返している")
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Premise != "先に書いた背景" {
		t.Errorf("Premise = %q, want 先に保存した側のまま", got.Board.Premise)
	}

	// いまの本文を基準にすれば通る。照合が「一度書いたら書けない」に
	// なっていないことを見る。
	if err := repo.UpdatePremise(t.Context(), "", "board-1", "先に書いた背景", "直した背景"); err != nil {
		t.Fatalf("UpdatePremise（いまの本文を基準に）: %v", err)
	}
	got, err = repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Premise != "直した背景" {
		t.Errorf("Premise = %q, want 直した背景", got.Board.Premise)
	}
}

// メンバーでないボードの背景は書けない。絞りを外すと、ID を知っているだけで
// 他人のボードの背景を書き換えられ、その人の解釈に効く（ADR 0016）。
func TestBoard_UpdatePremiseRequiresMembership(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	err := repo.UpdatePremise(t.Context(), "someone-else", "board-1", "", "乗っ取り")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdatePremise = %v, want port.ErrNotFound", err)
	}

	got, findErr := repo.Find(t.Context(), "", "board-1")
	if findErr != nil {
		t.Fatalf("Find: %v", findErr)
	}
	if got.Board.Premise != "" {
		t.Errorf("Premise = %q（書き換わってはいけない）", got.Board.Premise)
	}
}

func TestBoard_UpdatePremiseNotFound(t *testing.T) {
	t.Parallel()

	repo := sqlite.NewBoardRepository(newDB(t))

	err := repo.UpdatePremise(t.Context(), "", "no-such-board", "", "背景")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdatePremise = %v, want port.ErrNotFound", err)
	}
}

// 一覧は背景を読まない（シーンと同じ）。開く口（Find / FindWithFiles）は読む。
func TestBoard_ListOmitsPremiseAndOpenCarriesIt(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")
	if err := repo.UpdatePremise(t.Context(), "", "board-1", "", "決済は Stripe を使う"); err != nil {
		t.Fatalf("UpdatePremise: %v", err)
	}

	listed, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("件数 = %d, want 1", len(listed))
	}
	if listed[0].Board.Premise != "" {
		t.Errorf("List が Premise を返している: %q", listed[0].Board.Premise)
	}

	found, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.Board.Premise != "決済は Stripe を使う" {
		t.Errorf("Find の Premise = %q", found.Board.Premise)
	}

	opened, _, err := repo.FindWithFiles(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("FindWithFiles: %v", err)
	}
	if opened.Board.Premise != "決済は Stripe を使う" {
		t.Errorf("FindWithFiles の Premise = %q", opened.Board.Premise)
	}
}
