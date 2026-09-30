package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 一覧に載せる注釈の件数（#200）。**判定は注釈の状態の一覧（ListStates）と
// 同じ**で、保存済みシーンと最新の run から決める。

// 3 状態が混ざるシーン。**件数は状態ごとに変えてある**（未作成 1・作成済み 2・
// 変更あり 3）。同じ件数にすると、状態を取り違えて数えても同じ値になる。
const threeStatesScene = `{"type":"excalidraw","elements":[
	{"id":"a-new","type":"frame","name":"まだ作っていない","customData":{"etoki":{"granularity":""}}},
	{"id":"t1","type":"text","text":"ログイン","frameId":"a-new"},
	{"id":"a-done1","type":"frame","name":"作ったまま 1","customData":{"etoki":{"granularity":""}}},
	{"id":"t2","type":"text","text":"パスワード再設定","frameId":"a-done1"},
	{"id":"a-done2","type":"frame","name":"作ったまま 2","customData":{"etoki":{"granularity":""}}},
	{"id":"t3","type":"text","text":"ログアウト","frameId":"a-done2"},
	{"id":"a-edit1","type":"frame","name":"書き換えた 1","customData":{"etoki":{"granularity":""}}},
	{"id":"t4","type":"text","text":"セッション管理","frameId":"a-edit1"},
	{"id":"a-edit2","type":"frame","name":"書き換えた 2","customData":{"etoki":{"granularity":""}}},
	{"id":"t5","type":"text","text":"二要素認証","frameId":"a-edit2"},
	{"id":"a-edit3","type":"frame","name":"書き換えた 3","customData":{"etoki":{"granularity":""}}},
	{"id":"t6","type":"text","text":"監査ログ","frameId":"a-edit3"}]}`

// fakeBoardShelf は複数のボードを持つリポジトリ。一覧の件数だけに使う。
//
// **List はシーンを空で返す。** 実装と同じにしておかないと（`port` の
// `BoardRepository.List`）、一覧の返り値に入ったシーンをそのまま数える実装が
// 素通りする。そちらは全ボードのシーンを一度にメモリへ載せる形で、避けたい
// ものそのもの。
type fakeBoardShelf struct {
	fakeBoards
	// listed は List が返す並び。stored に無いボードも置ける（一覧を引いてから
	// 数えるまでのあいだに消えたボード）。
	listed []port.Board
	// stored は Find が引けるボード。
	stored map[string]port.Board
}

func (f *fakeBoardShelf) List(_ context.Context, actor string) ([]port.BoardAccess, error) {
	if actor != f.owner {
		return nil, nil
	}
	out := make([]port.BoardAccess, 0, len(f.listed))
	for _, b := range f.listed {
		b.Scene = ""
		out = append(out, port.BoardAccess{Board: b, Role: port.RoleOwner})
	}
	return out, nil
}

// Find は実装と同じく操作者も突き合わせる。
func (f *fakeBoardShelf) Find(_ context.Context, actor, id string) (*port.BoardAccess, error) {
	b, ok := f.stored[id]
	if !ok || actor != f.owner {
		return nil, nil
	}
	return &port.BoardAccess{Board: b, Role: port.RoleOwner}, nil
}

// shelf は listed の全部を Find でも引けるようにして並べる。
func shelf(boards ...port.Board) *fakeBoardShelf {
	stored := make(map[string]port.Board, len(boards))
	for _, b := range boards {
		stored[b.ID] = b
	}
	return &fakeBoardShelf{listed: boards, stored: stored}
}

func boardWithScene(id, scene string) port.Board {
	b := *newBoard(scene)
	b.ID = id
	return b
}

// annotationHash はシーンの中の注釈 1 つのいまのハッシュ。「作ったまま」を
// 作るには、run にこの値を持たせる。
func annotationHash(t *testing.T, scene, annotationID string) string {
	t.Helper()

	s, err := domain.ParseScene([]byte(scene))
	if err != nil {
		t.Fatalf("ParseScene: %v", err)
	}
	for _, a := range s.Annotations() {
		if a.ID == annotationID {
			return string(s.AnnotationHash(a))
		}
	}
	t.Fatalf("注釈 %s がシーンに無い", annotationID)
	return ""
}

func saveRun(t *testing.T, mappings *fakeMappings, boardID, annotationID, hash string) {
	t.Helper()

	if _, err := mappings.SaveRun(t.Context(), port.SyncRun{
		BoardID:      boardID,
		AnnotationID: annotationID,
		ContentHash:  hash,
		CreatedAt:    baseTime,
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
}

func listBoards(t *testing.T, boards port.BoardRepository, mappings port.MappingRepository) []usecase.BoardListEntry {
	t.Helper()

	got, err := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks()).
		List(t.Context())
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	return got
}

// 3 状態を数え分ける。件数を状態ごとに変えてあるので、取り違えて数えると
// どこかの欄がずれる。
//
// **別のボードの run を混ぜる。** board-2 には board-1 の「まだ作っていない」と
// 同じ注釈 ID・同じハッシュの run を置いてある。ボードで絞らずに引くと、
// 未作成が作成済みに数えられる。
//
// **キャンバスから消えた注釈の run も混ぜる。** 3 状態を持たない（ADR 0046）
// ので、数えると作成済みか変更ありが 1 件増える。
func TestList_CountsAnnotationStatesPerBoard(t *testing.T) {
	t.Parallel()

	boards := shelf(
		boardWithScene("board-1", threeStatesScene),
		boardWithScene("board-2", emptyScene),
	)
	mappings := &fakeMappings{}
	for _, id := range []string{"a-done1", "a-done2"} {
		saveRun(t, mappings, "board-1", id, annotationHash(t, threeStatesScene, id))
	}
	for _, id := range []string{"a-edit1", "a-edit2", "a-edit3"} {
		saveRun(t, mappings, "board-1", id, "書き換える前のハッシュ")
	}
	saveRun(t, mappings, "board-1", "a-gone", "消えた注釈のハッシュ")
	saveRun(t, mappings, "board-2", "a-new", annotationHash(t, threeStatesScene, "a-new"))

	got := listBoards(t, boards, mappings)

	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2", len(got))
	}
	want := map[string]usecase.AnnotationCounts{
		"board-1": {Uncreated: 1, Created: 2, Changed: 3},
		// 注釈の無いボードは 0 件で返す。null（読めなかった）と区別する。
		"board-2": {},
	}
	for _, e := range got {
		if e.Counts == nil {
			t.Errorf("%s: Counts = nil, want %+v", e.Board.ID, want[e.Board.ID])
			continue
		}
		if *e.Counts != want[e.Board.ID] {
			t.Errorf("%s: Counts = %+v, want %+v", e.Board.ID, *e.Counts, want[e.Board.ID])
		}
	}
}

// **並びは一覧のまま。** 更新時刻の降順で返る並び（ADR 0019）を、数えるために
// 引き直した順で崩さない。
func TestList_KeepsListOrder(t *testing.T) {
	t.Parallel()

	boards := shelf(
		boardWithScene("board-b", emptyScene),
		boardWithScene("board-a", emptyScene),
		boardWithScene("board-c", emptyScene),
	)

	got := listBoards(t, boards, &fakeMappings{})

	var ids []string
	for _, e := range got {
		ids = append(ids, e.Board.ID)
	}
	if len(ids) != 3 || ids[0] != "board-b" || ids[1] != "board-a" || ids[2] != "board-c" {
		t.Errorf("並び = %v, want [board-b board-a board-c]", ids)
	}
}

// **1 枚のシーンが読めなくても、一覧は返す。** 読めないボードだけ件数を nil に
// する。ここが切れると、壊れたシーンを 1 枚持つだけで、ほかのボードまで一覧から
// 開けなくなる。
//
// 諦めるのは件数だけ。**名前や作成先は一覧のまま出す。** 開けば、読めなかった
// ことはボードの画面が通知で出す。
func TestList_LeavesCountsNilWhenSceneIsUnreadable(t *testing.T) {
	t.Parallel()

	boards := shelf(
		boardWithScene("board-broken", "{"),
		boardWithScene("board-1", threeStatesScene),
	)

	got := listBoards(t, boards, &fakeMappings{})

	if len(got) != 2 {
		t.Fatalf("件数 = %d, want 2（読めないボードも一覧には残す）", len(got))
	}
	if got[0].Counts != nil {
		t.Errorf("board-broken: Counts = %+v, want nil", *got[0].Counts)
	}
	if got[0].Board.Name == "" {
		t.Error("board-broken: 名前が落ちている。一覧の中身は残す")
	}
	if got[1].Counts == nil || got[1].Counts.Uncreated != 6 {
		t.Errorf("board-1: Counts = %+v, want 未作成 6", got[1].Counts)
	}
}

// 一覧を引いてから数えるまでのあいだに消えた（か、メンバーから外れた）ボードは
// 一覧から落とす。**開けないボードを「読めなかった」として並べない。** 押しても
// not_found が返るだけになる。
func TestList_DropsBoardsGoneBeforeCounting(t *testing.T) {
	t.Parallel()

	boards := shelf(boardWithScene("board-1", emptyScene))
	boards.listed = append(boards.listed, boardWithScene("board-gone", emptyScene))

	got := listBoards(t, boards, &fakeMappings{})

	if len(got) != 1 || got[0].Board.ID != "board-1" {
		t.Errorf("一覧 = %+v, want board-1 だけ", got)
	}
}

// failingLatestRuns は最新の run を引くところで失敗する。
type failingLatestRuns struct {
	fakeMappings
	err error
}

func (f *failingLatestRuns) ListLatestRunsByBoard(context.Context, string) ([]port.SyncRun, error) {
	return nil, f.err
}

// **run を引けないのはシーンが壊れているのとは違う。** DB の失敗なので一覧ごと
// 失敗にする。件数を nil にして続けると、DB が落ちているのに「このボードの
// シーンが読めない」と見せることになる。
func TestList_FailsWhenRunsCannotBeRead(t *testing.T) {
	t.Parallel()

	boards := shelf(boardWithScene("board-1", threeStatesScene))
	boom := errors.New("database is locked")

	_, err := usecase.NewBoardService(boards, &failingLatestRuns{err: boom}, usecase.NewBoardLocks()).
		List(t.Context())
	if !errors.Is(err, boom) {
		t.Errorf("List() = %v, want %v", err, boom)
	}
}
