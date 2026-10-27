package usecase_test

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// ボードの背景（ADR 0093）のテスト。背景を書く口、解釈に添える材料、
// 3 状態とジョブの鍵への効き方をここに集める。

// --- 背景を書く口（BoardService.SetPremise） ---

// 背景だけを書き、**版は進めない**（ADR 0092）。進めると、そのボードを開いて
// いる別のメンバーの次の保存が、誰もシーンを触っていないのに 409 になる。
func TestSetPremise_WritesWithoutAdvancingVersion(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}

	if err := newBoardService(boards).SetPremise(t.Context(), "board-1", "", "Go と React"); err != nil {
		t.Fatalf("SetPremise() = %v", err)
	}
	if boards.board.Premise != "Go と React" {
		t.Errorf("Premise = %q, want Go と React", boards.board.Premise)
	}
	if !boards.board.UpdatedAt.Equal(baseTime) {
		t.Errorf("UpdatedAt = %v, want %v（背景で進めてはいけない）",
			boards.board.UpdatedAt, baseTime)
	}
	if boards.board.Scene != interpretScene {
		t.Error("背景を書いただけでシーンが変わった")
	}
}

// 改行を揃え、前後の空白を落とす。途中の空行は書いた人の区切りなので残す。
// 空白だけなら「書いていない」と同じ空にする。
func TestSetPremise_NormalizesNewlinesAndTrims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, in, want string
	}{
		{name: "CRLF と前後の空白", in: "  技術: Go\r\n\r\n用語: 絵 \r\n", want: "技術: Go\n\n用語: 絵"},
		{name: "CR だけの改行", in: "a\rb", want: "a\nb"},
		{name: "空白だけは空", in: " \t\r\n ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			boards.board.Premise = "前の背景"
			if err := newBoardService(boards).SetPremise(t.Context(), "board-1", "前の背景", tt.in); err != nil {
				t.Fatalf("SetPremise() = %v", err)
			}
			if boards.board.Premise != tt.want {
				t.Errorf("Premise = %q, want %q", boards.board.Premise, tt.want)
			}
		})
	}
}

// 上限ちょうどは通し、1 文字でも超えたら切り詰めずに弾く（ADR 0093）。数えるのは
// バイトではなく文字（コードポイント）で、前後の空白を落としてから数える。
func TestSetPremise_LimitIsInCharactersAfterTrimming(t *testing.T) {
	t.Parallel()

	atLimit := strings.Repeat("あ", usecase.MaxPremiseRunes)

	tests := []struct {
		name    string
		premise string
		wantErr bool
	}{
		// 「あ」は UTF-8 で 3 バイト。バイトで数える実装はここで落ちる。
		{name: "ちょうど", premise: atLimit},
		{name: "前後の空白は数えない", premise: "  " + atLimit + "\n\n"},
		{name: "1 文字超える", premise: atLimit + "あ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			boards := &fakeBoards{board: newBoard(interpretScene)}
			err := newBoardService(boards).SetPremise(t.Context(), "board-1", "", tt.premise)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("SetPremise() = %v", err)
				}
				return
			}
			if !errors.Is(err, usecase.ErrPremiseTooLarge) {
				t.Fatalf("SetPremise() = %v, want ErrPremiseTooLarge", err)
			}
			// 長さは中身の誤りではない。ErrInvalidInput に畳むと、画面が「短くする」
			// と案内できない。
			if errors.Is(err, usecase.ErrInvalidInput) {
				t.Error("長すぎることを ErrInvalidInput として返している")
			}
			if boards.writes != 0 {
				t.Errorf("弾いたのに書き込んでいる: writes = %d", boards.writes)
			}
		})
	}
}

// 編集を始めた時点の背景と食い違えば書かない。黙って上書きすると、先に保存した
// 人の背景が消える（ADR 0093）。
func TestSetPremise_RejectsStaleBase(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	boards.board.Premise = "先に保存された背景"

	err := newBoardService(boards).SetPremise(t.Context(), "board-1", "", "古い本文から書いた背景")
	if !errors.Is(err, usecase.ErrPremiseConflict) {
		t.Fatalf("SetPremise() = %v, want ErrPremiseConflict", err)
	}
	// シーンの衝突とは打ち手が違う。混ぜると、画面が「キャンバスを読み直す」と
	// 案内してしまう。
	if errors.Is(err, usecase.ErrSceneConflict) {
		t.Error("背景の食い違いを ErrSceneConflict で返している")
	}
	if boards.board.Premise != "先に保存された背景" {
		t.Errorf("Premise = %q, want 先に保存された背景のまま", boards.board.Premise)
	}
}

// --- 解釈に添える材料（ADR 0093） ---

// newPremiseInterpretService は背景と記録を持たせた解釈を作る。
func newPremiseInterpretService(
	t *testing.T, premise string, mappings *fakeMappings, llm *fakeLLM,
) *usecase.InterpretationService {
	t.Helper()

	board := newBoard(interpretScene)
	board.Premise = premise
	return usecase.NewInterpretationService(
		&fakeBoards{board: board}, mappings, llm, newLimiter(), usecase.WithMaxAttempts(1))
}

// saveItems は 1 回の作成の記録を積む。確定した item として積む。
func saveItems(t *testing.T, m *fakeMappings, annotationID string, items ...port.SyncItem) {
	t.Helper()

	for i := range items {
		items[i].Confirmed = true
		items[i].Action = port.ActionCreated
		if items[i].LocalID == "" {
			items[i].LocalID = fmt.Sprintf("l%d", i+1)
		}
	}
	if _, err := m.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: annotationID, ContentHash: "h",
		CreatedAt: baseTime, Outcome: port.OutcomeComplete, Items: items,
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
}

// 背景は行を保ったまま、行頭に印を付けて渡す。印を付けるのは、何でも書ける
// 背景の行が etoki の見出しや一覧（「- p1 (epic) …」）と取り違えられないため。
func TestInterpret_PassesPremiseAsQuotedLines(t *testing.T) {
	t.Parallel()

	premise := "技術: Go と React\n\n- p9 (issue) 背景に書いた箇条"
	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := newPremiseInterpretService(t, premise, &fakeMappings{}, llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	text := llm.requests[0].Text

	for _, want := range []string{
		"このボードの背景（開発者が書いたもの。囲みの外の材料です）:\n" +
			"> 技術: Go と React\n" +
			">\n" +
			"> - p9 (issue) 背景に書いた箇条\n",
		// 出口の側（ADR 0093）。背景があるときだけ body に書かせる。
		"背景のうちその項目に着手するのに要ること",
		"背景に書かれていることから項目を作らないでください",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("メッセージに %q が無い:\n%s", want, text)
		}
	}
	// 背景の行は行頭に出てこない。出てくると、偽の LLM や本物が前回ぶんの
	// 一覧と読み違える。
	if regexp.MustCompile(`(?m)^- p9 `).MatchString(text) {
		t.Errorf("背景の行が印なしで行頭に出ている:\n%s", text)
	}

	if result.Context.Premise != premise {
		t.Errorf("Context.Premise = %q, want %q", result.Context.Premise, premise)
	}
}

// 背景が無ければ節ごと省き、body の指示も出さない。空の節を見せると、モデルが
// 「あるはずのもの」を埋め合わせ始める。
func TestInterpret_OmitsPremiseSectionWhenEmpty(t *testing.T) {
	t.Parallel()

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := newPremiseInterpretService(t, "", &fakeMappings{}, llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	text := llm.requests[0].Text

	for _, unwanted := range []string{"このボードの背景", "着手するのに要ること"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("背景が無いのにメッセージに %q がある:\n%s", unwanted, text)
		}
	}
	if result.Context.Premise != "" {
		t.Errorf("Context.Premise = %q, want 空", result.Context.Premise)
	}
}

// 同じボードの別の絵から作ったものを、種別とタイトルだけ渡す（ADR 0093）。
//
//   - この囲みのぶんは前回ぶんの一覧にだけ出し、別の絵の一覧には混ぜない
//   - シーンから消えた注釈（annot-9）のぶんも含める
//   - 届いたか分からない書き込みは入れない（畳み込みと同じ、ADR 0056）
//   - ref を振らない（対応づけの候補にしない、ADR 0026）
//   - 最近作った・更新したものから並べる
func TestInterpret_PassesOtherFramesItems(t *testing.T) {
	t.Parallel()

	m := &fakeMappings{}
	saveItems(t, m, "annot-1", port.SyncItem{ItemID: "PVTI_mine", Kind: port.KindIssue, Title: "この囲みのもの"})
	saveItems(t, m, "annot-2", port.SyncItem{ItemID: "PVTI_a", Kind: port.KindEpic, Title: "認証の整備"})
	saveItems(t, m, "annot-9", port.SyncItem{ItemID: "PVTI_gone", Kind: port.KindIssue, Title: "消えた絵のもの"})
	// 認証の整備をあとで更新した。最新の記録になるので先頭に来る。
	saveItems(t, m, "annot-2", port.SyncItem{ItemID: "PVTI_a", Kind: port.KindEpic, Title: "認証まわりの整備"})
	// 届いたか分からない作成。在るとは言えないので渡さない。
	if _, err := m.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-2", ContentHash: "h",
		CreatedAt: baseTime, Outcome: port.OutcomeIncomplete, Error: "boom",
		Items: []port.SyncItem{{Kind: port.KindIssue, Title: "届いたか分からないもの", LocalID: "x"}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := newPremiseInterpretService(t, "", m, llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	text := llm.requests[0].Text

	want := []usecase.OtherItem{
		{Kind: domain.KindEpic, Title: "認証まわりの整備"},
		{Kind: domain.KindIssue, Title: "消えた絵のもの"},
	}
	if !slices.Equal(result.Context.OtherItems, want) {
		t.Errorf("Context.OtherItems = %+v, want %+v", result.Context.OtherItems, want)
	}
	if result.Context.OmittedOtherItems != 0 {
		t.Errorf("OmittedOtherItems = %d, want 0", result.Context.OmittedOtherItems)
	}

	section := "同じボードの別の絵からすでに作ったもの（参考。対応づけの対象ではありません）:\n" +
		"- (epic) 認証まわりの整備\n" +
		"- (issue) 消えた絵のもの\n"
	if !strings.Contains(text, section) {
		t.Errorf("別の絵の一覧が期待どおりでない:\n%s", text)
	}
	// この囲みのぶんは ref つきの前回一覧に出る。別の絵の一覧には出ない。
	if !strings.Contains(text, "- p1 (issue) この囲みのもの") {
		t.Errorf("この囲みのぶんが前回一覧に無い:\n%s", text)
	}
	if strings.Contains(text, "- (issue) この囲みのもの") {
		t.Errorf("この囲みのぶんが別の絵の一覧に混ざっている:\n%s", text)
	}
	for _, unwanted := range []string{"届いたか分からないもの", "認証の整備\n"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("渡さないはずの %q がメッセージにある:\n%s", unwanted, text)
		}
	}
	if !strings.Contains(text, "項目を外さずに出力し") {
		t.Errorf("重なっても落とさない指示が無い:\n%s", text)
	}
}

// 上限を超えたら最近のものから残し、渡さなかった件数を結果とメッセージに添える。
// 黙って打ち切ると、渡したつもりの材料が消えたことに気づけない（ADR 0093）。
func TestInterpret_CapsOtherItemsAndReportsOmitted(t *testing.T) {
	t.Parallel()

	m := &fakeMappings{}
	const extra = 3
	for i := range usecase.MaxOtherItems + extra {
		saveItems(t, m, "annot-2", port.SyncItem{
			ItemID: fmt.Sprintf("PVTI_%d", i), Kind: port.KindIssue, Title: fmt.Sprintf("項目 %d", i),
		})
	}

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := newPremiseInterpretService(t, "", m, llm)

	result, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	got := result.Context
	if len(got.OtherItems) != usecase.MaxOtherItems {
		t.Fatalf("len(OtherItems) = %d, want %d", len(got.OtherItems), usecase.MaxOtherItems)
	}
	if got.OmittedOtherItems != extra {
		t.Errorf("OmittedOtherItems = %d, want %d", got.OmittedOtherItems, extra)
	}
	// 最後に作ったものが先頭、最初に作った 3 件が落ちる。
	last := usecase.MaxOtherItems + extra - 1
	if got.OtherItems[0].Title != fmt.Sprintf("項目 %d", last) {
		t.Errorf("OtherItems[0] = %q, want 項目 %d", got.OtherItems[0].Title, last)
	}
	text := llm.requests[0].Text
	for i := range extra {
		if strings.Contains(text, fmt.Sprintf("- (issue) 項目 %d\n", i)) {
			t.Errorf("古いほうの 項目 %d が渡っている", i)
		}
	}
	if !strings.Contains(text, fmt.Sprintf("ほかに %d 件ありますが", extra)) {
		t.Errorf("打ち切ったことがメッセージに無い:\n%s", text)
	}
}

// 別の絵から何も作っていなければ節ごと省く。
func TestInterpret_OmitsOtherItemsSectionWhenNone(t *testing.T) {
	t.Parallel()

	m := &fakeMappings{}
	saveItems(t, m, "annot-1", port.SyncItem{ItemID: "PVTI_mine", Kind: port.KindIssue, Title: "この囲みのもの"})

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	svc := newPremiseInterpretService(t, "", m, llm)

	if _, err := interpret(t, svc, t.Context(), "board-1", "annot-1", nil); err != nil {
		t.Fatalf("interpret() = %v", err)
	}
	if text := llm.requests[0].Text; strings.Contains(text, "同じボードの別の絵") {
		t.Errorf("別の絵が無いのに節がある:\n%s", text)
	}
}

// --- 3 状態（合意したケース 1〜3）。背景はハッシュに入らない（ADR 0093） ---

// ケース 1・2: 作成済みの注釈は、背景を書いても書き換えても作成済みのまま。
func TestSetPremise_KeepsCreatedState(t *testing.T) {
	t.Parallel()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	m := &fakeMappings{}
	if _, err := m.SaveRun(t.Context(), port.SyncRun{
		BoardID: "board-1", AnnotationID: "annot-1", ContentHash: currentContentHash(t),
		CreatedAt: baseTime, Outcome: port.OutcomeComplete,
		Items: []port.SyncItem{{
			ItemID: "PVTI_1", Kind: port.KindIssue, Title: "作ったもの", LocalID: "i1",
			Action: port.ActionCreated, Confirmed: true,
		}},
	}); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	svc := usecase.NewBoardService(boards, m, usecase.NewBoardLocks())
	annotations := usecase.NewAnnotationService(boards, m)

	steps := []struct {
		name, base, premise string
	}{
		{name: "空から書いた", base: "", premise: "技術: Go"},
		{name: "別の文に書き換えた", base: "技術: Go", premise: "技術: Go と React"},
	}
	for _, step := range steps {
		if err := svc.SetPremise(t.Context(), "board-1", step.base, step.premise); err != nil {
			t.Fatalf("%s: SetPremise() = %v", step.name, err)
		}
		states, _, err := annotations.ListStates(t.Context(), "board-1")
		if err != nil {
			t.Fatalf("%s: ListStates() = %v", step.name, err)
		}
		if len(states) != 1 || states[0].State != domain.StateCreated {
			t.Errorf("%s: states = %+v, want annot-1 が created のまま", step.name, states)
		}
	}
}

// ケース 3: 解釈のあとで背景を変えても、解釈時点の contentHash で作成が通る
// （ADR 0010 の照合で止まらない）。照合が守るのは囲みの中身のほう。
func TestCreate_PassesAfterPremiseChanged(t *testing.T) {
	t.Parallel()

	board := newBoard(interpretScene)
	boards := &fakeBoards{board: board}
	m := &fakeMappings{}

	llm := &fakeLLM{responses: []string{validLLMOutput}}
	interpreted, err := interpret(t, usecase.NewInterpretationService(boards, m, llm, newLimiter()),
		t.Context(), "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("interpret() = %v", err)
	}

	if err := usecase.NewBoardService(boards, m, usecase.NewBoardLocks()).
		SetPremise(t.Context(), "board-1", "", "解釈のあとに書いた背景"); err != nil {
		t.Fatalf("SetPremise() = %v", err)
	}

	gh := &fakeGitHub{fields: projectFields()}
	if _, err := usecase.NewCreationService(boards, m, gh, usecase.NewBoardLocks()).
		Create(t.Context(), "board-1", "annot-1", interpreted.ContentHash, interpreted.Interpretation); err != nil {
		t.Fatalf("背景を変えたあとの Create() = %v, want 通る", err)
	}
}

// --- ジョブの鍵（合意したケース 4〜7） ---

// newPremiseJobs は記録も持たせたジョブを作る。ボードの持ち主は操作者 "u1"。
func newPremiseJobs(
	t *testing.T, llm port.LLMClient, m *fakeMappings,
) (*usecase.InterpretationJobs, *fakeBoards) {
	t.Helper()

	boards := &fakeBoards{board: newBoard(interpretScene), owner: "u1"}
	svc := usecase.NewInterpretationService(boards, m, llm, newLimiter(), usecase.WithMaxAttempts(1))
	return usecase.NewInterpretationJobs(svc), boards
}

// ケース 4〜6: 走っている最中に押し直したとき、同じジョブを返すかどうか。
//
// 違う入力のジョブは枠に弾かれる（同時実行の既定は 1）。ErrConcurrencyLimited が
// 返ることが「古い入力のジョブを返さなかった」印になる
// （TestJobs_StartWhileRunningComparesSavedInput と同じ見方）。
func TestJobs_PremiseIsPartOfTheKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func(t *testing.T, boards *fakeBoards, m *fakeMappings)
		sameJob bool
	}{
		{
			name:    "何も変えていない",
			change:  func(*testing.T, *fakeBoards, *fakeMappings) {},
			sameJob: true,
		},
		{
			name: "背景だけ変えた",
			change: func(_ *testing.T, boards *fakeBoards, _ *fakeMappings) {
				boards.board.Premise = "あとから書いた背景"
			},
		},
		// 別の絵の一覧は作成で変わる。前回ぶんと同じく鍵に入れない（ADR 0085）。
		{
			name: "別の絵から作成が増えた",
			change: func(t *testing.T, _ *fakeBoards, m *fakeMappings) {
				saveItems(t, m, "annot-2", port.SyncItem{ItemID: "PVTI_new", Kind: port.KindIssue, Title: "新しく作ったもの"})
			},
			sameJob: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			llm := newGatedLLM(validLLMOutput)
			m := &fakeMappings{}
			jobs, boards := newPremiseJobs(t, llm, m)
			boards.board.Premise = "最初の背景"
			ctx := asUser(t.Context(), "u1")

			first, err := jobs.Start(ctx, "board-1", "annot-1", nil)
			if err != nil {
				t.Fatalf("Start() = %v", err)
			}

			tt.change(t, boards, m)

			second, err := jobs.Start(ctx, "board-1", "annot-1", nil)
			if tt.sameJob {
				if err != nil || second.ID != first.ID {
					t.Errorf("Start() = (%q, %v), want 同じジョブ %q", second.ID, err, first.ID)
				}
			} else if !errors.Is(err, usecase.ErrConcurrencyLimited) {
				t.Errorf("Start() = (%q, %v), want ErrConcurrencyLimited（古い背景のジョブを返さない）",
					second.ID, err)
			}

			close(llm.release)
			waitJob(t, jobs, ctx, first.ID)
		})
	}
}

// ケース 7: 走っている途中で背景を保存しても、そのジョブは始めたときの背景で
// 読み、結果に載る背景もそちら。結果に載る背景が「実際に渡したもの」でないと、
// 見せている意味が無い（中核思想 3）。
func TestJobs_ReadsPremiseAsOfStart(t *testing.T) {
	t.Parallel()

	llm := newGatedLLM(validLLMOutput)
	jobs, boards := newPremiseJobs(t, llm, &fakeMappings{})
	boards.board.Premise = "始めたときの背景"
	ctx := asUser(t.Context(), "u1")

	job, err := jobs.Start(ctx, "board-1", "annot-1", nil)
	if err != nil {
		t.Fatalf("Start() = %v", err)
	}
	<-llm.started

	if err := usecase.NewBoardService(boards, &fakeMappings{}, usecase.NewBoardLocks()).
		SetPremise(asUser(t.Context(), "u1"), "board-1", "始めたときの背景", "途中で書き換えた背景"); err != nil {
		t.Fatalf("SetPremise() = %v", err)
	}

	close(llm.release)
	done := waitJob(t, jobs, ctx, job.ID)
	if done.Result == nil {
		t.Fatalf("done = %+v, want 結果あり", done)
	}
	if got := done.Result.Context.Premise; got != "始めたときの背景" {
		t.Errorf("Context.Premise = %q, want 始めたときの背景", got)
	}
	texts := llm.requestTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "> 始めたときの背景") ||
		strings.Contains(texts[0], "途中で書き換えた背景") {
		t.Errorf("LLM に渡した背景が始めたときのものでない: %q", texts)
	}
}
