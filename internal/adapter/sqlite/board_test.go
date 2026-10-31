package sqlite_test

import (
	"errors"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// ---------------------------------------------------------------------------
// D: BoardRepository
// ---------------------------------------------------------------------------

// C-9: ボードを削除すると run と items とメンバーが CASCADE で消える。
// SQLite は既定で外部キーを検査しないため、これは PRAGMA foreign_keys の
// 設定が効いているかの確認でもある。
func TestDeleteBoard_CascadesToRunsItemsAndMembers(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	seedBoard(t, db, "board-1")
	repo := sqlite.NewMappingRepository(db)

	if _, err := repo.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1",
		ContentHash: "hash-1", CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{item("e1", "PVTI_1", port.KindEpic, nil)},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	// **リポジトリの口を通す。** 生の DELETE で確かめると、CASCADE が効くことは
	// 分かっても、実際の削除経路が同じ範囲を消すのかは分からない（ADR 0042）。
	if err := sqlite.NewBoardRepository(db).Delete(t.Context(), "", "board-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	for _, table := range []string{"sync_runs", "sync_items", "board_members"} {
		var n int
		if err := db.QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s に %d 件残っている, want 0（CASCADE が効いていない）", table, n)
		}
	}
}

// D-1: 保存したものがそのまま読み戻せる。
func TestBoard_RoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	want := port.Board{
		ID:        "board-1",
		Name:      "決済まわりのブレスト",
		Scene:     `{"elements":[{"id":"a"}]}`,
		CreatedAt: baseTime,
		UpdatedAt: baseTime,
	}
	if err := repo.Create(t.Context(), want, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("Find returned nil")
	}

	if got.Board.ID != want.ID || got.Board.Name != want.Name || got.Board.Scene != want.Scene {
		t.Errorf("got = %+v, want %+v", got.Board, want)
	}
	if !got.Board.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.Board.CreatedAt, want.CreatedAt)
	}
}

// D-1a: 作成先の表示名も一緒に読み戻せる（ADR 0019）。
//
// project_id は不透明な node ID なので、一覧を Project 名でまとめるには
// 選んだ時点の番号と名前が要る。URL も同じ扱いで、GitHub へ辿る導線に要る
// （ADR 0025）。Create と UpdateTarget の両方で残ること。
func TestBoard_TargetDisplayNameRoundTrip(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	created := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_1",
		ProjectNumber:   3,
		ProjectTitle:    "ロードマップ",
		ProjectURL:      "https://github.com/orgs/acme/projects/3",
	}
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "b", Scene: "{}", Target: created,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target != created {
		t.Errorf("Target = %+v, want %+v", got.Board.Target, created)
	}

	updated := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "api",
		ProjectID:       "PVT_2",
		ProjectNumber:   7,
		ProjectTitle:    "技術的負債",
		ProjectURL:      "https://github.com/users/acme/projects/7",
	}
	if err := repo.UpdateTarget(t.Context(), "", "board-1", updated); err != nil {
		t.Fatalf("UpdateTarget: %v", err)
	}

	got, err = repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target != updated {
		t.Errorf("Target = %+v, want %+v", got.Board.Target, updated)
	}
	// **作成先の設定で版を進めてはいけない**（ADR 0092）。改名と同じで、
	// 進めると開いている別のメンバーの次の保存が理由なく 409 になる。ここが
	// 切れると、SET に updated_at を戻した実装が素通りする。
	if !got.Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（作成先の設定で進めてはいけない）",
			got.Board.UpdatedAt, baseTime)
	}
}

// D-1a2: 表示名だけを取り直せる。作成先そのものは動かない（ADR 0037）。
//
// 固定後に通る唯一の経路なので、ここが作成先まで書けるようになると
// ADR 0014 の固定が意味を失う。
func TestBoard_UpdateTargetDisplayKeepsTarget(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	target := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_1",
		ProjectNumber:   3,
		ProjectTitle:    "ロードマップ",
		ProjectURL:      "https://github.com/orgs/acme/projects/3",
	}
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "b", Scene: "{}", Target: target,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	display := port.BoardTargetDisplay{
		ProjectNumber: 4,
		ProjectTitle:  "改名後のロードマップ",
		ProjectURL:    "https://github.com/orgs/acme/projects/4",
	}
	if err := repo.UpdateTargetDisplay(t.Context(), "", "board-1", display); err != nil {
		t.Fatalf("UpdateTargetDisplay: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	want := target
	want.ProjectNumber = display.ProjectNumber
	want.ProjectTitle = display.ProjectTitle
	want.ProjectURL = display.ProjectURL
	if got.Board.Target != want {
		t.Errorf("Target = %+v, want %+v", got.Board.Target, want)
	}
	// **表示名の取り直しで版を進めてはいけない**（ADR 0092）。固定後も owner が
	// 押せる口なので、進めると同じボードを描いている人の保存がその場で断られる。
	if !got.Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（表示名の取り直しで進めてはいけない）",
			got.Board.UpdatedAt, baseTime)
	}
}

// D-1b: 表示名を送らずに設定した作成先は「名前を知らない」として残る。
//
// 表示名は任意（ADR 0019）。0 と空文字で保存され、それでも作成先としては
// 選ばれている。ここが崩れると、画面を通さずに設定したボードが未選択に見える。
func TestBoard_TargetWithoutDisplayNameIsStillSelected(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	target := port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_1",
	}
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "b", Scene: "{}", Target: target,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Target.ProjectNumber != 0 || got.Board.Target.ProjectTitle != "" ||
		got.Board.Target.ProjectURL != "" {
		t.Errorf("表示用の値 = %d/%q/%q, want 0 と空文字",
			got.Board.Target.ProjectNumber, got.Board.Target.ProjectTitle,
			got.Board.Target.ProjectURL)
	}
	if !got.Board.Target.Selected() {
		t.Error("表示名が無いだけで未選択になっている")
	}
}

// D-2: シーン更新で updated_at は進み、created_at は変わらない。
func TestBoard_UpdateScene(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	later := baseTime.Add(time.Hour)
	if _, err := repo.UpdateScene(
		t.Context(), "", "board-1", port.SceneWrite{Scene: `{"elements":["updated"]}`},
		baseTime, later,
	); err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Scene != `{"elements":["updated"]}` {
		t.Errorf("Scene = %q", got.Board.Scene)
	}
	if !got.Board.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", got.Board.UpdatedAt, later)
	}
	if !got.Board.CreatedAt.Equal(baseTime) {
		t.Errorf("CreatedAt = %v, want %v（更新で変わってはいけない）", got.Board.CreatedAt, baseTime)
	}
}

func TestBoard_UpdateName(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	if err := repo.UpdateName(t.Context(), "", "board-1", "新しい名前"); err != nil {
		t.Fatalf("UpdateName: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Name != "新しい名前" {
		t.Errorf("Name = %q, want 新しい名前", got.Board.Name)
	}
	// **改名で版を進めてはいけない**（ADR 0020）。updated_at はシーンの版で、
	// 保存の照合基準になっている。進めると、そのボードを開いている別の
	// メンバーの次の保存が、誰もシーンを触っていないのに 409 で断られる。
	// ここが切れると、SET に updated_at を足した実装が素通りする。
	if !got.Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（改名で進めてはいけない）",
			got.Board.UpdatedAt, baseTime)
	}
	// 名前だけを書く。SET にシーンが混ざると、改名でブレストが消える。
	if got.Board.Scene != `{"elements":[]}` {
		t.Errorf("Scene = %q（改名で書き換えてはいけない）", got.Board.Scene)
	}
}

// メンバーでないボードは改名できない。絞りを外すと、ID を知っているだけで
// 他人のボードの名前を書き換えられる（ADR 0016）。
func TestBoard_UpdateNameRequiresMembership(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	err := repo.UpdateName(t.Context(), "someone-else", "board-1", "乗っ取り")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("UpdateName = %v, want port.ErrNotFound", err)
	}

	got, findErr := repo.Find(t.Context(), "", "board-1")
	if findErr != nil {
		t.Fatalf("Find: %v", findErr)
	}
	if got.Board.Name != "テストボード" {
		t.Errorf("Name = %q, want テストボード（書き換わってはいけない）", got.Board.Name)
	}
}

// 非メンバーは消せない。Find を通らずに直接 DELETE する経路なので、絞り忘れると
// 他人のボードを消せる（ADR 0016）。
func TestBoard_DeleteRequiresMembership(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	err := repo.Delete(t.Context(), "someone-else", "board-1")
	if !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("Delete = %v, want port.ErrNotFound", err)
	}

	got, findErr := repo.Find(t.Context(), "", "board-1")
	if findErr != nil {
		t.Fatalf("Find: %v", findErr)
	}
	if got == nil {
		t.Error("非メンバーの削除でボードが消えている")
	}
}

// 消えたら一覧からも引き当てからも外れる。「消した」と言えるのはここまで。
func TestBoard_DeleteRemovesFromFindAndList(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")
	seedBoard(t, db, "board-2")

	if err := repo.Delete(t.Context(), "", "board-1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != nil {
		t.Error("削除したボードが Find で引ける")
	}

	boards, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 残したほうまで消えていないことも見る。WHERE を書き損なうと全部消える。
	if len(boards) != 1 || boards[0].Board.ID != "board-2" {
		t.Errorf("List = %+v, want board-2 だけ", boards)
	}
}

func TestBoard_DeleteNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	err := repo.Delete(t.Context(), "", "no-such-board")
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("Delete = %v, want port.ErrNotFound", err)
	}
}

func TestBoard_UpdateNameNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	err := repo.UpdateName(t.Context(), "", "no-such-board", "名前")
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("UpdateName = %v, want port.ErrNotFound", err)
	}
}

func TestBoard_UpdateSceneNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	_, err := repo.UpdateScene(
		t.Context(), "", "no-such-board", port.SceneWrite{Scene: "{}"}, baseTime, baseTime)
	if !errors.Is(err, port.ErrNotFound) {
		t.Errorf("UpdateScene = %v, want port.ErrNotFound", err)
	}
}

// 基準が古ければ 1 文の中で弾く。ボードは共有できるので、2 人が同じボードを
// 開いて別々に描く状況は例外ではない（ADR 0020）。
func TestBoard_UpdateSceneRejectsStaleBase(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)
	seedBoard(t, db, "board-1")

	// 先に保存した側。ここで版が baseTime から later に進む。
	later := baseTime.Add(time.Hour)
	if _, err := repo.UpdateScene(
		t.Context(), "", "board-1", port.SceneWrite{Scene: `{"elements":["first"]}`},
		baseTime, later,
	); err != nil {
		t.Fatalf("UpdateScene（先に保存した側）: %v", err)
	}

	// 後から保存する側は、開いたときの版のまま送ってくる。
	_, err := repo.UpdateScene(
		t.Context(), "", "board-1", port.SceneWrite{Scene: `{"elements":["second"]}`},
		baseTime, later.Add(time.Minute))
	if !errors.Is(err, port.ErrConflict) {
		t.Fatalf("UpdateScene = %v, want port.ErrConflict", err)
	}

	// 「無い」と混ぜない。無いものは待っても現れないが、食い違いは相手の
	// 変更を取り込めば進める。
	if errors.Is(err, port.ErrNotFound) {
		t.Error("版の食い違いを ErrNotFound で返している")
	}

	got, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Board.Scene != `{"elements":["first"]}` {
		t.Errorf("Scene = %q, want 先に保存した側のまま", got.Board.Scene)
	}
	if !got.Board.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v（弾いたのに進んでいる）", got.Board.UpdatedAt, later)
	}
}

// D-3: 存在しない ID は (nil, nil)。
func TestBoard_FindNotFound(t *testing.T) {
	t.Parallel()

	db := newDB(t)

	got, err := sqlite.NewBoardRepository(db).Find(t.Context(), "", "no-such-board")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != nil {
		t.Errorf("Find = %+v, want nil", got)
	}
}

// D-4: 一覧は更新時刻の降順。
func TestBoard_ListOrder(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	create := func(id string, updatedAt time.Time) {
		t.Helper()
		if err := repo.Create(t.Context(), port.Board{
			ID: id, Name: id, Scene: "{}",
			CreatedAt: baseTime, UpdatedAt: updatedAt,
		}, ""); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
	}

	create("old", baseTime)
	create("newest", baseTime.Add(2*time.Hour))
	create("middle", baseTime.Add(time.Hour))

	got, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var ids []string
	for _, a := range got {
		ids = append(ids, a.Board.ID)
	}
	want := []string{"newest", "middle", "old"}
	if len(ids) != len(want) {
		t.Fatalf("件数 = %d, want %d", len(ids), len(want))
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("順序 = %v, want %v", ids, want)
		}
	}
}

// 一覧はシーンを読まない。読むと、返さないもののために全ボードぶんの
// シーン JSON をメモリへ載せることになる（画像は base64 で入りうる）。
//
// ここが落ちるのは List が boardColumns 側に戻ったとき。Find は逆に
// シーンを返し続ける必要があるので、両方を 1 つのテストで固定する。
func TestBoard_ListOmitsScene(t *testing.T) {
	t.Parallel()

	db := newDB(t)
	repo := sqlite.NewBoardRepository(db)

	const scene = `{"elements":[{"id":"a"}]}`
	if err := repo.Create(t.Context(), port.Board{
		ID: "board-1", Name: "決済まわりのブレスト", Scene: scene,
		CreatedAt: baseTime, UpdatedAt: baseTime,
	}, ""); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].Board.Scene != "" {
		t.Errorf("List が Scene を返している: %q", got[0].Board.Scene)
	}
	// シーン以外は一覧に要る。落とすと表示名も作成先も出せなくなる。
	if got[0].Board.Name != "決済まわりのブレスト" {
		t.Errorf("Name = %q", got[0].Board.Name)
	}
	if !got[0].Board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v", got[0].Board.UpdatedAt, baseTime)
	}

	found, err := repo.Find(t.Context(), "", "board-1")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.Board.Scene != scene {
		t.Errorf("Find の Scene = %q, want %q", found.Board.Scene, scene)
	}
}

func TestBoard_ListEmpty(t *testing.T) {
	t.Parallel()

	got, err := sqlite.NewBoardRepository(newDB(t)).List(t.Context(), "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0", len(got))
	}
}
