package usecase

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// 解釈に固有のエラー。呼び出し側が応答コードを決めるために使う。
//
// ボードを引き当てられない場合の ErrBoardNotFound は access.go にある。
// 認可の判断と同じ場所に置かないと、404 と 403 の分け方が読み取れない。
var (
	// ErrAnnotationNotFound は対象の注釈がシーンに無いことを表す。
	ErrAnnotationNotFound = errors.New("etoki: annotation not found")
	// ErrInterpretationFailed は上限まで再送しても出力がスキーマを満たさなかった
	// ことを表す。LLM への接続自体は成功している。
	ErrInterpretationFailed = errors.New("etoki: llm output did not satisfy the schema")
	// ErrLLMUnavailable は LLM の呼び出しそのものが失敗したことを表す。
	//
	// 接続不可・認証エラー・原因の分からない失敗などが該当する。スキーマ違反とは
	// 原因も打ち手も違うので、呼び出し側が区別できるよう分けている。
	//
	// 打ち切り・拒否・上流の混雑は別の sentinel に分けてある（llmcall.go、
	// ADR 0087）。LLMClient の実装がそれを名乗らなかったときは、ここに落ちる。
	ErrLLMUnavailable = errors.New("etoki: llm call failed")
)

// defaultMaxAttempts は 1 回の解釈で LLM を呼ぶ回数の上限。
//
// 初回に加えて修正指示つきの再送を 2 回まで行う。上限を設けるのは、直らない
// 出力に対して呼び出しを繰り返しても課金が増えるだけであるため。
const defaultMaxAttempts = 3

// supportedImageMediaType は受け付ける画像の MIME タイプ。
//
// フロントの書き出しに合わせて PNG だけにする。増やすなら、モデル側が扱える
// かどうかをアダプタごとに確かめてからにする。
const supportedImageMediaType = "image/png"

// InterpretationService は注釈範囲を LLM に解釈させる。
//
// プロンプト構築・スキーマ検証・修正指示つき再送をこの層が持つ。port.LLMClient
// には 1 回の呼び出ししか任せない（ADR 0005）。
//
// この層は解釈するだけで、GitHub には何も作らず sync_runs にも書かない。
// 何を作るかは解釈結果を見た開発者が別途トリガーする。
type InterpretationService struct {
	boardGuard
	// mappings は前回までに作ったものを引くために持つ。解釈の入力にするのは
	// テキストと画像だけではなく、「前回何を作ったか」も含まれる（ADR 0026）。
	mappings port.MappingRepository
	llm      port.LLMClient
	// limits は LLM を叩く実行の上限（ADR 0044）。DiagramService と**同じものを
	// 共有する。** 別々に持たせると、合計では上限の倍だけ叩ける。
	limits      *LLMLimiter
	maxAttempts int
	logger      *slog.Logger
}

// InterpretationResult は解釈結果と、その入力になった保存済みシーンのハッシュ。
type InterpretationResult struct {
	Interpretation domain.Interpretation
	ContentHash    string
	// Context は囲みの外から添えたもの（ADR 0093）。**何を渡したかを結果と
	// 一緒に返す。** 黙って材料を足して結果だけを返すと、出力がなぜそうなった
	// かを読めない（中核思想 3）。
	Context InterpretationContext
}

// InterpretationContext は解釈の入力に、囲みの外から添えたもの（ADR 0093）。
//
// **どれもハッシュに入らない。** ハッシュに入るのは囲みの中に人が書いたもの
// と、囲みの読み方として選んだもの（粒度と種別）だけ（domain.ComputeContentHash）。
type InterpretationContext struct {
	// Premise はボードの背景。解釈を始めた時点のもの。空なら添えていない。
	Premise string
	// OtherItems は同じボードの別の絵から作ったもの。最近作った・更新した
	// ものから MaxOtherItems 件まで。
	OtherItems []OtherItem
	// OmittedOtherItems は上限で打ち切って渡さなかった件数。
	OmittedOtherItems int
}

// OtherItem は同じボードの別の絵から作ったもの 1 件。解釈に渡すのは種別と
// タイトルだけ。
//
// **ref を振らない。** 更新先はその囲みの畳み込みに限る（ADR 0026）。ref を
// 振ると、別の絵から作った draft issue が書き換えの候補に見える。
type OtherItem struct {
	Kind  domain.ItemKind
	Title string
}

// InterpretationServiceOption は InterpretationService の設定を差し替える。
type InterpretationServiceOption func(*InterpretationService)

// WithMaxAttempts は LLM を呼ぶ回数の上限を差し替える。
//
// テストが既定の回数だけ無駄に往復するのを避けるために公開している。
// 0 以下は無視する。1 度も呼ばずに「スキーマを満たさなかった」と報告すると、
// 何が起きたのか追えないため。
func WithMaxAttempts(n int) InterpretationServiceOption {
	return func(s *InterpretationService) {
		if n > 0 {
			s.maxAttempts = n
		}
	}
}

// WithLogger は実績の記録先を差し替える。nil は無視する。
//
// 解釈は LLM を叩く外部呼び出しで課金を伴うので、呼んだ実績はどこかに残る
// 必要がある（ADR 0031）。既定は slog の既定ロガー。
func WithLogger(l *slog.Logger) InterpretationServiceOption {
	return func(s *InterpretationService) {
		if l != nil {
			s.logger = l
		}
	}
}

// NewInterpretationService は InterpretationService を作る。
//
// limits は DiagramService と同じものを渡す（LLMLimiter を参照）。任意の設定
// ではなく引数にしてあるのは、渡し忘れを緑のまま通さないため。
func NewInterpretationService(
	boards port.BoardRepository, mappings port.MappingRepository, llm port.LLMClient,
	limits *LLMLimiter, opts ...InterpretationServiceOption,
) *InterpretationService {
	s := &InterpretationService{
		boardGuard:  boardGuard{boards: boards},
		mappings:    mappings,
		llm:         llm,
		limits:      limits,
		maxAttempts: defaultMaxAttempts,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// preparedInterpretation は LLM を叩く前に揃えた入力。
type preparedInterpretation struct {
	scene      domain.Scene
	annotation domain.Annotation
	images     []port.Image
	previous   []domain.PreviousItem
	// context は始めた時点で揃える。走っているあいだに背景が書き換わっても、
	// このジョブは始めたときの背景で読み、結果にもそちらを載せる。
	context InterpretationContext
}

// prepare は認可と入力の検証を済ませ、LLM に渡す入力を揃える。
//
// テキストを読むのは保存済みシーンである。フロントで編集中の内容は反映されない。
//
// images は注釈範囲を写した画像で、矢印やグルーピングのようにテキストに現れない
// 構造を渡すためのもの。空でもよく、その場合はテキストだけで解釈する。画像は
// フロントが画面から書き出すため、保存済みシーンと一致している保証はここには
// 無い。揃えるのは UI の責務にしてある（ADR 0018）。
//
// **枠はここでは取らない。** 認可も入力の検証もすべて通ってから取るので
// （ADR 0044）、404 や 400 で返るリクエストは枠を消費しない。取るのは
// InterpretationJobs.Start（jobTable.start）で、prepare の直後に取る。
func (s *InterpretationService) prepare(
	ctx context.Context, boardID, annotationID string, images []port.Image,
) (preparedInterpretation, error) {
	// 解釈は LLM を叩く外部呼び出しなので editor 以上に限る。viewer に許すのは
	// 「閲覧」ではない（ADR 0017）。
	acc, err := s.access(ctx, boardID, port.RoleEditor)
	if err != nil {
		return preparedInterpretation{}, err
	}

	// LLM を叩く前に弾く。弾くと決めた入力で呼ぶと、結果は捨てるのに課金だけ
	// 発生する。
	if err := validateImages(images); err != nil {
		return preparedInterpretation{}, err
	}

	scene, err := domain.ParseScene([]byte(acc.Board.Scene))
	if err != nil {
		return preparedInterpretation{}, err
	}

	annotation, ok := findAnnotation(scene, annotationID)
	if !ok {
		return preparedInterpretation{}, fmt.Errorf("%w: %s", ErrAnnotationNotFound, annotationID)
	}

	// 粒度と種別が未知の値ならここで止める。既定に倒して解釈を進めると、開発者が
	// 指定したつもりの制約が黙って外れる。保存の入口でも見ているが、検査を入れる
	// 前に保存されたシーンがありうるので、読む側でも見る。
	if err := annotation.CheckMeta(); err != nil {
		return preparedInterpretation{}, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	// 前回までに作ったものを見せて、「これは前回のどれの更新か」を答えさせる。
	// 対応づけを座標や文字列一致でルールベースに推測せず、LLM に解釈させる
	// （中核思想 2、ADR 0026）。決めるのは開発者で、これは候補にすぎない。
	//
	// **ボード全体の畳み込みを 1 回で引く。** この囲みのぶんは前回ぶんに、
	// 別の絵のぶんは用語と重なりの参考に使う（ADR 0093）。どちらも同じ畳み込み
	// （確定したものだけ、ADR 0056）から採る。
	byAnnotation, err := s.mappings.ListItemsByBoard(ctx, boardID)
	if err != nil {
		return preparedInterpretation{}, err
	}
	others, omitted := otherItems(byAnnotation, annotationID)

	return preparedInterpretation{
		scene:      scene,
		annotation: annotation,
		images:     images,
		previous:   toPreviousItems(byAnnotation[annotationID]),
		context: InterpretationContext{
			Premise:           acc.Board.Premise,
			OtherItems:        others,
			OmittedOtherItems: omitted,
		},
	}, nil
}

// execute は枠を取った状態で LLM を呼び、結果を返す。実績もここで残す。
func (s *InterpretationService) execute(
	ctx context.Context, boardID, annotationID string, p preparedInterpretation,
) (InterpretationResult, error) {
	in, usage, err := s.complete(
		ctx, p.annotation, p.scene.AnnotationTexts(annotationID), p.images, p.previous, p.context)
	// **失敗しても残す。** 再送して直らなかったぶんも課金されているので、
	// 成功したときだけ記録すると、実績が実際より小さく見える（ADR 0031）。
	logLLMUsage(ctx, s.logger, "interpretation llm usage", usage, err,
		slog.String("boardId", boardID), slog.String("annotationId", annotationID))
	if err != nil {
		return InterpretationResult{}, err
	}

	return InterpretationResult{
		Interpretation: in,
		ContentHash:    string(p.scene.AnnotationHash(p.annotation)),
		Context:        p.context,
	}, nil
}

// otherItems は同じボードの別の絵から作ったものを、解釈に渡す形にする
// （ADR 0093）。
//
// **シーンから消えた注釈のぶんも含める。** 畳み込みは注釈がシーンに残って
// いるかを見ないので、ここでも分けない。GitHub には在るので、重複を避ける
// 材料としては同じ（ADR 0046）。
//
// **最近作った・更新したものから残す。** いま描いているものと重なりやすいのは
// 最近の作業だから。畳み込みが返す ID は最新の記録のもの（ADR 0026）なので、
// その降順で並べる。上限を超えたぶんは渡さず、件数だけを返す。
func otherItems(byAnnotation map[string][]port.SyncItem, annotationID string) ([]OtherItem, int) {
	var all []port.SyncItem
	for id, items := range byAnnotation {
		if id == annotationID {
			continue
		}
		all = append(all, items...)
	}
	slices.SortFunc(all, func(a, b port.SyncItem) int { return cmp.Compare(b.ID, a.ID) })

	omitted := 0
	if len(all) > MaxOtherItems {
		omitted = len(all) - MaxOtherItems
		all = all[:MaxOtherItems]
	}

	out := make([]OtherItem, 0, len(all))
	for _, it := range all {
		out = append(out, OtherItem{Kind: domain.ItemKind(it.Kind), Title: it.Title})
	}
	return out, omitted
}

// toPreviousItems は保存済みの記録を、解釈に見せる形へ詰め替える。
//
// ref は並び順に `p1` から振る。畳み込みの並びは最初に作られた順で固定して
// あるので（ADR 0026）、同じ状態なら同じ ref になる。**この ref を保存しない。**
// 1 回の解釈のあいだだけ通じる一時 ID であり、次の解釈では振り直される。
// LocalID が解釈ごとに振り直されるのと同じ扱い。
func toPreviousItems(items []port.SyncItem) []domain.PreviousItem {
	previous := make([]domain.PreviousItem, 0, len(items))
	for i, it := range items {
		previous = append(previous, domain.PreviousItem{
			Ref:    fmt.Sprintf("p%d", i+1),
			ItemID: it.ItemID,
			Kind:   domain.ItemKind(it.Kind),
			Title:  it.Title,
			Body:   it.Body,
		})
	}
	return previous
}

// validateImages は LLM に渡す画像が上限と形式を満たすかを見る。
//
// 超えたものを縮小したり落としたりはしない。渡したはずの情報が黙って消えたこと
// に、開発者は気づけないため（ADR 0018）。
func validateImages(images []port.Image) error {
	if len(images) > MaxImages {
		return fmt.Errorf("%w: at most %d image(s) per interpretation, got %d",
			ErrInvalidInput, MaxImages, len(images))
	}

	for i, img := range images {
		if img.MediaType != supportedImageMediaType {
			return fmt.Errorf("%w: images[%d] media type %q is not supported, want %q",
				ErrInvalidInput, i, img.MediaType, supportedImageMediaType)
		}
		if len(img.Data) == 0 {
			return fmt.Errorf("%w: images[%d] is empty", ErrInvalidInput, i)
		}
		if len(img.Data) > MaxImageBytes {
			return fmt.Errorf("%w: images[%d] is %d bytes, limit is %d",
				ErrInvalidInput, i, len(img.Data), MaxImageBytes)
		}
	}

	return nil
}

// complete は検証を満たす出力が得られるまで、修正指示を添えて呼び直す。
//
// 払ったぶんは成否によらず返す。呼び出し側がそれを記録する。呼び直しの手順は
// 図のドラフト生成と同じで、llmRetry が持つ。
func (s *InterpretationService) complete(
	ctx context.Context, a domain.Annotation, texts []domain.TextElement, images []port.Image,
	previous []domain.PreviousItem, ic InterpretationContext,
) (domain.Interpretation, llmUsage, error) {
	base := buildUserMessage(a, texts, len(images) > 0, previous, ic)
	req := port.VisionRequest{
		System: interpretationSystemPrompt(a.Granularity, len(previous) > 0),
		Text:   base,
		Images: images,
	}

	return llmRetry[domain.Interpretation]{
		maxAttempts: s.maxAttempts,
		parse: func(text string) (domain.Interpretation, error) {
			return parseInterpretation(text, a.Granularity, previous)
		},
		retryText: func(previousOutput string, err error) string {
			return buildRetryMessage(base, previousOutput, err)
		},
		exhausted: ErrInterpretationFailed,
	}.run(ctx, s.llm, req)
}

// parseInterpretation は LLM の出力テキストを解釈結果に変換して検証する。
func parseInterpretation(
	text string, g domain.Granularity, previous []domain.PreviousItem,
) (domain.Interpretation, error) {
	in, err := domain.ParseInterpretation([]byte(extractJSON(text)), previous)
	if err != nil {
		return domain.Interpretation{}, err
	}
	if err := in.Validate(g); err != nil {
		return domain.Interpretation{}, err
	}
	return in, nil
}

// findAnnotation はシーンから ID の一致する注釈を探す。
func findAnnotation(scene domain.Scene, id string) (domain.Annotation, bool) {
	for _, a := range scene.Annotations() {
		if a.ID == id {
			return a, true
		}
	}
	return domain.Annotation{}, false
}

// extractJSON は LLM の出力から JSON 本体を取り出す。
//
// 「JSON だけを返せ」と指示してもコードフェンスや前置きが付いてくることがある。
// それを検証エラーとして再送に回すのは往復の無駄なので、機械的に落とせるものは
// ここで落とす。綴り間違いのように内容の誤りは落とさず、検証に回す。
func extractJSON(text string) string {
	s := strings.TrimSpace(text)

	if after, found := strings.CutPrefix(s, "```"); found {
		// 開きフェンスの言語指定（```json など）は行末まで捨てる。
		if _, rest, ok := strings.Cut(after, "\n"); ok {
			s = rest
		} else {
			s = after
		}
		if before, _, ok := strings.Cut(s, "```"); ok {
			s = before
		}
		s = strings.TrimSpace(s)
	}

	// 前置きや後書きが残っていても、最も外側の波括弧までを本体とみなす。
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		s = s[start : end+1]
	}

	return s
}
