package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

var createdAt = time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

// githubCall は GitHub への 1 回の操作。順序を確かめるために記録する。
type githubCall struct {
	op       string // "create" | "field"
	title    string
	itemID   string
	fieldID  string
	text     string
	optionID string
}

// fakeGitHub は作成操作を記録する GitHubClient。
type fakeGitHub struct {
	fields []port.ProjectField
	calls  []githubCall
	// failOnTitle が空でなければ、その draft issue の作成で失敗する。
	failOnTitle string
	// listErr が非 nil なら ListProjectFields が失敗する。
	listErr error
	seq     int
	// repos と projects は作成先の候補一覧が返すもの。
	repos    []port.Repository
	projects []port.Project
	// projectIDs は呼び出しごとに渡された作成先。ボードの Project が
	// 使われていることを確かめる。
	projectIDs []string
}

func (f *fakeGitHub) ListRepositories(context.Context) ([]port.Repository, error) {
	return f.repos, nil
}

func (f *fakeGitHub) ListRepositoryProjects(context.Context, string, string) ([]port.Project, error) {
	return f.projects, nil
}

func (f *fakeGitHub) ListProjectFields(_ context.Context, projectID string) ([]port.ProjectField, error) {
	f.projectIDs = append(f.projectIDs, projectID)
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.fields, nil
}

func (f *fakeGitHub) CreateDraftIssue(_ context.Context, projectID string, item port.DraftIssue) (string, error) {
	f.projectIDs = append(f.projectIDs, projectID)
	if f.failOnTitle != "" && item.Title == f.failOnTitle {
		return "", errors.New("github: boom")
	}
	f.seq++
	id := "PVTI_" + string(rune('a'+f.seq-1))
	f.calls = append(f.calls, githubCall{op: "create", title: item.Title, itemID: id})
	return id, nil
}

func (f *fakeGitHub) SetItemFieldValue(_ context.Context, projectID, itemID string, v port.FieldValue) error {
	f.projectIDs = append(f.projectIDs, projectID)
	call := githubCall{op: "field", itemID: itemID, fieldID: v.FieldID}
	if v.Text != nil {
		call.text = *v.Text
	}
	if v.OptionID != nil {
		call.optionID = *v.OptionID
	}
	f.calls = append(f.calls, call)
	return nil
}

// fakeMappings は保存された run を覚えておく MappingRepository。
type fakeMappings struct {
	runs    []port.SyncRun
	saveErr error
}

func (f *fakeMappings) SaveRun(_ context.Context, run port.SyncRun) (int64, error) {
	if f.saveErr != nil {
		return 0, f.saveErr
	}
	f.runs = append(f.runs, run)
	return int64(len(f.runs)), nil
}

func (f *fakeMappings) FindLatestRun(context.Context, string, string) (*port.SyncRun, error) {
	return nil, nil
}

func (f *fakeMappings) ListLatestRunsByBoard(context.Context, string) ([]port.SyncRun, error) {
	return f.runs, nil
}

// projectFields は etoki が必要とするフィールドが揃った状態。
func projectFields() []port.ProjectField {
	return []port.ProjectField{
		{ID: "F_title", Name: "Title", DataType: "TITLE"},
		{ID: "F_kind", Name: "Kind", DataType: "SINGLE_SELECT", Options: []port.ProjectFieldOption{
			{ID: "O_epic", Name: "epic"},
			{ID: "O_issue", Name: "issue"},
		}},
		{ID: "F_parent", Name: "Parent", DataType: "TEXT"},
	}
}

func interpretation() domain.Interpretation {
	parent := "e1"
	return domain.Interpretation{
		Summary: "決済まわりの課題出し",
		Items: []domain.InterpretedItem{
			// issue を先に並べても epic が先に作られることを確かめたい。
			{LocalID: "i1", Kind: domain.KindIssue, Title: "Stripe SDK の更新", ParentLocalID: &parent},
			{LocalID: "e1", Kind: domain.KindEpic, Title: "決済フローの見直し", Body: "全体の方針"},
			{LocalID: "i2", Kind: domain.KindIssue, Title: "返金導線の整理"},
		},
	}
}

func currentContentHash(t *testing.T) string {
	t.Helper()

	scene, err := domain.ParseScene([]byte(interpretScene))
	if err != nil {
		t.Fatalf("ParseScene: %v", err)
	}
	return string(scene.AnnotationHash(scene.Annotations()[0]))
}

func newCreationService(t *testing.T, gh *fakeGitHub, mappings *fakeMappings) *usecase.CreationService {
	t.Helper()

	boards := &fakeBoards{board: newBoard(interpretScene)}
	return usecase.NewCreationService(boards, mappings, gh,
		usecase.WithCreationClock(func() time.Time { return createdAt }))
}

func TestCreate_CreatesEpicsBeforeIssues(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	run, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation())
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	// issue の親フィールドに epic のタイトルを入れるので、epic が先に確定して
	// いないと値を決められない。
	var titles []string
	for _, c := range gh.calls {
		if c.op == "create" {
			titles = append(titles, c.title)
		}
	}
	want := []string{"決済フローの見直し", "Stripe SDK の更新", "返金導線の整理"}
	if strings.Join(titles, "|") != strings.Join(want, "|") {
		t.Errorf("作成順 = %v, want %v", titles, want)
	}

	if len(run.Items) != 3 {
		t.Fatalf("len(run.Items) = %d, want 3", len(run.Items))
	}
	if run.ID == 0 {
		t.Error("run.ID が発番されていない")
	}
}

// summary は GitHub には作らない。作成前の確認表示にだけ使う（ADR 0006）。
func TestCreate_DoesNotCreateSummary(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	svc := newCreationService(t, gh, &fakeMappings{})

	in := interpretation()
	if _, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), in); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	for _, c := range gh.calls {
		if c.op == "create" && c.title == in.Summary {
			t.Errorf("summary が draft issue として作られている: %q", c.title)
		}
	}

	// 作られるのは items の件数ちょうど。まとめノードは増えない。
	created := 0
	for _, c := range gh.calls {
		if c.op == "create" {
			created++
		}
	}
	if created != len(in.Items) {
		t.Errorf("作成件数 = %d, want %d", created, len(in.Items))
	}
}

// 親は epic のタイトルで指す。draft issue は native な親子関係を持てない（ADR 0006）。
func TestCreate_SetsKindAndParent(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	svc := newCreationService(t, gh, &fakeMappings{})

	if _, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation()); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	// epic は PVTI_a、以降 PVTI_b（i1）、PVTI_c（i2）。
	byItem := map[string][]githubCall{}
	for _, c := range gh.calls {
		if c.op == "field" {
			byItem[c.itemID] = append(byItem[c.itemID], c)
		}
	}

	if got := byItem["PVTI_a"]; len(got) != 1 || got[0].optionID != "O_epic" {
		t.Errorf("epic のフィールド設定 = %+v", got)
	}

	// i1 は種別と親の 2 回。
	i1 := byItem["PVTI_b"]
	if len(i1) != 2 {
		t.Fatalf("issue のフィールド設定 = %d 回, want 2 (%+v)", len(i1), i1)
	}
	if i1[0].optionID != "O_issue" {
		t.Errorf("issue の種別 = %q, want O_issue", i1[0].optionID)
	}
	if i1[1].fieldID != "F_parent" || i1[1].text != "決済フローの見直し" {
		t.Errorf("親の設定 = %+v, want F_parent に epic のタイトル", i1[1])
	}

	// 親のない issue には親を設定しない。
	if got := byItem["PVTI_c"]; len(got) != 1 {
		t.Errorf("親のない issue のフィールド設定 = %d 回, want 1 (%+v)", len(got), got)
	}
}

func TestCreate_RecordsRunWithCurrentHash(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	run, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation())
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}

	if len(mappings.runs) != 1 {
		t.Fatalf("保存された run = %d 件, want 1", len(mappings.runs))
	}
	saved := mappings.runs[0]

	// 状態判定の基準は保存済みシーン。実行時点のハッシュを記録する。
	scene, err := domain.ParseScene([]byte(interpretScene))
	if err != nil {
		t.Fatalf("ParseScene: %v", err)
	}
	annotations := scene.Annotations()
	want := string(scene.AnnotationHash(annotations[0]))

	if saved.ContentHash != want {
		t.Errorf("ContentHash = %q, want %q", saved.ContentHash, want)
	}
	if saved.BoardID != "board-1" || saved.AnnotationID != "annot-1" {
		t.Errorf("run = %+v", saved)
	}
	if !saved.CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt = %v, want %v", saved.CreatedAt, createdAt)
	}
	if run.Items[0].Kind != port.KindEpic {
		t.Errorf("Items[0].Kind = %q, want epic", run.Items[0].Kind)
	}
}

func TestCreate_RejectsMismatchedContentHashBeforeGitHub(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	_, err := svc.Create(t.Context(), "board-1", "annot-1", "stale-hash", interpretation())
	if !errors.Is(err, usecase.ErrContentHashMismatch) {
		t.Fatalf("Create() = %v, want ErrContentHashMismatch", err)
	}

	if len(gh.calls) != 0 {
		t.Errorf("hash が食い違っているのに GitHub を呼んでいる: %+v", gh.calls)
	}
	if len(mappings.runs) != 0 {
		t.Errorf("hash が食い違っているのに run が記録されている: %+v", mappings.runs)
	}
}

// 任意にすると API を直接叩く経路で検証を素通りできる（ADR 0010）。
func TestCreate_RequiresContentHash(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	_, err := svc.Create(t.Context(), "board-1", "annot-1", "", interpretation())
	if !errors.Is(err, usecase.ErrInvalidInput) {
		t.Fatalf("Create() = %v, want ErrInvalidInput", err)
	}

	if len(gh.calls) != 0 {
		t.Errorf("contentHash が無いのに GitHub を呼んでいる: %+v", gh.calls)
	}
	if len(mappings.runs) != 0 {
		t.Errorf("contentHash が無いのに run が記録されている: %+v", mappings.runs)
	}
}

// GitHub 側の draft issue は削除しない。記録しないと追跡不能になる（ADR 0009）。
func TestCreate_RecordsPartialRunOnFailure(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields(), failOnTitle: "返金導線の整理"}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	run, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation())
	if !errors.Is(err, usecase.ErrCreationIncomplete) {
		t.Fatalf("Create() = %v, want ErrCreationIncomplete", err)
	}

	// 作れた分は返す。呼び出し側が何ができたのか示せるように。
	if run == nil {
		t.Fatal("run = nil, want 作成済みの記録")
	}
	if len(run.Items) != 2 {
		t.Errorf("len(run.Items) = %d, want 2", len(run.Items))
	}

	if len(mappings.runs) != 1 {
		t.Fatalf("保存された run = %d 件, want 1", len(mappings.runs))
	}
	if len(mappings.runs[0].Items) != 2 {
		t.Errorf("保存された Items = %d 件, want 2", len(mappings.runs[0].Items))
	}
}

// 空の run を残すと、状態が created に変わって「作成済み」に見えてしまう。
func TestCreate_DoesNotRecordWhenNothingCreated(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields(), failOnTitle: "決済フローの見直し"}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	if _, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation()); err == nil {
		t.Fatal("Create() = nil, want error")
	}
	if len(mappings.runs) != 0 {
		t.Errorf("1 件も作れていないのに run が記録されている: %+v", mappings.runs)
	}
}

// 作成には成功したのに記録できなかった状態。GitHub には draft issue が残るのに
// etoki は何も知らないので、手で追える手掛かりがエラーに要る。
func TestCreate_ReportsCreatedItemsWhenSaveFails(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: projectFields()}
	mappings := &fakeMappings{saveErr: errors.New("db: boom")}
	svc := newCreationService(t, gh, mappings)

	_, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation())
	if err == nil {
		t.Fatal("Create() = nil, want error")
	}
	// 作った 3 件すべてが分からないと、どれが残っているのか特定できない。
	for _, id := range []string{"PVTI_a", "PVTI_b", "PVTI_c"} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("作成済みの item ID %s が示されていない: %v", id, err)
		}
	}
}

// フィールドを引けない時点で止める。種別も親子も無い draft issue を作っても、
// 開発者が手で直すしかなくなる。
func TestCreate_StopsWhenFieldsCannotBeListed(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{listErr: errors.New("github: boom")}
	mappings := &fakeMappings{}
	svc := newCreationService(t, gh, mappings)

	if _, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation()); err == nil {
		t.Fatal("Create() = nil, want error")
	}
	if len(gh.calls) != 0 {
		t.Errorf("フィールドを引けていないのに作成している: %+v", gh.calls)
	}
	if len(mappings.runs) != 0 {
		t.Errorf("フィールドを引けていないのに run を記録している: %+v", mappings.runs)
	}
}

// 黙って作成を続けると、種別も親子も無い draft issue が並ぶだけになる。
func TestCreate_RequiresProjectFields(t *testing.T) {
	t.Parallel()

	tests := map[string][]port.ProjectField{
		"種別フィールドが無い": {
			{ID: "F_parent", Name: "Parent", DataType: "TEXT"},
		},
		"親フィールドが無い": {
			{ID: "F_kind", Name: "Kind", DataType: "SINGLE_SELECT", Options: []port.ProjectFieldOption{
				{ID: "O_epic", Name: "epic"}, {ID: "O_issue", Name: "issue"},
			}},
		},
		"選択肢が足りない": {
			{ID: "F_kind", Name: "Kind", DataType: "SINGLE_SELECT", Options: []port.ProjectFieldOption{
				{ID: "O_epic", Name: "epic"},
			}},
			{ID: "F_parent", Name: "Parent", DataType: "TEXT"},
		},
	}

	for name, fields := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{fields: fields}
			mappings := &fakeMappings{}
			svc := newCreationService(t, gh, mappings)

			_, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation())
			if !errors.Is(err, usecase.ErrProjectFieldMissing) {
				t.Fatalf("Create() = %v, want ErrProjectFieldMissing", err)
			}
			// 何を作ればよいのか分かるメッセージであること。
			if !strings.Contains(err.Error(), "Kind") && !strings.Contains(err.Error(), "Parent") {
				t.Errorf("不足しているフィールド名が示されていない: %v", err)
			}
			if len(gh.calls) != 0 {
				t.Error("フィールドが揃っていないのに作成している")
			}
			if len(mappings.runs) != 0 {
				t.Error("フィールドが揃っていないのに run を記録している")
			}
		})
	}
}

// フロントを経由しない呼び出しでも 2 階層の制約が守られる必要がある。
func TestCreate_RevalidatesInterpretation(t *testing.T) {
	t.Parallel()

	tests := map[string]domain.Interpretation{
		"summary が空": {
			Items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "t"},
			},
		},
		"epic が親を持つ": {
			Summary: "s",
			Items: []domain.InterpretedItem{
				{LocalID: "e1", Kind: domain.KindEpic, Title: "t1"},
				{LocalID: "e2", Kind: domain.KindEpic, Title: "t2", ParentLocalID: ptrTo("e1")},
			},
		},
		"項目が無い": {Summary: "s"},
	}

	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			gh := &fakeGitHub{fields: projectFields()}
			mappings := &fakeMappings{}
			svc := newCreationService(t, gh, mappings)

			_, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), in)
			if !errors.Is(err, usecase.ErrInvalidInput) {
				t.Fatalf("Create() = %v, want ErrInvalidInput", err)
			}
			if len(gh.calls) != 0 {
				t.Error("検証を通っていないのに作成している")
			}
		})
	}
}

func TestCreate_NotFound(t *testing.T) {
	t.Parallel()

	t.Run("ボードが無い", func(t *testing.T) {
		t.Parallel()

		gh := &fakeGitHub{fields: projectFields()}
		svc := newCreationService(t, gh, &fakeMappings{})

		_, err := svc.Create(t.Context(), "no-such-board", "annot-1", currentContentHash(t), interpretation())
		if !errors.Is(err, usecase.ErrBoardNotFound) {
			t.Fatalf("Create() = %v, want ErrBoardNotFound", err)
		}
	})

	t.Run("注釈が無い", func(t *testing.T) {
		t.Parallel()

		gh := &fakeGitHub{fields: projectFields()}
		svc := newCreationService(t, gh, &fakeMappings{})

		_, err := svc.Create(t.Context(), "board-1", "no-such-annot", currentContentHash(t), interpretation())
		if !errors.Is(err, usecase.ErrAnnotationNotFound) {
			t.Fatalf("Create() = %v, want ErrAnnotationNotFound", err)
		}
	})
}

// フィールド名は GitHub 側の都合に合わせられる必要がある。
func TestCreate_UsesConfiguredFieldNames(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{fields: []port.ProjectField{
		{ID: "F_type", Name: "種別", DataType: "SINGLE_SELECT", Options: []port.ProjectFieldOption{
			{ID: "O_e", Name: "Epic"}, {ID: "O_i", Name: "ISSUE"},
		}},
		{ID: "F_oya", Name: "親", DataType: "TEXT"},
	}}

	boards := &fakeBoards{board: newBoard(interpretScene)}
	svc := usecase.NewCreationService(boards, &fakeMappings{}, gh,
		usecase.WithFieldNames("種別", "親"),
		usecase.WithCreationClock(func() time.Time { return createdAt }))

	if _, err := svc.Create(t.Context(), "board-1", "annot-1", currentContentHash(t), interpretation()); err != nil {
		t.Fatalf("Create() = %v", err)
	}

	const epicTitle = "決済フローの見直し"

	// フィールドの操作は itemID しか持たないので、どの item かを引けるようにする。
	titles := make(map[string]string)
	for _, c := range gh.calls {
		if c.op == "create" {
			titles[c.itemID] = c.title
		}
	}

	// 種別と親の両方が設定した名前で解決されていること。片方しか見ないと、
	// もう片方が既定の名前のままでも気付けない。
	kinds, parents := 0, 0
	for _, c := range gh.calls {
		switch {
		case c.op == "field" && c.fieldID == "F_type":
			// 選択肢名の大文字小文字は揃わないことがある。全部 epic になって
			// いても件数は合うので、item ごとに期待する選択肢を照らす。
			want := "O_i"
			if titles[c.itemID] == epicTitle {
				want = "O_e"
			}
			if c.optionID != want {
				t.Errorf("%q の種別 = %q, want %q", titles[c.itemID], c.optionID, want)
			}
			kinds++
		case c.op == "field" && c.fieldID == "F_oya":
			if c.text != epicTitle {
				t.Errorf("親の値 = %q, want epic のタイトル", c.text)
			}
			parents++
		}
	}
	if kinds != 3 || parents != 1 {
		t.Errorf("設定したフィールド名が使われていない: 種別 %d 回, 親 %d 回", kinds, parents)
	}
}

func ptrTo[T any](v T) *T { return &v }
