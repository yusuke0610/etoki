package usecase

import (
	"context"
	"errors"
	"fmt"
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
	// 接続不可・認証エラー・モデル側の失敗などが該当する。スキーマ違反とは
	// 原因も打ち手も違うので、呼び出し側が区別できるよう分けている。
	ErrLLMUnavailable = errors.New("etoki: llm call failed")
)

// defaultMaxAttempts は 1 回の解釈で LLM を呼ぶ回数の上限。
//
// 初回に加えて修正指示つきの再送を 2 回まで行う。上限を設けるのは、直らない
// 出力に対して呼び出しを繰り返しても課金が増えるだけであるため。
const defaultMaxAttempts = 3

// 画像の上限。超えたら縮小せずに弾く（ADR 0018）。
//
// 上限を持つのはここだけである。フロントは書き出しの解像度を抑えるが、バイト数
// の判定は持たない。両方に持たせると、片方だけ変えたときに「フロントは通すが
// サーバーが弾く」がどちらの言い分か分からなくなる。
//
// 呼び出し側がリクエストボディの大きさを見積もれるよう公開している。
const (
	// MaxImageBytes は画像 1 枚のバイト数の上限。
	MaxImageBytes = 4 << 20
	// MaxImages は解釈 1 回に渡せる画像の枚数の上限。
	//
	// 注釈は 1 つの frame なので、写すべき範囲も 1 つ。
	MaxImages = 1
)

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
	mappings    port.MappingRepository
	llm         port.LLMClient
	maxAttempts int
}

// InterpretationResult は解釈結果と、その入力になった保存済みシーンのハッシュ。
type InterpretationResult struct {
	Interpretation domain.Interpretation
	ContentHash    string
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

// NewInterpretationService は InterpretationService を作る。
func NewInterpretationService(
	boards port.BoardRepository, mappings port.MappingRepository, llm port.LLMClient,
	opts ...InterpretationServiceOption,
) *InterpretationService {
	s := &InterpretationService{
		boardGuard:  boardGuard{boards: boards},
		mappings:    mappings,
		llm:         llm,
		maxAttempts: defaultMaxAttempts,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Interpret は注釈範囲を LLM に解釈させ、スキーマを満たした結果を返す。
//
// テキストを読むのは保存済みシーンである。フロントで編集中の内容は反映されない。
//
// images は注釈範囲を写した画像で、矢印やグルーピングのようにテキストに現れない
// 構造を渡すためのもの。空でもよく、その場合はテキストだけで解釈する。画像は
// フロントが画面から書き出すため、保存済みシーンと一致している保証はここには
// 無い。揃えるのは UI の責務にしてある（ADR 0018）。
func (s *InterpretationService) Interpret(
	ctx context.Context, boardID, annotationID string, images []port.Image,
) (InterpretationResult, error) {
	// 解釈は LLM を叩く外部呼び出しなので editor 以上に限る。viewer に許すのは
	// 「閲覧」ではない（ADR 0017）。
	acc, err := s.access(ctx, boardID, port.RoleEditor)
	if err != nil {
		return InterpretationResult{}, err
	}

	// LLM を叩く前に弾く。弾くと決めた入力で呼ぶと、結果は捨てるのに課金だけ
	// 発生する。
	if err := validateImages(images); err != nil {
		return InterpretationResult{}, err
	}

	scene, err := domain.ParseScene([]byte(acc.Board.Scene))
	if err != nil {
		return InterpretationResult{}, err
	}

	annotation, ok := findAnnotation(scene, annotationID)
	if !ok {
		return InterpretationResult{}, fmt.Errorf("%w: %s", ErrAnnotationNotFound, annotationID)
	}

	// 粒度が未知の値ならここで止める。既定に倒して解釈を進めると、開発者が
	// 指定したつもりの制約が黙って外れる。状態を見せて選ばせる方針に反する。
	if !annotation.Granularity.Valid() {
		return InterpretationResult{}, fmt.Errorf("%w: unknown granularity %q on annotation %s",
			ErrInvalidInput, annotation.Granularity, annotationID)
	}

	// 前回までに作ったものを見せて、「これは前回のどれの更新か」を答えさせる。
	// 対応づけを座標や文字列一致でルールベースに推測せず、LLM に解釈させる
	// （中核思想 2、ADR 0026）。決めるのは開発者で、これは候補にすぎない。
	saved, err := s.mappings.ListItemsByAnnotation(ctx, boardID, annotationID)
	if err != nil {
		return InterpretationResult{}, err
	}
	previous := toPreviousItems(saved)

	in, err := s.complete(ctx, annotation, scene.AnnotationTexts(annotationID), images, previous)
	if err != nil {
		return InterpretationResult{}, err
	}

	return InterpretationResult{
		Interpretation: in,
		ContentHash:    string(scene.AnnotationHash(annotation)),
	}, nil
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
func (s *InterpretationService) complete(
	ctx context.Context, a domain.Annotation, texts []domain.TextElement, images []port.Image,
	previous []domain.PreviousItem,
) (domain.Interpretation, error) {
	base := buildUserMessage(a, texts, len(images) > 0, previous)
	req := port.VisionRequest{
		System: interpretationSystemPrompt,
		Text:   base,
		Images: images,
	}

	var last error

	for attempt := 1; attempt <= s.maxAttempts; attempt++ {
		resp, err := s.llm.Complete(ctx, req)
		if err != nil {
			// 接続やモデル側の失敗は修正指示で直る問題ではないので再送しない。
			return domain.Interpretation{}, fmt.Errorf("%w (attempt %d): %w", ErrLLMUnavailable, attempt, err)
		}

		in, err := parseInterpretation(resp.Text, a.Granularity, previous)
		if err == nil {
			return in, nil
		}

		last = err
		// VisionRequest は 1 往復ぶんしか運べないので、会話履歴の代わりに
		// 元の指示ごと組み立て直して自己完結させる。
		req.Text = buildRetryMessage(base, resp.Text, err)
	}

	return domain.Interpretation{}, fmt.Errorf("%w (%d attempts): %w", ErrInterpretationFailed, s.maxAttempts, last)
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

// buildUserMessage は注釈範囲のテキストと粒度指定を 1 通のメッセージにする。
//
// hasImage は画像を添えたかどうか。画像の話は添えたときにだけ書く。常に書くと、
// 画像なしの解釈でモデルが「見えるはずの図」を前提に埋め合わせを始める。
func buildUserMessage(
	a domain.Annotation, texts []domain.TextElement, hasImage bool, previous []domain.PreviousItem,
) string {
	var b strings.Builder

	b.WriteString("囲みに含まれるテキスト:\n")
	if len(texts) == 0 {
		// テキストが無くても解釈は試みる。図形と矢印だけの囲みがありうるため
		// で、その中身は画像でしか渡せない。
		//
		// 「無い」ことは明示する。黙って空にすると、モデルが入力の欠落を
		// 疑って作り話を始める。
		b.WriteString("(なし)\n")
	}
	for _, t := range texts {
		// 囲みの名前は AnnotationTexts が先頭に含めているので、ここでは
		// 別立てにしない。
		b.WriteString("- ")
		b.WriteString(strings.ReplaceAll(t.Text, "\n", " "))
		b.WriteString("\n")
	}

	if hasImage {
		b.WriteString("\n")
		b.WriteString(imageInstruction)
	}

	b.WriteString(previousInstruction(previous))

	b.WriteString("\n")
	b.WriteString(granularityInstruction(a.Granularity))
	b.WriteString("\n")

	return b.String()
}

// previousInstruction は前回までに作ったものを ref つきで並べる。
//
// **node ID は見せない。** `PVTI_lADOA...` を復唱させると取り違えるので、`p1` の
// ような短い ref にして、解決は etoki 側で行う（ADR 0026）。
//
// 1 件も無ければ節ごと省く。空の一覧を見せると、モデルが「あるはずのもの」を
// 埋め合わせ始める。
func previousInstruction(previous []domain.PreviousItem) string {
	if len(previous) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n前回までにこの囲みから作ったもの:\n")

	for _, p := range previous {
		fmt.Fprintf(&b, "- %s (%s) %s\n", p.Ref, p.Kind, oneLine(p.Title))
		if body := strings.TrimSpace(p.Body); body != "" {
			fmt.Fprintf(&b, "    本文: %s\n", oneLine(body))
		}
	}

	b.WriteString(previousMatchInstruction)

	return b.String()
}

// oneLine は一覧に載せるために改行を潰す。
//
// 箇条書きの途中で改行されると、次の行が別の項目に見える。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// previousMatchInstruction は対応づけの出し方を伝える。
const previousMatchInstruction = `
上の一覧と今回の項目が同じものを指しているなら、その項目の previousRef に
一覧の ID（p1 など）を入れてください。書き換えではなく新しく作るものは
previousRef を null にしてください。

- タイトルが変わっていても、同じことを指しているなら対応づけてください。
  文言の見直しはよくある変更です。
- 迷ったら null にしてください。**新しく作るほうが取り返しがつきます。**
  対応づけを間違えると、別のものを書き換えてしまいます。
- 1 つの previousRef を 2 つの項目に使わないでください。
- 今回の出力に対応するものが無い項目が一覧に残っていても構いません。
`

// imageInstruction は添えた画像とテキスト一覧の役割分担を伝える。
//
// 分担を書かないとモデルは画像の文字を読み直す。画像は frame の範囲を写した
// ものなので囲みの外は入らないが、読み取りの誤りは入る。文言の正はテキスト
// 一覧に置き、画像はテキストに現れない関係のためだけに使わせる。
const imageInstruction = `この囲みを写した画像を添えています。矢印の向き、囲みの入れ子、
配置の近さといった、上のテキスト一覧には現れない関係を読み取るために使ってください。
文言は上のテキスト一覧が正です。画像から文字を読み取り直さないでください。
`

// granularityInstruction は開発者の粒度指定を指示文にする。
func granularityInstruction(g domain.Granularity) string {
	switch g {
	// **どう振る舞うかは制約一覧（domain.Rules）が言う。** ここで繰り返すと、
	// 粒度の規則だけが 2 箇所になる。
	case domain.GranularityEpic:
		return "開発者はこの範囲を epic 相当と指定しています。"
	case domain.GranularityIssue:
		return "開発者はこの範囲を issue 相当と指定しています。"
	default:
		return "粒度の指定はありません。内容に応じて epic と issue を使い分けてください。"
	}
}

// buildRetryMessage は直前の出力と指摘を添えた再送メッセージを組み立てる。
func buildRetryMessage(base, previous string, err error) string {
	var b strings.Builder

	b.WriteString(base)
	b.WriteString("\n---\n\n")
	b.WriteString("直前の出力は次の点で誤っています。指摘をすべて直したうえで、")
	b.WriteString("JSON 全体をもう一度出力してください。\n\n")
	b.WriteString("直前の出力:\n")
	b.WriteString(previous)
	b.WriteString("\n\n指摘:\n")

	for _, line := range correctionLines(err) {
		b.WriteString("- ")
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// correctionLines は検証エラーを 1 件 1 行の修正指示にする。
//
// domain.ValidationErrors がスライスなのは、ここで 1 件ずつ取り出すため。
// 全件を並べれば LLM は 1 往復ですべて直せる。
func correctionLines(err error) []string {
	var verrs domain.ValidationErrors
	if errors.As(err, &verrs) {
		lines := make([]string, len(verrs))
		for i, e := range verrs {
			lines[i] = e.Error()
		}
		return lines
	}

	// パースに失敗した場合は 1 件だけ。JSON として読めなかった旨を渡す。
	return []string{err.Error()}
}

// interpretationSystemPrompt は解釈の役割と出力形式を伝えるシステム指示。
//
// **制約一覧は domain.Rules から組み立てる。** 手で書くと、検査を足したときに
// 片方だけ古くなり、指示していない制約で弾くことになる（ADR 0028）。ここが持つ
// のは役割・出力形式・再送の枠組みだけ。
var interpretationSystemPrompt = fmt.Sprintf(interpretationSystemPromptTemplate,
	domain.InterpretationConstraints())

// interpretationSystemPromptTemplate の %s に制約一覧が入る。
const interpretationSystemPromptTemplate = `あなたはホワイトボード上のブレスト内容を読み、開発タスクへ整理する担当です。

与えられるのは、ホワイトボード上で開発者が囲んだ 1 つの範囲に含まれるテキストです。
ブレスト中に書かれたものなので、粒度も表現もばらばらです。これを GitHub の
draft issue に落とせる形へ整理してください。

出力は次の形の JSON だけにしてください。前置き・説明文・コードフェンスを付けないでください。

{
  "summary": "この範囲をどう解釈したかの説明",
  "items": [
    {"localId": "e1", "kind": "epic",  "title": "...", "body": "...", "parentLocalId": null,  "previousRef": null},
    {"localId": "i1", "kind": "issue", "title": "...", "body": "...", "parentLocalId": "e1", "previousRef": "p1"}
  ]
}

制約:

%s- 上に挙げたフィールド以外を出力しないでください。
`
