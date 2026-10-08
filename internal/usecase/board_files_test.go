package usecase_test

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// ---------------------------------------------------------------------------
// 貼った画像の保存と読み出し（ADR 0074、#102）
// ---------------------------------------------------------------------------

// imageScene は fileIDs の画像を 1 枚ずつ貼ったシーン。
func imageScene(fileIDs ...string) string {
	elements := make([]string, 0, len(fileIDs))
	for i, id := range fileIDs {
		elements = append(elements,
			`{"id":"img-`+string(rune('a'+i))+`","type":"image","fileId":"`+id+`"}`)
	}
	return `{"type":"excalidraw","version":2,"elements":[` +
		strings.Join(elements, ",") + `],"appState":{},"files":{}}`
}

// imageData は id を名乗る Excalidraw の画像データ。中身で見分けられるように
// content を dataURL に入れる。
func imageData(id, content string) string {
	return `{"id":"` + id + `","mimeType":"image/png","dataURL":"` + content + `"}`
}

// imageDataOfSize は size バイトちょうどの画像データを作る。
func imageDataOfSize(t *testing.T, id string, size int) string {
	t.Helper()

	shell := imageData(id, "")
	if size < len(shell) {
		t.Fatalf("size = %d だが、包みだけで %d バイトある", size, len(shell))
	}
	data := imageData(id, strings.Repeat("A", size-len(shell)))
	if len(data) != size {
		t.Fatalf("len(data) = %d, want %d", len(data), size)
	}
	return data
}

// saveFiles は版を合わせて保存し、結果を返す。
func saveFiles(
	t *testing.T, svc *usecase.BoardService, boards *fakeBoards,
	scene string, files map[string]string,
) usecase.SavedScene {
	t.Helper()

	saved, err := svc.SaveScene(t.Context(), "board-1", scene, files, boards.board.UpdatedAt)
	if err != nil {
		t.Fatalf("SaveScene() = %v", err)
	}
	return saved
}

// 送った画像が残り、保存後に持っている画像の ID が返る。
func TestSaveScene_StoresSentFiles(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	data := imageData("file-1", "AAAA")
	saved := saveFiles(t, svc, boards, imageScene("file-1"), map[string]string{"file-1": data})

	if want := []string{"file-1"}; !slices.Equal(saved.FileIDs, want) {
		t.Errorf("FileIDs = %v, want %v", saved.FileIDs, want)
	}
	if want := map[string]string{"file-1": data}; !maps.Equal(boards.files, want) {
		t.Errorf("files = %v, want %v", boards.files, want)
	}
}

// 2 回目の保存で送らなかった画像も、参照しているかぎり残る。
//
// **これが分けた意味そのもの。** 図形を動かしただけの保存では画像を送らない。
func TestSaveScene_KeepsFilesNotResent(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	data := imageData("file-1", "AAAA")
	saveFiles(t, svc, boards, imageScene("file-1"), map[string]string{"file-1": data})
	saved := saveFiles(t, svc, boards, imageScene("file-1"), nil)

	if want := []string{"file-1"}; !slices.Equal(saved.FileIDs, want) {
		t.Errorf("FileIDs = %v, want %v", saved.FileIDs, want)
	}
	if boards.files["file-1"] != data {
		t.Errorf("送り直さなかった画像が残っていない: %v", boards.files)
	}
}

// 参照はシーンから読む。削除済みの要素だけが指す画像は参照に入れない。
//
// 参照に入れたままだと、消したはずの画像が上限を埋め続ける。
func TestSaveScene_DeletedElementReleasesItsFile(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	saveFiles(t, svc, boards, imageScene("file-1", "file-2"), map[string]string{
		"file-1": imageData("file-1", "AAAA"),
		"file-2": imageData("file-2", "BBBB"),
	})

	deleted := `{"type":"excalidraw","elements":[
		{"id":"img-a","type":"image","fileId":"file-1","isDeleted":true},
		{"id":"img-b","type":"image","fileId":"file-2"}]}`
	saved := saveFiles(t, svc, boards, deleted, nil)

	if want := []string{"file-2"}; !slices.Equal(boards.saved.Referenced, want) {
		t.Errorf("Referenced = %v, want %v", boards.saved.Referenced, want)
	}
	if want := []string{"file-2"}; !slices.Equal(saved.FileIDs, want) {
		t.Errorf("FileIDs = %v, want %v", saved.FileIDs, want)
	}
}

// 照合に負けた保存は、送った画像を残さない（ADR 0020 / 0074）。
func TestSaveScene_ConflictKeepsFiles(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	kept := imageData("file-1", "AAAA")
	saveFiles(t, svc, boards, imageScene("file-1"), map[string]string{"file-1": kept})
	writes := boards.writes

	stale := boards.board.UpdatedAt.Add(-1)
	_, err := svc.SaveScene(t.Context(), "board-1", imageScene("file-2"),
		map[string]string{"file-2": imageData("file-2", "BBBB")}, stale)
	if !errors.Is(err, usecase.ErrSceneConflict) {
		t.Fatalf("SaveScene() = %v, want ErrSceneConflict", err)
	}
	if boards.writes != writes {
		t.Errorf("照合に負けたのに書いている: writes = %d, want %d", boards.writes, writes)
	}
	if want := map[string]string{"file-1": kept}; !maps.Equal(boards.files, want) {
		t.Errorf("files = %v, want %v", boards.files, want)
	}
}

// 画像を受け取れない形は、書く前に 400 で弾く。
//
//   - シーンに画像の実体が入っている … 画像の入口を 1 つにする。受け付けると、
//     シーンに乗った画像は上限も「参照されなくなったら消す」約束も素通りする。
//   - 送った画像をシーンが参照していない … 受け取って捨てると、送った画像が
//     残っていないことに呼び出し側が気づけない。
//   - 画像データが自分の ID を名乗らない … Excalidraw は画像を id で引くので、
//     開き直したときに画像の要素が指す先が見つからない。
func TestSaveScene_RejectsMalformedFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		scene string
		files map[string]string
	}{
		{
			"シーンに画像の実体がある",
			`{"type":"excalidraw","elements":[{"id":"i","type":"image","fileId":"file-1"}],` +
				`"files":{"file-1":` + imageData("file-1", "AAAA") + `}}`,
			nil,
		},
		{
			"参照されていない画像を送った",
			imageScene("file-1"),
			map[string]string{
				"file-1": imageData("file-1", "AAAA"),
				"file-9": imageData("file-9", "ZZZZ"),
			},
		},
		{
			"削除済みの要素だけが指す画像を送った",
			`{"type":"excalidraw","elements":[
				{"id":"i","type":"image","fileId":"file-1","isDeleted":true}]}`,
			map[string]string{"file-1": imageData("file-1", "AAAA")},
		},
		{
			"画像データの id がキーと違う",
			imageScene("file-1"),
			map[string]string{"file-1": imageData("file-2", "AAAA")},
		},
		{
			"画像データに id が無い",
			imageScene("file-1"),
			map[string]string{"file-1": `{"dataURL":"AAAA"}`},
		},
		{
			"画像データが JSON のオブジェクトでない",
			imageScene("file-1"),
			map[string]string{"file-1": `"AAAA"`},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(emptyScene)}
			_, err := newBoardService(boards).SaveScene(
				t.Context(), "board-1", c.scene, c.files, boards.board.UpdatedAt)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("SaveScene() = %v, want ErrInvalidInput", err)
			}
			if boards.writes != 0 {
				t.Errorf("弾いたのに書いている: writes = %d", boards.writes)
			}
		})
	}
}

// 作成も画像を受け取らない。作成の口に画像の入口を作ると、保存と違う経路で
// 画像がシーンに乗る。
func TestCreate_RejectsSceneCarryingFiles(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{}
	scene := `{"type":"excalidraw","elements":[],"files":{"file-1":` +
		imageData("file-1", "AAAA") + `}}`

	_, err := newBoardService(boards).Create(t.Context(), "b", scene, newTarget())
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Create() = %v, want ErrInvalidInput", err)
	}
	if boards.writes != 0 {
		t.Errorf("弾いたのに作っている: writes = %d", boards.writes)
	}
}

// 空の files は画像を抱えていない。Excalidraw は画像が無くても `files: {}` を
// 書くので、弾くと画像の無いボードが保存できない。
func TestSaveScene_AcceptsEmptyFilesInScene(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	saveFiles(t, newBoardService(boards), boards,
		`{"type":"excalidraw","elements":[],"files":{}}`, nil)
}

// 参照先の画像がどこにも無くても保存できる。
//
// いまも実体の欠けた画像の要素はそのまま保存されている（Excalidraw の直列化が
// 実体の無い画像を落とす）。弾くと、実体の欠けた `.excalidraw` を取り込んだ
// ボードが保存できなくなる。
func TestSaveScene_AllowsMissingFile(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	saved := saveFiles(t, newBoardService(boards), boards, imageScene("file-missing"), nil)

	if len(saved.FileIDs) != 0 {
		t.Errorf("FileIDs = %v, want 空", saved.FileIDs)
	}
}

// 画像の合計は上限ちょうどまで通す。持っている画像と今回送る画像の両方を
// 数える。
//
// 持っている画像を 1 枚、送る画像を 1 枚にして、合わせてちょうど上限にする。
// どちらかを数え損ねる実装は、次のテストの「1 バイト超え」を通してしまう。
func TestSaveScene_AcceptsFilesAtTheLimit(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	half := usecase.MaxBoardFileBytes / 2
	saveFiles(t, svc, boards, imageScene("file-1"),
		map[string]string{"file-1": imageDataOfSize(t, "file-1", half)})

	saved := saveFiles(t, svc, boards, imageScene("file-1", "file-2"),
		map[string]string{"file-2": imageDataOfSize(t, "file-2", usecase.MaxBoardFileBytes-half)})

	if want := []string{"file-1", "file-2"}; !slices.Equal(saved.FileIDs, want) {
		t.Errorf("FileIDs = %v, want %v", saved.FileIDs, want)
	}
}

// 1 バイト超えたら 413 で弾き、何も書かない。**縮小も切り捨てもしない**
// （ADR 0038 / 0074）。
func TestSaveScene_RejectsFilesOverTheLimit(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	half := usecase.MaxBoardFileBytes / 2
	kept := imageDataOfSize(t, "file-1", half)
	saveFiles(t, svc, boards, imageScene("file-1"), map[string]string{"file-1": kept})
	writes := boards.writes

	_, err := svc.SaveScene(t.Context(), "board-1", imageScene("file-1", "file-2"),
		map[string]string{"file-2": imageDataOfSize(t, "file-2", usecase.MaxBoardFileBytes-half+1)},
		boards.board.UpdatedAt)
	if !errors.Is(err, usecase.ErrSceneTooLarge) {
		t.Fatalf("SaveScene() = %v, want ErrSceneTooLarge", err)
	}
	// 入力の誤りに畳まない。打ち手は「送った内容を直す」ではなく
	// 「貼った画像を減らす」。
	if errors.Is(err, usecase.ErrInvalidInput) {
		t.Error("大きさの拒否を入力の誤りに畳んでいる")
	}
	if boards.writes != writes {
		t.Errorf("上限を超えたのに書いている: writes = %d, want %d", boards.writes, writes)
	}
	if want := map[string]string{"file-1": kept}; !maps.Equal(boards.files, want) {
		t.Error("上限を超えた保存で画像が変わった")
	}
}

// 合計に数えるのは保存したあとに残る画像だけ。外した画像は数えない。
//
// 数えると、上限近くまで貼ったボードで「1 枚消して 1 枚貼る」が通らない。
// 消すつもりの画像のせいで、消す保存そのものができなくなる。
func TestSaveScene_CountsOnlyFilesKeptAfterSave(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	saveFiles(t, svc, boards, imageScene("file-1"),
		map[string]string{"file-1": imageDataOfSize(t, "file-1", usecase.MaxBoardFileBytes)})

	saved := saveFiles(t, svc, boards, imageScene("file-2"),
		map[string]string{"file-2": imageData("file-2", "BBBB")})

	if want := []string{"file-2"}; !slices.Equal(saved.FileIDs, want) {
		t.Errorf("FileIDs = %v, want %v", saved.FileIDs, want)
	}
}

// 同じ ID を送り直したら、合計には新しいほうだけを数える。
func TestSaveScene_CountsReplacedFileOnce(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	saveFiles(t, svc, boards, imageScene("file-1"),
		map[string]string{"file-1": imageDataOfSize(t, "file-1", usecase.MaxBoardFileBytes)})

	replaced := imageDataOfSize(t, "file-1", usecase.MaxBoardFileBytes)
	saveFiles(t, svc, boards, imageScene("file-1"), map[string]string{"file-1": replaced})

	if boards.files["file-1"] != replaced {
		t.Error("同じ ID の画像が置き換わっていない")
	}
}

// 開くと、シーンと一緒に画像を返す。
func TestOpen_ReturnsFiles(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(emptyScene)}
	svc := newBoardService(boards)

	data := imageData("file-1", "AAAA")
	saveFiles(t, svc, boards, imageScene("file-1"), map[string]string{"file-1": data})

	opened, err := svc.Open(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if opened.Board.Scene != imageScene("file-1") {
		t.Errorf("Scene = %s", opened.Board.Scene)
	}
	want := []port.BoardFile{{ID: "file-1", Data: data}}
	if !slices.Equal(opened.Files, want) {
		t.Errorf("Files = %v, want %v", opened.Files, want)
	}
	if opened.OverLimit {
		t.Error("上限に収まっているのに OverLimit")
	}
}

// 上限の判定は画像の合計も見る（ADR 0048 / 0074）。開いた口と、改名などの
// 応答に載せる口の両方で同じ答えになる。
//
// 上限を超えた画像は保存では入らないので、持っているものを直に置いて作る
// （上限を引き下げたあとのボードに当たる）。
func TestOverLimit_CountsFiles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		bytes int
		want  bool
	}{
		{"ちょうど上限", usecase.MaxBoardFileBytes, false},
		{"上限を 1 バイト超える", usecase.MaxBoardFileBytes + 1, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{
				board: newBoard(imageScene("file-1")),
				files: map[string]string{"file-1": imageDataOfSize(t, "file-1", c.bytes)},
			}
			svc := newBoardService(boards)

			opened, err := svc.Open(t.Context(), "board-1")
			if err != nil {
				t.Fatalf("Open() = %v", err)
			}
			if opened.OverLimit != c.want {
				t.Errorf("Open().OverLimit = %v, want %v", opened.OverLimit, c.want)
			}

			got, err := svc.OverLimit(t.Context(), opened.BoardAccess)
			if err != nil {
				t.Fatalf("OverLimit() = %v", err)
			}
			if got != c.want {
				t.Errorf("OverLimit() = %v, want %v", got, c.want)
			}
		})
	}
}

// シーンが上限を超えていれば、画像が無くても上限超え（issue #103 のまま）。
func TestOverLimit_CountsScene(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(sceneOfSize(t, usecase.MaxSceneBytes+1))}

	opened, err := newBoardService(boards).Open(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("Open() = %v", err)
	}
	if !opened.OverLimit {
		t.Error("シーンが上限を超えているのに OverLimit が false")
	}
}
