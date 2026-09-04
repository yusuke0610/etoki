package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// カスタムフィールド名の既定値。
//
// GitHub のフィールド名は利用者が決めるので、合わせられるよう差し替え可能に
// してある。既定は日本語環境でも読める短い英単語にした。
const (
	DefaultKindFieldName   = "Kind"
	DefaultParentFieldName = "Parent"
)

// 作成に固有のエラー。
var (
	// ErrProjectFieldMissing は必要なカスタムフィールドが見つからないことを表す。
	//
	// 黙って作成を続けると、種別も親子も無い draft issue が並ぶだけになり、
	// 2 階層という設計（ADR 0006）が成立しない。止めて設定を促す。
	ErrProjectFieldMissing = errors.New("etoki: required project field is missing")

	// ErrCreationIncomplete は途中まで作成して失敗したことを表す。
	//
	// このエラーが返っても run は記録されている。GitHub 側の draft issue は
	// 削除しないため、記録しないと追跡不能になる（ADR 0009）。
	ErrCreationIncomplete = errors.New("etoki: draft issue creation did not complete")

	// ErrContentHashMismatch は解釈時点と作成時点の保存済みシーンが違うことを表す。
	ErrContentHashMismatch = errors.New("etoki: board changed after interpretation")

	// ErrPreviousItemUnknown は更新先がその注釈のものではないことを表す。
	//
	// **これが唯一の関門。** previousItemId はリクエストから来るので、確かめずに
	// 通すと、任意の node ID を書いて無関係な draft issue を書き換えられる。
	// 解釈と作成のあいだに別の run が積まれて対応づけが古くなった場合もここに来る。
	// どちらも打ち手は同じで、解釈からやり直す。
	ErrPreviousItemUnknown = errors.New("etoki: previous item does not belong to this annotation")

	// ErrTargetNotSelected はボードに作成先が設定されていないことを表す。
	//
	// 作成先はボードごとに持つ（ADR 0014）。未選択のまま作ると、どこにも
	// 置き場所が無い。
	ErrTargetNotSelected = errors.New("etoki: board has no target project")
)

// CreationService は解釈結果から draft issue を作り、実行を記録する。
//
// 何を作るかは開発者が解釈結果を見て決める。この層は渡されたものを作るだけで、
// 自分で解釈し直したり、作る内容を選んだりしない（中核思想 3）。
//
// 作成先はコンストラクタでは受け取らない。ボードごとに違うので、Create の中で
// 読み込んだ board から取る（ADR 0014）。
type CreationService struct {
	boardGuard
	mappings    port.MappingRepository
	github      port.GitHubClient
	locks       *BoardLocks
	kindField   string
	parentField string
	now         func() time.Time
}

// CreationServiceOption は CreationService の設定を差し替える。
type CreationServiceOption func(*CreationService)

// WithFieldNames は種別・親のカスタムフィールド名を差し替える。
func WithFieldNames(kind, parent string) CreationServiceOption {
	return func(s *CreationService) {
		if kind != "" {
			s.kindField = kind
		}
		if parent != "" {
			s.parentField = parent
		}
	}
}

// WithCreationClock は時刻の取得方法を差し替える。
func WithCreationClock(f func() time.Time) CreationServiceOption {
	return func(s *CreationService) { s.now = f }
}

// NewCreationService は CreationService を作る。
//
// locks は BoardService と同じものを渡す（BoardLocks を参照）。
func NewCreationService(
	boards port.BoardRepository,
	mappings port.MappingRepository,
	github port.GitHubClient,
	locks *BoardLocks,
	opts ...CreationServiceOption,
) *CreationService {
	s := &CreationService{
		boardGuard:  boardGuard{boards: boards},
		mappings:    mappings,
		github:      github,
		locks:       locks,
		kindField:   DefaultKindFieldName,
		parentField: DefaultParentFieldName,
		now:         time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Create は解釈結果から draft issue を作り、run として記録する。
//
// 途中で失敗しても、作れたところまでを run に記録してから
// ErrCreationIncomplete を返す。GitHub 側に作られた draft issue は削除しないので、
// 記録しないと etoki から追跡できなくなる（ADR 0009）。
func (s *CreationService) Create(
	ctx context.Context, boardID, annotationID, contentHash string, in domain.Interpretation,
) (*port.SyncRun, error) {
	// 作成先を読んでから run を記録するまで、このボードの作成先を動かさせない。
	// 途中で変えられると、作った先と記録が指す先がずれる（BoardLocks を参照）。
	release, err := s.locks.Acquire(ctx, boardID)
	if err != nil {
		return nil, err
	}
	defer release()

	// 作れるかどうかを最終的に決めるのは GitHub。ここで見るのは etoki 側の
	// メンバーシップだけで、リポジトリの権限は複製しない（ADR 0017）。
	acc, err := s.access(ctx, boardID, port.RoleEditor)
	if err != nil {
		return nil, err
	}
	board := acc.Board

	// 作成先はボードが持つ。未選択なら止める。ここを通すと、置き場所の無い
	// draft issue を GitHub 側のエラーとして受け取ることになり、原因が
	// 設定不足だと分かりにくい。
	if !board.Target.Selected() {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotSelected, boardID)
	}
	projectID := board.Target.ProjectID

	scene, err := domain.ParseScene([]byte(board.Scene))
	if err != nil {
		return nil, err
	}

	annotation, ok := findAnnotation(scene, annotationID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrAnnotationNotFound, annotationID)
	}
	if !annotation.Granularity.Valid() {
		return nil, fmt.Errorf("%w: unknown granularity %q on annotation %s",
			ErrInvalidInput, annotation.Granularity, annotationID)
	}

	// 解釈時点と作成時点で保存済みシーンが変わっていたら作らない。作った
	// draft issue は削除できないので、古い解釈のまま作ると取り消せない（ADR 0010）。
	// 任意にすると API を直接叩く経路で素通りできるため、未指定も弾く。
	if contentHash == "" {
		return nil, fmt.Errorf("%w: contentHash is required", ErrInvalidInput)
	}
	currentHash := string(scene.AnnotationHash(annotation))
	if contentHash != currentHash {
		return nil, fmt.Errorf("%w: interpret again before creating draft issues", ErrContentHashMismatch)
	}

	// リクエストで受け取った内容は信用せず、ここで検証し直す。フロントを
	// 経由しない呼び出しでも 2 階層の制約が守られる必要がある。
	if err := in.Validate(annotation.Granularity); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	// 更新先がこの注釈のものであることを確かめる。畳み込みが「いま GitHub に
	// 在らしめているもの」なので、そこに無い ID は更新してよい相手ではない
	// （ADR 0026）。
	known, err := s.mappings.ListItemsByAnnotation(ctx, boardID, annotationID)
	if err != nil {
		return nil, err
	}
	if err := checkPreviousItems(in, known); err != nil {
		return nil, err
	}

	fields, err := s.resolveFields(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// 記録するのは保存済みシーンのハッシュ。状態判定の基準と同じものを残さないと、
	// 次回の 3 状態判定が噛み合わない。
	run := port.SyncRun{
		BoardID:      boardID,
		AnnotationID: annotationID,
		ContentHash:  currentHash,
		CreatedAt:    s.now(),
	}

	items, createErr := s.applyItems(ctx, projectID, in, fields, run.CreatedAt)
	run.Items = items

	// 1 件も作れていないなら記録するものが無い。空の run を残すと、状態が
	// created に変わって「作成済み」に見えてしまう。
	if len(items) == 0 {
		if createErr != nil {
			return nil, createErr
		}
		return nil, fmt.Errorf("%w: nothing to create", ErrInvalidInput)
	}

	// 途中で失敗したことを run に載せてから保存する。応答にしか出さないと、
	// 手掛かりが生きているのはその 1 回ぶんだけになり、あとから履歴を見た
	// 開発者には「作れた件数が少ない run」としか見えない（ADR 0043）。
	//
	// 文字列は応答に載せるものと同じにする。片方だけ包みを剥くと、同じ失敗が
	// 2 通りの見え方になる。
	if createErr != nil {
		run.Outcome = port.OutcomeIncomplete
		run.Error = createErr.Error()
	} else {
		run.Outcome = port.OutcomeComplete
	}

	id, saveErr := s.mappings.SaveRun(ctx, run)
	if saveErr != nil {
		// 作成には成功したが記録できなかった。ここが一番まずい状態なので、
		// 作った item ID をエラーに載せて手で追えるようにする。
		return nil, fmt.Errorf("save run (created %s): %w", itemIDs(items), saveErr)
	}
	run.ID = id

	return &run, createErr
}

// checkPreviousItems は更新先がその注釈のものかを確かめる。
//
// 1 件でも見知らぬ ID があれば、何も作らず何も更新せずに止める。半分だけ
// 進めると、作成は取り消せないのに「どこまで進んだか」が分からなくなる。
func checkPreviousItems(in domain.Interpretation, known []port.SyncItem) error {
	allowed := make(map[string]struct{}, len(known))
	for _, it := range known {
		allowed[it.ItemID] = struct{}{}
	}

	for _, item := range in.Items {
		if item.PreviousItemID == nil {
			continue
		}
		if _, ok := allowed[*item.PreviousItemID]; !ok {
			return fmt.Errorf("%w: %s (%q): interpret again before creating",
				ErrPreviousItemUnknown, *item.PreviousItemID, item.Title)
		}
	}

	return nil
}

// applyItems は epic を先に、次に issue を作る（または書き換える）。
//
// 順序に意味がある。issue の親フィールドには epic のタイトルを入れるため、
// epic が確定していないと値を決められない。**epic のタイトルを書き換えたときも
// この順序が効く。** 先に epic を書き換えてから子の Parent を張り直すので、
// 同じ解釈に入っている子は新しいタイトルを指す。
func (s *CreationService) applyItems(
	ctx context.Context, projectID string, in domain.Interpretation,
	fields projectFields, now time.Time,
) ([]port.SyncItem, error) {
	var created []port.SyncItem

	// localID から epic のタイトルを引くための表。
	epicTitles := make(map[string]string)

	for _, kind := range []domain.ItemKind{domain.KindEpic, domain.KindIssue} {
		for _, item := range in.Items {
			if item.Kind != kind {
				continue
			}

			saved, err := s.applyOne(ctx, projectID, item, fields, epicTitles, now)
			if err != nil {
				// **済んだところまでは記録する。** draft issue そのものは書けて
				// いて、あとのフィールド設定で失敗した、という並びがある。捨てると
				// GitHub 側は変わったのに履歴は古いままになり、次の 3 状態判定が
				// 実物と食い違う（ADR 0009 / 0026）。
				if saved.ItemID != "" {
					created = append(created, saved)
				}
				return created, fmt.Errorf("%w: %w", ErrCreationIncomplete, err)
			}

			if item.Kind == domain.KindEpic {
				epicTitles[item.LocalID] = item.Title
			}
			created = append(created, saved)
		}
	}

	return created, nil
}

// applyOne は draft issue を 1 件作るか書き換え、種別と親を設定し直す。
//
// **種別と親は更新でも張り直す。** 手直しで kind を変えられるし（ADR 0024）、
// 親の epic のタイトルが変われば子の Parent 値は古くなる。作成時と同じ経路を
// 通しておけば、片方だけ古いフィールドが残らない。
func (s *CreationService) applyOne(
	ctx context.Context,
	projectID string,
	item domain.InterpretedItem,
	fields projectFields,
	epicTitles map[string]string,
	now time.Time,
) (port.SyncItem, error) {
	itemID, action, err := s.writeDraftIssue(ctx, projectID, item)
	if err != nil {
		return port.SyncItem{}, err
	}

	// GitHub に送ったものをそのまま控える。逆方向同期を実装しない以上、
	// ここで取らなければ何を作ったのか二度と分からない（ADR 0023）。
	//
	// **書き込みが済んだ直後に組み立てる。** このあとのフィールド設定で失敗
	// しても、draft issue そのものはもう変わっている。呼び出し側が記録できる
	// よう、エラーと一緒にこれを返す（ADR 0009 / 0026）。
	saved := port.SyncItem{
		ItemID:        itemID,
		Kind:          toPortKind(item.Kind),
		Title:         item.Title,
		Body:          item.Body,
		LocalID:       item.LocalID,
		ParentLocalID: item.ParentLocalID,
		Action:        action,
		CreatedAt:     now,
	}

	optionID := fields.epicOptionID
	if item.Kind == domain.KindIssue {
		optionID = fields.issueOptionID
	}
	if err := s.github.SetItemFieldValue(ctx, projectID, itemID,
		port.FieldValue{FieldID: fields.kindID, OptionID: &optionID}); err != nil {
		return saved, fmt.Errorf("set kind on %q: %w", item.Title, err)
	}

	if item.ParentLocalID == nil {
		// 子だったものが top-level（または epic）に変わったら、GitHub 側に
		// 残っている Parent 値を消す。放っておくと、親を外したはずの item が
		// 古い epic にぶら下がったままになる。**新規作成では消さない。**
		// 空のフィールドをわざわざ空で上書きする往復が増えるだけ。
		if action == port.ActionUpdated {
			empty := ""
			if err := s.github.SetItemFieldValue(ctx, projectID, itemID,
				port.FieldValue{FieldID: fields.parentID, Text: &empty}); err != nil {
				return saved, fmt.Errorf("clear parent on %q: %w", item.Title, err)
			}
		}
		return saved, nil
	}

	// 親は epic のタイトルで指す。draft issue は native な親子関係を持てない
	// ため、Text フィールドによる手作りの外部キーになる（ADR 0006）。
	title, ok := epicTitles[*item.ParentLocalID]
	if !ok {
		// 検証を通っていれば起きない。起きたら親子が壊れるので止める。
		return saved, fmt.Errorf("parent %q of %q was not created",
			*item.ParentLocalID, item.Title)
	}
	if err := s.github.SetItemFieldValue(ctx, projectID, itemID,
		port.FieldValue{FieldID: fields.parentID, Text: &title}); err != nil {
		return saved, fmt.Errorf("set parent on %q: %w", item.Title, err)
	}

	return saved, nil
}

// writeDraftIssue は draft issue を作るか書き換え、その item ID を返す。
//
// 分岐はここだけ。呼び出し側は「作った」か「書き換えた」かを Action で受け取る。
func (s *CreationService) writeDraftIssue(
	ctx context.Context, projectID string, item domain.InterpretedItem,
) (string, port.SyncAction, error) {
	draft := port.DraftIssue{Title: item.Title, Body: item.Body}

	if item.PreviousItemID == nil {
		itemID, err := s.github.CreateDraftIssue(ctx, projectID, draft)
		if err != nil {
			return "", "", fmt.Errorf("create %q: %w", item.Title, err)
		}
		return itemID, port.ActionCreated, nil
	}

	// 更新先が本当にこの注釈のものかは Create の入口で確かめてある。
	itemID := *item.PreviousItemID
	if err := s.github.UpdateDraftIssue(ctx, itemID, draft); err != nil {
		return "", "", fmt.Errorf("update %q: %w", item.Title, err)
	}

	return itemID, port.ActionUpdated, nil
}

// projectFields は作成に必要なカスタムフィールドの ID。
type projectFields struct {
	kindID        string
	epicOptionID  string
	issueOptionID string
	parentID      string
}

// resolveFields は種別と親のフィールド ID を名前から解決する。
func (s *CreationService) resolveFields(ctx context.Context, projectID string) (projectFields, error) {
	all, err := s.github.ListProjectFields(ctx, projectID)
	if err != nil {
		return projectFields{}, err
	}

	var out projectFields

	for _, f := range all {
		switch f.Name {
		case s.kindField:
			for _, o := range f.Options {
				switch strings.ToLower(o.Name) {
				case string(domain.KindEpic):
					out.kindID, out.epicOptionID = f.ID, o.ID
				case string(domain.KindIssue):
					out.kindID, out.issueOptionID = f.ID, o.ID
				}
			}
		case s.parentField:
			out.parentID = f.ID
		}
	}

	// 何が足りないかを具体的に返す。「フィールドがありません」だけでは、
	// 開発者は GitHub 側で何を作ればよいのか分からない。
	var missing []string
	if out.epicOptionID == "" {
		missing = append(missing, fmt.Sprintf("single select %q with option %q", s.kindField, domain.KindEpic))
	}
	if out.issueOptionID == "" {
		missing = append(missing, fmt.Sprintf("single select %q with option %q", s.kindField, domain.KindIssue))
	}
	if out.parentID == "" {
		missing = append(missing, fmt.Sprintf("text %q", s.parentField))
	}
	if len(missing) > 0 {
		return projectFields{}, fmt.Errorf("%w: %s", ErrProjectFieldMissing, strings.Join(missing, ", "))
	}

	return out, nil
}

// toPortKind はドメインの種別を境界の DTO に詰め替える。
//
// domain と port は同じ値を別々に定義している。port は internal/ に依存
// できないため型を共有できない（ADR 0001）。片方だけ値を変えてもコンパイルは
// 通るので、対応はここ 1 箇所に閉じておく。
func toPortKind(k domain.ItemKind) port.ItemKind {
	switch k {
	case domain.KindEpic:
		return port.KindEpic
	case domain.KindIssue:
		return port.KindIssue
	default:
		return port.ItemKind(k)
	}
}

// itemIDs は作成済みの item ID を並べる。記録に失敗したときの手掛かり。
func itemIDs(items []port.SyncItem) string {
	ids := make([]string, len(items))
	for i, it := range items {
		ids[i] = it.ItemID
	}
	return strings.Join(ids, ", ")
}
