package sqlite_test

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// ---------------------------------------------------------------------------
// 貼った画像の読み書き（ADR 0074、#102）
// ---------------------------------------------------------------------------

// file は中身で見分けられる画像を 1 枚作る。
func file(id, content string) port.BoardFile {
	return port.BoardFile{ID: id, Data: `{"id":"` + id + `","dataURL":"` + content + `"}`}
}

// filesOf はボードの画像を ID → 中身で返す。開くときと同じ口で読む。
func filesOf(t *testing.T, repo *sqlite.BoardRepository, actor, id string) map[string]string {
	t.Helper()

	a, files, err := repo.FindWithFiles(t.Context(), actor, id)
	if err != nil {
		t.Fatalf("FindWithFiles: %v", err)
	}
	if a == nil {
		t.Fatalf("FindWithFiles(%s) = nil", id)
	}

	out := map[string]string{}
	for _, f := range files {
		out[f.ID] = f.Data
	}
	return out
}

// save は版を進めながらシーンを保存する。返すのは次の基準と持っている画像。
func save(
	t *testing.T, repo *sqlite.BoardRepository, base time.Time, w port.SceneWrite,
) (time.Time, []string) {
	t.Helper()

	next := base.Add(time.Minute)
	held, err := repo.UpdateScene(t.Context(), "", "board-1", w, base, next)
	if err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}
	return next, held
}

// 足した画像が残り、書いたあとに持っている画像の ID が返る。
func TestBoard_UpdateSceneStoresAddedFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	f1, f2 := file("file-1", "AAAA"), file("file-2", "BBBB")
	_, held := save(t, repo, baseTime, port.SceneWrite{
		Scene:      `{"elements":[]}`,
		Added:      []port.BoardFile{f2, f1},
		Referenced: []string{"file-1", "file-2"},
	})

	if want := []string{"file-1", "file-2"}; !slices.Equal(held, want) {
		t.Errorf("held = %v, want %v", held, want)
	}
	want := map[string]string{"file-1": f1.Data, "file-2": f2.Data}
	if got := filesOf(t, repo, "", "board-1"); !maps.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

// 送り直さなくても、参照しているかぎり画像は残る。
//
// **これが分けた意味そのもの。** 2 回目以降の保存は画像を送らない。送らなかった
// 画像を消す実装だと、図形を動かして保存しただけで画像が消える。
func TestBoard_UpdateSceneKeepsFilesNotResent(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	f1 := file("file-1", "AAAA")
	next, _ := save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{f1}, Referenced: []string{"file-1"},
	})
	_, held := save(t, repo, next, port.SceneWrite{
		Scene: `{"elements":["moved"]}`, Referenced: []string{"file-1"},
	})

	if want := []string{"file-1"}; !slices.Equal(held, want) {
		t.Errorf("held = %v, want %v", held, want)
	}
	if got := filesOf(t, repo, "", "board-1"); got["file-1"] != f1.Data {
		t.Errorf("送り直さなかった画像が残っていない: %v", got)
	}
}

// シーンから外れた画像は消える。残したほうは消さない。
func TestBoard_UpdateSceneDeletesUnreferencedFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	f1, f2 := file("file-1", "AAAA"), file("file-2", "BBBB")
	next, _ := save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{f1, f2},
		Referenced: []string{"file-1", "file-2"},
	})
	_, held := save(t, repo, next, port.SceneWrite{
		Scene: `{"elements":[]}`, Referenced: []string{"file-2"},
	})

	if want := []string{"file-2"}; !slices.Equal(held, want) {
		t.Errorf("held = %v, want %v", held, want)
	}
	want := map[string]string{"file-2": f2.Data}
	if got := filesOf(t, repo, "", "board-1"); !maps.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

// 参照がひとつも無ければ全部消える。nil を JSON の null にすると NOT IN が
// 空集合と比べる形にならず、1 枚も消えない。
func TestBoard_UpdateSceneWithNoReferencesDeletesAllFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	next, _ := save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{file("file-1", "AAAA")},
		Referenced: []string{"file-1"},
	})
	_, held := save(t, repo, next, port.SceneWrite{Scene: `{"elements":[]}`})

	if len(held) != 0 {
		t.Errorf("held = %v, want 空", held)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM board_files`); n != 0 {
		t.Errorf("board_files = %d 件, want 0", n)
	}
}

// 同じ ID を送り直したら置き換える。行は増えない。
func TestBoard_UpdateSceneReplacesFileWithSameID(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	next, _ := save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{file("file-1", "OLD")},
		Referenced: []string{"file-1"},
	})
	replaced := file("file-1", "NEW")
	save(t, repo, next, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{replaced},
		Referenced: []string{"file-1"},
	})

	want := map[string]string{"file-1": replaced.Data}
	if got := filesOf(t, repo, "", "board-1"); !maps.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

// 照合に負けた保存は、画像にも触れない。
//
// **シーンと画像を別々に書くと、ここが崩れる。** 負けた側が足した画像は
// どのシーンからも指されずに残り、負けた側から見て参照の外れた画像（勝った側の
// シーンが指している画像）は消える（ADR 0074）。
func TestBoard_UpdateSceneConflictWritesNoFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	winner := file("file-1", "WIN")
	save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":["winner"]}`, Added: []port.BoardFile{winner},
		Referenced: []string{"file-1"},
	})

	// 開いたときの版のまま、別の画像だけを指すシーンを送ってくる。
	_, err := repo.UpdateScene(t.Context(), "", "board-1", port.SceneWrite{
		Scene: `{"elements":["loser"]}`, Added: []port.BoardFile{file("file-2", "LOSE")},
		Referenced: []string{"file-2"},
	}, baseTime, baseTime.Add(time.Hour))
	if !errors.Is(err, port.ErrConflict) {
		t.Fatalf("UpdateScene = %v, want port.ErrConflict", err)
	}

	want := map[string]string{"file-1": winner.Data}
	if got := filesOf(t, repo, "", "board-1"); !maps.Equal(got, want) {
		t.Errorf("files = %v, want 先に保存した側のまま %v", got, want)
	}
}

// 非メンバーの保存は、画像にも触れない。
func TestBoard_UpdateSceneByNonMemberWritesNoFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	_, err := repo.UpdateScene(t.Context(), "user-b", "board-a", port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{file("file-1", "AAAA")},
		Referenced: []string{"file-1"},
	}, baseTime, baseTime.Add(time.Hour))
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateScene = %v, want port.ErrNotFound", err)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM board_files`); n != 0 {
		t.Errorf("board_files = %d 件, want 0（非メンバーが画像を書けている）", n)
	}
}

// 画像はボードごと。別のボードの画像は混ざらず、別のボードの保存で消えない。
func TestBoard_FilesAreScopedToBoard(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")

	other := file("file-x", "OTHER")
	if _, err := repo.UpdateScene(t.Context(), "", "board-2", port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{other}, Referenced: []string{"file-x"},
	}, baseTime, baseTime.Add(time.Minute)); err != nil {
		t.Fatalf("UpdateScene(board-2): %v", err)
	}

	// board-1 の保存は参照を持たない。WHERE の board_id を書き損なうと、
	// board-2 の画像まで消える。
	_, held := save(t, repo, baseTime, port.SceneWrite{Scene: `{"elements":[]}`})
	if len(held) != 0 {
		t.Errorf("board-1 の held = %v, want 空", held)
	}

	if got := filesOf(t, repo, "", "board-1"); len(got) != 0 {
		t.Errorf("board-1 に別のボードの画像が混ざった: %v", got)
	}
	want := map[string]string{"file-x": other.Data}
	if got := filesOf(t, repo, "", "board-2"); !maps.Equal(got, want) {
		t.Errorf("board-2 の画像 = %v, want %v", got, want)
	}

	sizes, err := repo.FileSizes(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("FileSizes: %v", err)
	}
	if len(sizes) != 0 {
		t.Errorf("FileSizes(board-1) = %v, want 空", sizes)
	}
}

// 大きさは data のバイト数。中身は読まずに返す。
func TestBoard_FileSizes(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	// マルチバイトを混ぜる。文字数で数えると上限の判定がずれる。
	f1, f2 := file("file-1", "AAAA"), file("file-2", "画像")
	save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{f1, f2},
		Referenced: []string{"file-1", "file-2"},
	})

	got, err := repo.FileSizes(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("FileSizes: %v", err)
	}
	want := map[string]int64{"file-1": int64(len(f1.Data)), "file-2": int64(len(f2.Data))}
	if !maps.Equal(got, want) {
		t.Errorf("FileSizes = %v, want %v", got, want)
	}
}

// 非メンバーには画像ごと「無い」。Find と同じく、無いと区別しない。
func TestBoard_FindWithFilesRequiresMembership(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedOwnedBoard(t, db, "board-a", "user-a")

	a, files, err := repo.FindWithFiles(t.Context(), "user-b", "board-a")
	if err != nil {
		t.Fatalf("FindWithFiles: %v", err)
	}
	if a != nil || files != nil {
		t.Errorf("FindWithFiles(非メンバー) = (%+v, %v), want (nil, nil)", a, files)
	}
}

// 画像の無いボードは空の一覧で返す。nil だと JSON で null になり、契約の
// 「ID → 画像」が読めない。
func TestBoard_FindWithFilesWithoutFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	a, files, err := repo.FindWithFiles(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("FindWithFiles: %v", err)
	}
	if a == nil || a.Board.Scene != `{"elements":[]}` {
		t.Fatalf("FindWithFiles = %+v, want シーンつきのボード", a)
	}
	if files == nil || len(files) != 0 {
		t.Errorf("files = %#v, want 空の一覧", files)
	}
}

// ボードを消したら画像も消える。消える範囲はスキーマの ON DELETE CASCADE が
// 決める（Delete の doc コメント）。
func TestBoard_DeleteRemovesFiles(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	save(t, repo, baseTime, port.SceneWrite{
		Scene: `{"elements":[]}`, Added: []port.BoardFile{file("file-1", "AAAA")},
		Referenced: []string{"file-1"},
	})
	if err := repo.Delete(t.Context(), "", "board-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := countRows(t, db, `SELECT COUNT(*) FROM board_files`); n != 0 {
		t.Errorf("board_files = %d 件, want 0", n)
	}
}
