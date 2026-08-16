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

	items, createErr := s.createItems(ctx, projectID, in, fields, run.CreatedAt)
	run.Items = items

	// 1 件も作れていないなら記録するものが無い。空の run を残すと、状態が
	// created に変わって「作成済み」に見えてしまう。
	if len(items) == 0 {
		if createErr != nil {
			return nil, createErr
		}
		return nil, fmt.Errorf("%w: nothing to create", ErrInvalidInput)
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

// createItems は epic を先に、次に issue を作る。
//
// 順序に意味がある。issue の親フィールドには epic のタイトルを入れるため、
// epic が確定していないと値を決められない。
func (s *CreationService) createItems(
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

			saved, err := s.createOne(ctx, projectID, item, fields, epicTitles, now)
			if err != nil {
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

// createOne は draft issue を 1 件作り、種別と親を設定する。
func (s *CreationService) createOne(
	ctx context.Context,
	projectID string,
	item domain.InterpretedItem,
	fields projectFields,
	epicTitles map[string]string,
	now time.Time,
) (port.SyncItem, error) {
	itemID, err := s.github.CreateDraftIssue(ctx, projectID,
		port.DraftIssue{Title: item.Title, Body: item.Body})
	if err != nil {
		return port.SyncItem{}, fmt.Errorf("create %q: %w", item.Title, err)
	}

	optionID := fields.epicOptionID
	if item.Kind == domain.KindIssue {
		optionID = fields.issueOptionID
	}
	if err := s.github.SetItemFieldValue(ctx, projectID, itemID,
		port.FieldValue{FieldID: fields.kindID, OptionID: &optionID}); err != nil {
		return port.SyncItem{}, fmt.Errorf("set kind on %q: %w", item.Title, err)
	}

	// GitHub に送ったものをそのまま控える。逆方向同期を実装しない以上、
	// ここで取らなければ何を作ったのか二度と分からない（ADR 0023）。
	saved := port.SyncItem{
		ItemID:        itemID,
		Kind:          toPortKind(item.Kind),
		Title:         item.Title,
		Body:          item.Body,
		LocalID:       item.LocalID,
		ParentLocalID: item.ParentLocalID,
		CreatedAt:     now,
	}

	if item.ParentLocalID == nil {
		return saved, nil
	}

	// 親は epic のタイトルで指す。draft issue は native な親子関係を持てない
	// ため、Text フィールドによる手作りの外部キーになる（ADR 0006）。
	title, ok := epicTitles[*item.ParentLocalID]
	if !ok {
		// 検証を通っていれば起きない。起きたら親子が壊れるので止める。
		return port.SyncItem{}, fmt.Errorf("parent %q of %q was not created",
			*item.ParentLocalID, item.Title)
	}
	if err := s.github.SetItemFieldValue(ctx, projectID, itemID,
		port.FieldValue{FieldID: fields.parentID, Text: &title}); err != nil {
		return port.SyncItem{}, fmt.Errorf("set parent on %q: %w", item.Title, err)
	}

	return saved, nil
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
