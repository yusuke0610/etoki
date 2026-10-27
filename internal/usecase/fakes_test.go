package usecase_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// このファイルは、usecase のテストが共有するフェイクと入力を置く場所。
// 対象に固有のフェイク（blockingMembers や racingBoards など）は、使うテストの
// ファイルに置いたままにする。

var baseTime = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

// newBoard は作成先を設定済みのボードを返す。
//
// 未選択のボードには draft issue を作れない（ADR 0014）。作成先の有無そのものを
// 見るテストだけが、ここから Target を落として使う。
func newBoard(scene string) *port.Board {
	return &port.Board{
		ID:    "board-1",
		Name:  "設計会",
		Scene: scene,
		// 保存は基準にした版を伴う（ADR 0020）。ゼロ値のままにすると、保存を
		// 通したいテストが「基準が無い」で 400 に落ちる。
		UpdatedAt: baseTime,
		Target:    port.BoardTarget{RepositoryOwner: "acme", RepositoryName: "web", ProjectID: "PVT_1"},
	}
}

const interpretScene = `{"type":"excalidraw","elements":[
	{"id":"annot-1","type":"frame","name":"決済まわり","customData":{"etoki":{"granularity":""}}},
	{"id":"t1","type":"text","text":"Stripe の SDK が古い","frameId":"annot-1"},
	{"id":"t2","type":"text","text":"返金導線が無い","frameId":"annot-1"},
	{"id":"t9","type":"text","text":"囲みの外のメモ"}]}`

const validLLMOutput = `{"summary":"決済まわりの課題出し。SDK 更新と返金導線の 2 件。","items":[
	{"localId":"e1","kind":"epic","title":"決済まわりの整備","body":"","parentLocalId":null},
	{"localId":"i1","kind":"issue","title":"Stripe SDK の更新","body":"","parentLocalId":"e1"}]}`

// fakeBoards はメンバーの絞り方と版の照合まで真似るボードリポジトリ。
//
// 書き込み系が呼ばれたら数える。解釈は読むだけで何も残さない、という約束を
// テストから確かめるため。
type fakeBoards struct {
	board *port.Board
	// owner はこのボードのメンバーである操作者。既定は空文字（認証なし）。
	owner string
	// role は操作者のロール。空なら owner として扱う。ロール不足を確かめる
	// テストだけがここを変える。
	role    port.BoardRole
	members []port.BoardMember
	writes  int
	// display は最後に書かれた表示用スナップショット。
	display port.BoardTargetDisplay
	// files はボードが持っている画像（ADR 0074）。ID → 中身。
	files map[string]string
	// saved は最後に書かれた保存。何を足して何を参照として渡したかを見る。
	saved *port.SceneWrite
}

// Find は操作者も突き合わせる。実装と同じ形にしておかないと、絞り忘れを
// フェイクが吸収してしまう（ADR 0016 / 0017）。
func (f *fakeBoards) Find(_ context.Context, actor, id string) (*port.BoardAccess, error) {
	if f.board == nil || f.board.ID != id || f.owner != actor {
		return nil, nil
	}

	role := f.role
	if role == "" {
		role = port.RoleOwner
	}
	return &port.BoardAccess{Board: *f.board, Role: role}, nil
}

func (f *fakeBoards) Create(context.Context, port.Board, string) error {
	f.writes++
	return nil
}

// UpdateScene は版の照合まで真似る。素通しにすると、ユースケースが基準を
// 渡し忘れても緑のままになる（ADR 0020）。
//
// 画像も実装と同じく、照合に通ったときだけ足して、参照から外れたものを消す
// （ADR 0074）。返す ID は昇順。
func (f *fakeBoards) UpdateScene(
	_ context.Context, _, _ string, w port.SceneWrite, base, updatedAt time.Time,
) ([]string, error) {
	if f.board != nil && !base.Equal(f.board.UpdatedAt) {
		return nil, port.ErrConflict
	}
	f.writes++
	f.saved = &w
	if f.board != nil {
		f.board.Scene = w.Scene
		f.board.UpdatedAt = updatedAt
	}

	if f.files == nil {
		f.files = map[string]string{}
	}
	for _, file := range w.Added {
		f.files[file.ID] = file.Data
	}
	for id := range f.files {
		if !slices.Contains(w.Referenced, id) {
			delete(f.files, id)
		}
	}
	return slices.Sorted(maps.Keys(f.files)), nil
}

// FindWithFiles は Find と同じ絞り方で、画像も返す。
func (f *fakeBoards) FindWithFiles(
	ctx context.Context, actor, id string,
) (*port.BoardAccess, []port.BoardFile, error) {
	a, err := f.Find(ctx, actor, id)
	if a == nil || err != nil {
		return nil, nil, err
	}

	files := []port.BoardFile{}
	for _, fileID := range slices.Sorted(maps.Keys(f.files)) {
		files = append(files, port.BoardFile{ID: fileID, Data: f.files[fileID]})
	}
	return a, files, nil
}

// FileSizes はボードの画像の大きさを返す。別のボードの ID では空を返す
// （実装と同じく boardID で絞る）。
func (f *fakeBoards) FileSizes(_ context.Context, boardID string) (map[string]int64, error) {
	sizes := map[string]int64{}
	if f.board == nil || f.board.ID != boardID {
		return sizes, nil
	}
	for id, data := range f.files {
		sizes[id] = int64(len(data))
	}
	return sizes, nil
}

// UpdateName は名前だけを書く。**更新時刻は動かさない。** 実装と揃えて
// おかないと、版を進める実装に変わってもテストが素通しする（ADR 0020）。
func (f *fakeBoards) UpdateName(_ context.Context, _, _, name string) error {
	f.writes++
	if f.board != nil {
		f.board.Name = name
	}
	return nil
}

func (f *fakeBoards) UpdateTarget(context.Context, string, string, port.BoardTarget) error {
	f.writes++
	return nil
}

// UpdateTargetDisplay は表示用の 3 つだけを書く。作成先にも版にも触らない
// （ADR 0037 / 0092）。実装と同じ形にしておかないと、触ったことをフェイクが吸収する。
func (f *fakeBoards) UpdateTargetDisplay(
	_ context.Context, _, _ string, d port.BoardTargetDisplay,
) error {
	f.writes++
	f.display = d
	if f.board != nil {
		f.board.Target.ProjectNumber = d.ProjectNumber
		f.board.Target.ProjectTitle = d.ProjectTitle
		f.board.Target.ProjectURL = d.ProjectURL
	}
	return nil
}

// Delete は実装と同じく操作者も突き合わせる。素通しにすると、絞り忘れを
// フェイクが吸収する（ADR 0016）。消えたことは board を nil にして表す。
func (f *fakeBoards) Delete(_ context.Context, actor, id string) error {
	if f.board == nil || f.board.ID != id || f.owner != actor {
		return port.ErrNotFound
	}
	f.writes++
	f.board = nil
	return nil
}

func (f *fakeBoards) List(context.Context, string) ([]port.BoardAccess, error) { return nil, nil }

func (f *fakeBoards) ListMembers(_ context.Context, boardID string) ([]port.BoardMember, error) {
	if f.board == nil || f.board.ID != boardID {
		return nil, nil
	}
	return f.members, nil
}

func (f *fakeBoards) AddMember(_ context.Context, m port.BoardMember) error {
	for _, existing := range f.members {
		if existing.UserID == m.UserID {
			return port.ErrAlreadyExists
		}
	}
	f.writes++
	f.members = append(f.members, m)
	return nil
}

func (f *fakeBoards) UpdateMemberRole(
	_ context.Context, _, userID string, role port.BoardRole,
) error {
	for i, m := range f.members {
		if m.UserID == userID {
			f.writes++
			f.members[i].Role = role
			return nil
		}
	}
	return port.ErrNotFound
}

func (f *fakeBoards) RemoveMember(_ context.Context, _, userID string) error {
	for i, m := range f.members {
		if m.UserID == userID {
			f.writes++
			f.members = append(f.members[:i], f.members[i+1:]...)
			return nil
		}
	}
	return port.ErrNotFound
}

func (f *fakeBoards) CountUnowned(context.Context) (int, error) { return 0, nil }

func (f *fakeBoards) ClaimUnowned(context.Context, string) (int64, error) {
	f.writes++
	return 0, nil
}

// fakeLLM は決められた応答を順に返す LLMClient。
//
// 応答を使い切ったら最後のものを返し続ける。上限まで失敗し続ける場合を
// 書きやすくするため。
type fakeLLM struct {
	responses []string
	// usages は応答ごとに返すトークン数。空なら報告しない実装を真似る
	// （port.Usage は埋めるかどうかが任意）。
	usages   []port.Usage
	err      error
	requests []port.VisionRequest
}

func (f *fakeLLM) Complete(_ context.Context, req port.VisionRequest) (port.VisionResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return port.VisionResponse{}, f.err
	}

	i := min(len(f.requests)-1, len(f.responses)-1)
	resp := port.VisionResponse{Text: f.responses[i]}
	if len(f.usages) > 0 {
		resp.Usage = f.usages[min(len(f.requests)-1, len(f.usages)-1)]
	}
	return resp, nil
}

// githubCall は GitHub への 1 回の操作。順序を確かめるために記録する。
type githubCall struct {
	op       string // "create" | "field"
	title    string
	body     string
	itemID   string
	fieldID  string
	text     string
	optionID string
}

// fakeGitHub は作成操作を記録する GitHubClient。
type fakeGitHub struct {
	fields []port.ProjectField
	// canWrite は CanWriteProject が返す値。既定の false のままだと
	// 「書けない」になるので、書ける前提のテストは true を立てる。
	canWrite bool
	calls    []githubCall
	// failOnTitle が空でなければ、その draft issue の作成で失敗する。
	failOnTitle string
	// failOnField が空でなければ、そのフィールドの設定で失敗する。
	// draft issue は書けたが後続で失敗する並びを作るために使う。
	failOnField string
	// listErr が非 nil なら ListProjectFields が失敗する。
	listErr error
	// afterCreate が非 nil なら、draft issue を 1 件作った直後に呼ぶ。
	// GitHub 側では作れた直後にブラウザがタブを閉じた、という並びを作る。
	afterCreate func()
	// cancelDuring が非 nil なら、draft issue を作る呼び出しの途中で呼び、
	// そのあと実物の HTTP クライアントと同じく ctx が切れていれば失敗する。
	// GitHub が受理したのに応答が届かない並びを作る。
	cancelDuring func()
	// updatedDatabaseIDs は UpdateDraftIssue が item ごとに返す識別子。
	updatedDatabaseIDs map[string]int64
	// afterListFields が非 nil なら、フィールドを引いた直後に呼ぶ。作成に
	// 手を付ける手前でリクエストが切れた並びを作る（ADR 0051）。
	afterListFields func()
	// attemptedCreates は CreateDraftIssue を呼ばれた回数。**失敗したぶんも
	// 数える。** calls は成功したものしか積まないので、「こけた 1 件の先へ
	// 進んでいないこと」をそちらでは確かめられない。
	attemptedCreates int
	seq              int
	// repos と projects は作成先の候補一覧が返すもの。
	repos    []port.Repository
	projects []port.Project
	// truncated は候補を取り切らずに辿るのをやめたこと（ADR 0054）。
	truncated bool
	// projectIDs は呼び出しごとに渡された作成先。ボードの Project が
	// 使われていることを確かめる。
	projectIDs []string
}

// CanWriteProject は表示用の可否を返す。作成を弾く判定には使われない
// （ADR 0017）ので、ここが false でも Create は通る。
func (f *fakeGitHub) CanWriteProject(context.Context, string) (bool, error) {
	return f.canWrite, nil
}

func (f *fakeGitHub) ListRepositories(context.Context) (port.RepositoryList, error) {
	return port.RepositoryList{Repositories: f.repos, Truncated: f.truncated}, nil
}

func (f *fakeGitHub) ListRepositoryProjects(context.Context, string, string) ([]port.Project, error) {
	return f.projects, nil
}

func (f *fakeGitHub) ListProjectFields(_ context.Context, projectID string) ([]port.ProjectField, error) {
	f.projectIDs = append(f.projectIDs, projectID)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.afterListFields != nil {
		f.afterListFields()
	}
	return f.fields, nil
}

func (f *fakeGitHub) CreateDraftIssue(
	ctx context.Context, projectID string, item port.DraftIssue,
) (port.ProjectItemRef, error) {
	f.projectIDs = append(f.projectIDs, projectID)
	f.attemptedCreates++
	if f.cancelDuring != nil {
		f.cancelDuring()
		if err := ctx.Err(); err != nil {
			return port.ProjectItemRef{}, err
		}
	}
	if f.failOnTitle != "" && item.Title == f.failOnTitle {
		return port.ProjectItemRef{}, errors.New("github: boom")
	}
	f.seq++
	id := "PVTI_" + string(rune('a'+f.seq-1))
	f.calls = append(f.calls, githubCall{op: "create", title: item.Title, body: item.Body, itemID: id})
	if f.afterCreate != nil {
		f.afterCreate()
	}
	// 識別子は ID ごとに違う値にする。全部に同じ値を返すと、取り違えた
	// 詰め替えでも通る（.claude/rules/test-effectiveness.md）。
	return port.ProjectItemRef{ItemID: id, DatabaseID: createdDatabaseID(f.seq)}, nil
}

// createdDatabaseID は seq 件目に作った item の識別子。
func createdDatabaseID(seq int) int64 { return int64(1000 + seq) }

// UpdateDraftIssue は既存の draft issue を書き換えたことにする。
//
// projectID を取らない。更新は content の ID で行うので、GitHub 側も Project を
// 要求しない（ADR 0026）。
//
// 返す識別子は updatedDatabaseIDs に置いたもの。置いていなければ 0（引き直しで
// 取れなかった）。
func (f *fakeGitHub) UpdateDraftIssue(
	_ context.Context, itemID string, item port.DraftIssue,
) (port.ProjectItemRef, error) {
	if f.failOnTitle != "" && item.Title == f.failOnTitle {
		return port.ProjectItemRef{}, errors.New("github: boom")
	}
	f.calls = append(f.calls, githubCall{
		op: "update", itemID: itemID, title: item.Title, body: item.Body,
	})
	return port.ProjectItemRef{ItemID: itemID, DatabaseID: f.updatedDatabaseIDs[itemID]}, nil
}

func (f *fakeGitHub) SetItemFieldValue(_ context.Context, projectID, itemID string, v port.FieldValue) error {
	f.projectIDs = append(f.projectIDs, projectID)
	if f.failOnField != "" && v.FieldID == f.failOnField {
		return errors.New("github: boom")
	}
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

func (f *fakeMappings) SaveRun(ctx context.Context, run port.SyncRun) (int64, error) {
	if f.saveErr != nil {
		return 0, f.saveErr
	}
	// 実装（sqlite の BeginTx）と同じく、切れた ctx では書けない。ctx を
	// 見ないフェイクだと、記録に切れた ctx を渡す実装でも緑になる（#140）。
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// 実装と同じく採番する。ID を振らないと、履歴の並び（新しい順は id で
	// 決める）をフェイクの上で確かめられない。
	run.ID = int64(len(f.runs) + 1)
	f.runs = append(f.runs, run)
	return run.ID, nil
}

func (f *fakeMappings) FindLatestRun(context.Context, string, string) (*port.SyncRun, error) {
	return nil, nil
}

// 実装は board_id で絞る。フェイクが全部返すと、別ボードの run が固定判定を
// 誤らせても気づけない。
func (f *fakeMappings) ListLatestRunsByBoard(
	_ context.Context, boardID string,
) ([]port.SyncRun, error) {
	var runs []port.SyncRun
	for _, run := range f.runs {
		if run.BoardID == boardID {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

// ListRunsByAnnotation は畳まずに 1 回ずつを新しい順で返す。
//
// **実装と同じく board_id と注釈で絞り、id の降順にする。** 全部返すフェイクに
// すると、別の注釈の run が履歴に混ざる不具合を素通しする。
func (f *fakeMappings) ListRunsByAnnotation(
	_ context.Context, boardID, annotationID string, limit int,
) ([]port.SyncRun, error) {
	if limit <= 0 {
		return nil, nil
	}

	var runs []port.SyncRun
	for i := len(f.runs) - 1; i >= 0; i-- {
		run := f.runs[i]
		if run.BoardID != boardID || run.AnnotationID != annotationID {
			continue
		}
		runs = append(runs, run)
		if len(runs) == limit {
			break
		}
	}
	return runs, nil
}

// ListItemsByAnnotation は run 履歴を ItemID で畳んで返す（ADR 0026）。
//
// **実装と同じく畳む。** 最新 run の Items を返すフェイクにすると、更新の run の
// あとに取り残しが消える不具合をテストが素通しする。並びは最初に作られた順で、
// 更新しても動かさない。
//
// **実装と同じく未確定の書き込みを外す**（ADR 0056）。混ぜるフェイクにすると、
// 届いたか分からないものが「いま GitHub に在るもの」に紛れ、更新先の照合
// （checkPreviousItems）まで通ってしまう不具合を素通しする。
func (f *fakeMappings) ListItemsByAnnotation(
	_ context.Context, boardID, annotationID string,
) ([]port.SyncItem, error) {
	latest := map[string]port.SyncItem{}
	var order []string

	for _, run := range f.runs {
		if run.BoardID != boardID || run.AnnotationID != annotationID {
			continue
		}
		for _, it := range run.Items {
			if !it.Confirmed {
				continue
			}
			if _, seen := latest[it.ItemID]; !seen {
				order = append(order, it.ItemID)
			}
			latest[it.ItemID] = it
		}
	}

	items := make([]port.SyncItem, 0, len(order))
	for _, id := range order {
		items = append(items, latest[id])
	}
	return items, nil
}

// ListUnconfirmedItemsByBoard は届いたか分からない書き込みを注釈ごとに返す
// （ADR 0056）。
//
// **実装と同じく、あとで同じ item に確定の記録が付いたものは外す。** 常に全部
// 返すフェイクにすると、解けた不確かさが残り続ける不具合を素通しする。
func (f *fakeMappings) ListUnconfirmedItemsByBoard(
	_ context.Context, boardID string,
) (map[string][]port.SyncItem, error) {
	// あとから確定した (注釈, ItemID) を先に集める。ItemID を持たない未確定の
	// 作成は、確定させる手段が無いのでここには現れない。
	type key struct{ annotation, itemID string }
	confirmedAt := map[key]int{}
	seq := 0
	for _, run := range f.runs {
		for _, it := range run.Items {
			seq++
			if run.BoardID == boardID && it.Confirmed && it.ItemID != "" {
				confirmedAt[key{run.AnnotationID, it.ItemID}] = seq
			}
		}
	}

	byAnnotation := make(map[string][]port.SyncItem)
	seq = 0
	for _, run := range f.runs {
		for _, it := range run.Items {
			seq++
			if run.BoardID != boardID || it.Confirmed {
				continue
			}
			if at, ok := confirmedAt[key{run.AnnotationID, it.ItemID}]; ok && it.ItemID != "" && at > seq {
				continue
			}
			byAnnotation[run.AnnotationID] = append(byAnnotation[run.AnnotationID], it)
		}
	}

	return byAnnotation, nil
}

// ListItemsByBoard は畳み込みをボード全体で行い、注釈ごとに束ねて返す。
func (f *fakeMappings) ListItemsByBoard(
	ctx context.Context, boardID string,
) (map[string][]port.SyncItem, error) {
	byAnnotation := make(map[string][]port.SyncItem)
	seen := map[string]struct{}{}

	for _, run := range f.runs {
		if run.BoardID != boardID {
			continue
		}
		if _, done := seen[run.AnnotationID]; done {
			continue
		}
		seen[run.AnnotationID] = struct{}{}

		items, err := f.ListItemsByAnnotation(ctx, boardID, run.AnnotationID)
		if err != nil {
			return nil, err
		}
		byAnnotation[run.AnnotationID] = items
	}

	return byAnnotation, nil
}
