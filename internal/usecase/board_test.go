package usecase_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 作成先はボードごとに持ち、最初の draft issue を作ると固定される（ADR 0014）。

func newTarget() port.BoardTarget {
	return port.BoardTarget{
		RepositoryOwner: "acme",
		RepositoryName:  "web",
		ProjectID:       "PVT_2",
		ProjectNumber:   4,
		ProjectTitle:    "技術的負債",
	}
}

// run が 1 件も無いうちは選び直せる。ブレストを始める前なら GitHub 側に
// 何も無いので、変えて困るものが無い。
func TestSetTarget_SucceedsBeforeAnyRun(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

	if err := svc.SetTarget(t.Context(), "board-1", newTarget()); err != nil {
		t.Fatalf("SetTarget() = %v", err)
	}
	if boards.writes != 1 {
		t.Errorf("writes = %d, want 1", boards.writes)
	}
}

// run が 1 件でもあれば固定。sync_runs は GitHub 側に残っている item の
// 追跡表であり、作成先が変わると記録が指す先を見失う。
func TestSetTarget_RejectsAfterFirstRun(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	err := svc.SetTarget(t.Context(), "board-1", newTarget())
	if !errors.Is(err, usecase.ErrTargetLocked) {
		t.Fatalf("SetTarget() = %v, want ErrTargetLocked", err)
	}
	if boards.writes != 0 {
		t.Errorf("固定済みなのに書き込んでいる: writes = %d", boards.writes)
	}
}

// 同じ値でも通さない。通すと「変更できた」と「たまたま同じだった」を
// 呼び出し側が区別できなくなる。
func TestSetTarget_RejectsSameValueAfterRun(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	boards := &fakeBoards{board: board}
	mappings := &fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	if err := svc.SetTarget(t.Context(), "board-1", board.Target); !errors.Is(err, usecase.ErrTargetLocked) {
		t.Fatalf("SetTarget() = %v, want ErrTargetLocked", err)
	}
}

// 固定するのは作成先そのもの。表示名は固定の対象ではない（ADR 0037）。
// GitHub 側で Project を改名すると、直す手段が無いまま古い名前が残っていた。
func TestRefreshTargetDisplay_SucceedsAfterFirstRun(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	boards := &fakeBoards{board: board}
	mappings := &fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	// 値で控える。同じボードを指しているので、後で比べるには複製が要る。
	before := board.Target

	display := port.BoardTargetDisplay{
		ProjectNumber: 7,
		ProjectTitle:  "改名後のロードマップ",
		ProjectURL:    "https://github.com/orgs/acme/projects/7",
	}
	if err := svc.RefreshTargetDisplay(t.Context(), "board-1", board.Target.ProjectID, display); err != nil {
		t.Fatalf("RefreshTargetDisplay() = %v", err)
	}
	if boards.display != display {
		t.Errorf("書かれた表示名 = %+v, want %+v", boards.display, display)
	}
	// 作成先そのものは触らない。触れる経路にすると固定が意味を失う。
	got := boards.board.Target
	if got.RepositoryOwner != before.RepositoryOwner ||
		got.RepositoryName != before.RepositoryName ||
		got.ProjectID != before.ProjectID {
		t.Errorf("作成先が変わっている: %+v, want %+v", got, before)
	}
}

// この口から作成先は動かせない。ErrTargetLocked に相乗りさせないのは、
// 直し方が違うため（固定は解けないが、食い違いは開き直せば解ける）。
func TestRefreshTargetDisplay_RejectsOtherProject(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

	err := svc.RefreshTargetDisplay(t.Context(), "board-1", "PVT_other",
		port.BoardTargetDisplay{ProjectTitle: "別の Project"})
	if !errors.Is(err, usecase.ErrTargetMismatch) {
		t.Fatalf("RefreshTargetDisplay() = %v, want ErrTargetMismatch", err)
	}
	if boards.writes != 0 {
		t.Errorf("食い違っているのに書き込んでいる: writes = %d", boards.writes)
	}
}

// どの作成先の表示名なのかを名乗らせる。空を通すと、この口が
// 「いまの作成先が何であれ書く」ものになる。
func TestRefreshTargetDisplay_RejectsEmptyProjectID(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

	err := svc.RefreshTargetDisplay(t.Context(), "board-1", "", port.BoardTargetDisplay{})
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("RefreshTargetDisplay() = %v, want ErrInvalidInput", err)
	}
}

// 作成先が未選択のボードには表示名も無い。
func TestRefreshTargetDisplay_RejectsUnselectedTarget(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	board.Target = port.BoardTarget{}
	boards := &fakeBoards{board: board}
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

	err := svc.RefreshTargetDisplay(t.Context(), "board-1", "PVT_1", port.BoardTargetDisplay{})
	if !errors.Is(err, usecase.ErrTargetNotSelected) {
		t.Fatalf("RefreshTargetDisplay() = %v, want ErrTargetNotSelected", err)
	}
}

// URL の門番は作成先の設定と同じ（ADR 0025）。こちらだけ素通しにすると、
// 固定済みのボードに javascript: を入れられる。
func TestRefreshTargetDisplay_RejectsUnsafeProjectURL(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	boards := &fakeBoards{board: board}
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

	err := svc.RefreshTargetDisplay(t.Context(), "board-1", board.Target.ProjectID,
		port.BoardTargetDisplay{ProjectURL: "javascript:alert(1)"})
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("RefreshTargetDisplay() = %v, want ErrInvalidInput", err)
	}
	if boards.writes != 0 {
		t.Errorf("危険な URL を書き込んでいる: writes = %d", boards.writes)
	}
}

func TestSetTarget_RejectsIncompleteTarget(t *testing.T) {
	t.Parallel()

	for name, target := range map[string]port.BoardTarget{
		"すべて空":        {},
		"project が無い": {RepositoryOwner: "acme", RepositoryName: "web"},
		"リポジトリ名が無い":   {RepositoryOwner: "acme", ProjectID: "PVT_2"},
		"リポジトリ所有者が無い": {RepositoryName: "web", ProjectID: "PVT_2"},
		// 表示名は作成先そのものではない（ADR 0019）。埋まっていても
		// 作成先が決まったことにはならない。
		"表示名だけある": {ProjectNumber: 3, ProjectTitle: "ロードマップ"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

			if err := svc.SetTarget(t.Context(), "board-1", target); !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("SetTarget() = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// 作成先 URL はリクエストから来る。保存された値はフロントがそのまま href に
// 入れるので、ここが唯一の門番になる（ADR 0025）。owner は招待した相手に画面を
// 見せられる（ADR 0017）ため、`javascript:` を保存できると相手のブラウザで走る。
// 外部ドメインは「GitHub でこの Project を開く」の文言で別の場所へ送れる。
func TestSetTarget_RejectsUnsafeProjectURL(t *testing.T) {
	t.Parallel()

	selected := func(u string) port.BoardTarget {
		return port.BoardTarget{
			RepositoryOwner: "acme", RepositoryName: "web", ProjectID: "PVT_1",
			ProjectURL: u,
		}
	}

	for name, raw := range map[string]string{
		"javascript": "javascript:alert(1)",
		"data":       "data:text/html,<script>alert(1)</script>",
		"http":       "http://github.com/orgs/acme/projects/1",
		"別ドメイン":      "https://evil.example/orgs/acme/projects/1",
		"ホストの後ろに足したドメイン": "https://github.com.evil.example/orgs/acme/projects/1",
		"認証情報つき":         "https://evil.example@github.com/orgs/acme/projects/1",
		"スキームなし":         "github.com/orgs/acme/projects/1",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

			if err := svc.SetTarget(t.Context(), "board-1", selected(raw)); !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("SetTarget(%q) = %v, want ErrInvalidInput", raw, err)
			}
			if _, err := svc.Create(t.Context(), "b", "", selected(raw)); !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("Create(%q) = %v, want ErrInvalidInput", raw, err)
			}
		})
	}
}

// 空文字は「URL を知らない」で、弾く相手ではない（ADR 0025）。パスの形は見ない。
// /orgs か /users かは owner が user か org かで変わり、etoki は知らない。
func TestSetTarget_AcceptsGitHubProjectURL(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"知らない":    "",
		"org":     "https://github.com/orgs/acme/projects/1",
		"user":    "https://github.com/users/yusuke0610/projects/4",
		"見慣れないパス": "https://github.com/acme/web/projects",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks())

			target := port.BoardTarget{
				RepositoryOwner: "acme", RepositoryName: "web", ProjectID: "PVT_1",
				ProjectURL: raw,
			}
			if err := svc.SetTarget(t.Context(), "board-1", target); err != nil {
				t.Fatalf("SetTarget(%q) = %v", raw, err)
			}
		})
	}
}

func TestSetTarget_RejectsUnknownBoard(t *testing.T) {
	t.Parallel()

	svc := usecase.NewBoardService(&fakeBoards{}, &fakeMappings{}, usecase.NewBoardLocks())

	// 存在しないボードと、メンバーでないボードは同じ扱いにする。区別すると
	// ID を総当たりして他人のボードの存在を確かめられる（ADR 0016 / 0017）。
	if err := svc.SetTarget(t.Context(), "missing", newTarget()); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Fatalf("SetTarget() = %v, want ErrBoardNotFound", err)
	}
}

// 共有ボードの同時編集は後勝ちにしない。保存はシーン全体を書くので、通すと
// 消えるのは「相手が触った要素」ではなく相手の作業すべてになる（ADR 0020）。
func TestSaveScene_RejectsStaleBase(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := newBoardService(boards)

	// 相手が先に保存した後の姿。こちらが開いたときの版はもう古い。
	stale := boards.board.UpdatedAt.Add(-time.Hour)

	_, err := svc.SaveScene(t.Context(), "board-1", emptyScene, stale)
	if !errors.Is(err, usecase.ErrSceneConflict) {
		t.Fatalf("SaveScene() = %v, want ErrSceneConflict", err)
	}
	if boards.writes != 0 {
		t.Errorf("食い違っているのに書き込んでいる: writes = %d", boards.writes)
	}
}

// 保存できたら次の基準を返す。返さないと、続けて保存するたびにボードを
// 取り直すことになる。
func TestSaveScene_ReturnsNextBase(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	saved := boards.board.UpdatedAt.Add(time.Hour)
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks(),
		usecase.WithClock(func() time.Time { return saved }))

	got, err := svc.SaveScene(t.Context(), "board-1", emptyScene, boards.board.UpdatedAt)
	if err != nil {
		t.Fatalf("SaveScene() = %v", err)
	}
	if !got.Equal(saved) {
		t.Errorf("返った版 = %v, want %v", got, saved)
	}

	// 返った版で続けて保存できる。ここが食い違うと、2 回目の保存が必ず衝突する。
	if _, err := svc.SaveScene(t.Context(), "board-1", emptyScene, got); err != nil {
		t.Errorf("返った版で保存できない: %v", err)
	}
}

// 時計の分解能に検知を預けない。同じ時刻の読みで 2 回保存できてしまうと、
// 後から来た古い基準が一致し、照合を置いた意味が無くなる（ADR 0020）。
//
// 時計を止めて、同時刻の連続保存を強制的に作って確かめる。
func TestSaveScene_AdvancesVersionWhenTheClockDoesNot(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	stopped := boards.board.UpdatedAt
	svc := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks(),
		usecase.WithClock(func() time.Time { return stopped }))

	saved, err := svc.SaveScene(t.Context(), "board-1", emptyScene, stopped)
	if err != nil {
		t.Fatalf("SaveScene() = %v", err)
	}
	if !saved.After(stopped) {
		t.Errorf("版 = %v, want %v より後（時計が止まっていても進める）", saved, stopped)
	}

	// 同じ基準での 2 回目は、時計が動いていなくても弾かれる。
	if _, err := svc.SaveScene(t.Context(), "board-1", emptyScene, stopped); !errors.Is(err, usecase.ErrSceneConflict) {
		t.Fatalf("SaveScene() = %v, want ErrSceneConflict", err)
	}
	// 返った版では続けて保存できる。進めた版が次の基準として使える。
	if _, err := svc.SaveScene(t.Context(), "board-1", emptyScene, saved); err != nil {
		t.Errorf("進めた版で保存できない: %v", err)
	}
}

// 基準の未指定は「照合しない」に倒さない。倒すと API を直接叩く経路で照合を
// 素通りでき、防ぎたい後勝ちがそのまま残る（ADR 0010 と同じ理由）。
func TestSaveScene_RequiresBase(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}

	_, err := newBoardService(boards).SaveScene(t.Context(), "board-1", emptyScene, time.Time{})
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("SaveScene() = %v, want ErrInvalidInput", err)
	}
	if boards.writes != 0 {
		t.Errorf("基準が無いのに書き込んでいる: writes = %d", boards.writes)
	}
}

// シーンの大きさは上限ちょうどまで通す。境界を「未満」で書くと、上限ぴったりの
// ボードが理由の分からないまま保存できなくなる。
func TestSaveScene_AcceptsSceneAtTheLimit(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	scene := sceneOfSize(t, usecase.MaxSceneBytes)

	if _, err := newBoardService(boards).SaveScene(
		t.Context(), "board-1", scene, boards.board.UpdatedAt); err != nil {
		t.Fatalf("SaveScene() = %v", err)
	}
	if boards.writes != 1 {
		t.Errorf("writes = %d, want 1", boards.writes)
	}
}

// 1 バイト超えたら弾く。**縮小も切り捨てもしない**（ADR 0038）ので、
// 書かずに返ることまで見る。エラーだけを見ていると、弾く前に書いてしまう実装でも
// 緑になる。
func TestSaveScene_RejectsSceneOverTheLimit(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	scene := sceneOfSize(t, usecase.MaxSceneBytes+1)

	_, err := newBoardService(boards).SaveScene(
		t.Context(), "board-1", scene, boards.board.UpdatedAt)
	if !errors.Is(err, usecase.ErrSceneTooLarge) {
		t.Fatalf("SaveScene() = %v, want ErrSceneTooLarge", err)
	}
	// ErrInvalidInput に畳むと 400 になり、画面は「送った内容を直せ」としか
	// 言えない。大きさの打ち手は「貼った画像を減らす」なので分けてある。
	if errors.Is(err, usecase.ErrInvalidInput) {
		t.Error("大きさの拒否を入力の誤りに畳んでいる")
	}
	if boards.writes != 0 {
		t.Errorf("上限を超えたのに書き込んでいる: writes = %d", boards.writes)
	}
}

// 作成の入口も同じ上限で見る。片方だけだと、作成時に上限を超えたシーンを
// 置いてしまい、そのボードは開いた瞬間から保存できない。
func TestCreate_RejectsSceneOverTheLimit(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{}
	scene := sceneOfSize(t, usecase.MaxSceneBytes+1)

	_, err := newBoardService(boards).Create(t.Context(), "b", scene, newTarget())
	if !errors.Is(err, usecase.ErrSceneTooLarge) {
		t.Fatalf("Create() = %v, want ErrSceneTooLarge", err)
	}
	if boards.writes != 0 {
		t.Errorf("上限を超えたのに作っている: writes = %d", boards.writes)
	}
}

// sceneOfSize は指定したバイト数ちょうどの、読めるシーン JSON を作る。
//
// 実際に大きさを押し上げるのは貼った画像（base64 でシーンに乗る）だが、ここで
// 要るのはバイト数だけなのでテキスト要素の本文で埋める。
func sceneOfSize(t *testing.T, size int) string {
	t.Helper()

	const shell = `{"type":"excalidraw","version":2,"source":"etoki",` +
		`"elements":[{"id":"t1","type":"text","text":""}],"appState":{}}`
	if size < len(shell) {
		t.Fatalf("size = %d だが、包みだけで %d バイトある", size, len(shell))
	}

	scene := strings.Replace(shell, `"text":""`,
		`"text":"`+strings.Repeat("a", size-len(shell))+`"`, 1)
	if len(scene) != size {
		t.Fatalf("len(scene) = %d, want %d", len(scene), size)
	}
	return scene
}

// 改名は名前だけを書く。**版は進めない**（ADR 0020）。進めると、そのボードを
// 開いている別のメンバーの次の保存が、誰もシーンを触っていないのに 409 になる。
func TestRename(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}

	if err := newBoardService(boards).Rename(t.Context(), "board-1", "新しい名前"); err != nil {
		t.Fatalf("Rename() = %v", err)
	}
	if boards.board.Name != "新しい名前" {
		t.Errorf("Name = %q, want 新しい名前", boards.board.Name)
	}
	if !boards.board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（改名で進めてはいけない）",
			boards.board.UpdatedAt, baseTime)
	}
}

// 前後の空白は落とす。落としたうえで空なら弾く。空白だけの名前を通すと、
// 一覧に見出しの無い行が並び、開くまでどのボードか分からなくなる。
func TestRename_TrimsAndRejectsBlank(t *testing.T) {
	t.Parallel()

	t.Run("前後の空白を落として書く", func(t *testing.T) {
		t.Parallel()

		boards := &fakeBoards{board: newBoard(interpretScene)}
		if err := newBoardService(boards).Rename(
			t.Context(), "board-1", "  余白つき  "); err != nil {
			t.Fatalf("Rename() = %v", err)
		}
		if boards.board.Name != "余白つき" {
			t.Errorf("Name = %q, want 余白つき", boards.board.Name)
		}
	})

	for _, name := range []string{"", "   ", "\t\n"} {
		t.Run("空とみなして弾く/"+name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			err := newBoardService(boards).Rename(t.Context(), "board-1", name)

			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("Rename(%q) = %v, want ErrInvalidInput", name, err)
			}
			// 弾いたのだから書かれていない。エラーだけを見ていると、
			// 書いてから弾く実装でも緑になる。
			if boards.writes != 0 {
				t.Errorf("弾いたのに書き込んでいる: writes = %d", boards.writes)
			}
		})
	}
}

func TestTargetLocked(t *testing.T) {
	t.Parallel()

	svc := usecase.NewBoardService(&fakeBoards{}, &fakeMappings{}, usecase.NewBoardLocks())
	locked, err := svc.TargetLocked(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("TargetLocked() = %v", err)
	}
	if locked {
		t.Error("run が無いのに固定されている")
	}

	withRun := usecase.NewBoardService(&fakeBoards{},
		&fakeMappings{runs: []port.SyncRun{{BoardID: "board-1"}}}, usecase.NewBoardLocks())
	if locked, err = withRun.TargetLocked(t.Context(), "board-1"); err != nil {
		t.Fatalf("TargetLocked() = %v", err)
	}
	if !locked {
		t.Error("run があるのに固定されていない")
	}
}

// 作成先が未選択のボードには作れない。置き場所が無いまま GitHub を叩くと、
// 原因が設定不足だと分かりにくい形で失敗する。
func TestCreate_RejectsBoardWithoutTarget(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	board.Target = port.BoardTarget{}

	gh := &fakeGitHub{fields: projectFields()}
	svc := usecase.NewCreationService(&fakeBoards{board: board}, &fakeMappings{}, gh,
		usecase.NewBoardLocks())

	_, err := svc.Create(t.Context(), "board-1", "annot-1", "sha256:whatever", interpretation())
	if !errors.Is(err, usecase.ErrTargetNotSelected) {
		t.Fatalf("Create() = %v, want ErrTargetNotSelected", err)
	}
	if len(gh.calls) != 0 {
		t.Errorf("GitHub を叩いている: %+v", gh.calls)
	}
}

// 作成先はボードから取る。プロセス全体の設定ではない。
func TestCreate_UsesTargetProjectOfBoard(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	board.Target.ProjectID = "PVT_board_specific"

	gh := &fakeGitHub{fields: projectFields()}
	svc := usecase.NewCreationService(&fakeBoards{board: board}, &fakeMappings{}, gh,
		usecase.NewBoardLocks())

	if _, err := svc.Create(
		t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation(),
	); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if gh.projectIDs == nil {
		t.Fatal("GitHub が呼ばれていない")
	}
	for _, id := range gh.projectIDs {
		if id != "PVT_board_specific" {
			t.Fatalf("projectID = %q, want PVT_board_specific", id)
		}
	}
}

// 削除の判断材料は「作成を記録している draft issue が何件か」（ADR 0042）。
//
// **数え方は注釈のカードに出している畳み込みと同じ**（ADR 0026）。更新の run は
// 同じ item に吸収されるので、2 回触った item は 1 件として数える。ここが
// run ごとの合計に変わると、画面が同じボードについて 2 つの数を出すことになる。
func TestDeletion_CountsFoldedItems(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{runs: []port.SyncRun{
		{
			BoardID: "board-1", AnnotationID: "annot-1",
			Items: []port.SyncItem{
				{ItemID: "PVTI_1", LocalID: "e1", Action: port.ActionCreated},
				{ItemID: "PVTI_2", LocalID: "i1", Action: port.ActionCreated},
			},
		},
		{
			// 同じ item を書き換えた run。畳み込みで 1 件に吸収される。
			BoardID: "board-1", AnnotationID: "annot-1",
			Items: []port.SyncItem{
				{ItemID: "PVTI_1", LocalID: "e1", Action: port.ActionUpdated},
			},
		},
		{
			BoardID: "board-1", AnnotationID: "annot-2",
			Items: []port.SyncItem{
				{ItemID: "PVTI_3", LocalID: "e1", Action: port.ActionCreated},
			},
		},
		// 別のボードの run は数えない。混ぜると、消してもいないボードの件数を
		// 見せることになる。
		{
			BoardID: "board-2", AnnotationID: "annot-9",
			Items: []port.SyncItem{
				{ItemID: "PVTI_9", LocalID: "e1", Action: port.ActionCreated},
			},
		},
	}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	d, err := svc.Deletion(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("Deletion() = %v", err)
	}
	if d.RecordedItemCount != 3 {
		t.Errorf("RecordedItemCount = %d, want 3", d.RecordedItemCount)
	}
	if boards.writes != 0 {
		t.Errorf("見せるだけの口が書き込んでいる: writes = %d", boards.writes)
	}
}

// 一度も作っていないボードは 0 件。件数が無いことと引けなかったことを混ぜない。
func TestDeletion_ZeroWithoutRuns(t *testing.T) {
	t.Parallel()

	svc := usecase.NewBoardService(&fakeBoards{board: newBoard(interpretScene)},
		&fakeMappings{}, usecase.NewBoardLocks())

	d, err := svc.Deletion(t.Context(), "board-1")
	if err != nil {
		t.Fatalf("Deletion() = %v", err)
	}
	if d.RecordedItemCount != 0 {
		t.Errorf("RecordedItemCount = %d, want 0", d.RecordedItemCount)
	}
}

// **run があっても削除は拒まない**（ADR 0042）。拒むと、いちばん畳みたいボード
// ——作成先を間違えたまま 1 回作ってしまったボード——が永久に残る。失われるものは
// Deletion で見せ、決めるのは開発者にする（中核思想 3）。
//
// 作成先の固定（ErrTargetLocked）と同じ考え方に倒すと、この判断ごと反転する。
func TestDelete_SucceedsEvenWithRuns(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	mappings := &fakeMappings{runs: []port.SyncRun{{
		BoardID: "board-1", AnnotationID: "annot-1",
		Items: []port.SyncItem{{ItemID: "PVTI_1", LocalID: "e1"}},
	}}}
	svc := usecase.NewBoardService(boards, mappings, usecase.NewBoardLocks())

	if err := svc.Delete(t.Context(), "board-1"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}

	// 消えたことを引き当てで確かめる。エラーが無いことだけを見ると、
	// 何も書かない実装でも通る。
	if _, err := svc.Find(t.Context(), "board-1"); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Fatalf("削除後の Find() = %v, want ErrBoardNotFound", err)
	}
}

// 存在しないボードは「無い」として返す。削除できたことにしない。
func TestDelete_NotFound(t *testing.T) {
	t.Parallel()

	svc := usecase.NewBoardService(&fakeBoards{}, &fakeMappings{}, usecase.NewBoardLocks())

	if err := svc.Delete(t.Context(), "board-1"); !errors.Is(err, usecase.ErrBoardNotFound) {
		t.Fatalf("Delete() = %v, want ErrBoardNotFound", err)
	}
}
