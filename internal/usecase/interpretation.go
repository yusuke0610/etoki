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
var (
	// ErrBoardNotFound は対象のボードが存在しないことを表す。
	ErrBoardNotFound = errors.New("etoki: board not found")
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

// InterpretationService は注釈範囲を LLM に解釈させる。
//
// プロンプト構築・スキーマ検証・修正指示つき再送をこの層が持つ。port.LLMClient
// には 1 回の呼び出ししか任せない（ADR 0005）。
//
// この層は解釈するだけで、GitHub には何も作らず sync_runs にも書かない。
// 何を作るかは解釈結果を見た開発者が別途トリガーする。
type InterpretationService struct {
	boards      port.BoardRepository
	llm         port.LLMClient
	maxAttempts int
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
func NewInterpretationService(boards port.BoardRepository, llm port.LLMClient, opts ...InterpretationServiceOption) *InterpretationService {
	s := &InterpretationService{
		boards:      boards,
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
// 読むのは保存済みシーンである。フロントで編集中の内容は反映されない。
func (s *InterpretationService) Interpret(ctx context.Context, boardID, annotationID string) (domain.Interpretation, error) {
	board, err := s.boards.Find(ctx, boardID)
	if err != nil {
		return domain.Interpretation{}, err
	}
	if board == nil {
		return domain.Interpretation{}, fmt.Errorf("%w: %s", ErrBoardNotFound, boardID)
	}

	scene, err := domain.ParseScene([]byte(board.Scene))
	if err != nil {
		return domain.Interpretation{}, err
	}

	annotation, ok := findAnnotation(scene, annotationID)
	if !ok {
		return domain.Interpretation{}, fmt.Errorf("%w: %s", ErrAnnotationNotFound, annotationID)
	}

	// 粒度が未知の値ならここで止める。既定に倒して解釈を進めると、開発者が
	// 指定したつもりの制約が黙って外れる。状態を見せて選ばせる方針に反する。
	if !annotation.Granularity.Valid() {
		return domain.Interpretation{}, fmt.Errorf("%w: unknown granularity %q on annotation %s",
			ErrInvalidInput, annotation.Granularity, annotationID)
	}

	return s.complete(ctx, annotation, scene.AnnotationTexts(annotationID))
}

// complete は検証を満たす出力が得られるまで、修正指示を添えて呼び直す。
func (s *InterpretationService) complete(ctx context.Context, a domain.Annotation, texts []domain.TextElement) (domain.Interpretation, error) {
	base := buildUserMessage(a, texts)
	req := port.VisionRequest{
		System: interpretationSystemPrompt,
		Text:   base,
		// 画像はまだ渡していない。図形や矢印はここに現れないため、構造は
		// テキストから読める範囲でしか解釈されない。
		Images: nil,
	}

	var last error

	for attempt := 1; attempt <= s.maxAttempts; attempt++ {
		resp, err := s.llm.Complete(ctx, req)
		if err != nil {
			// 接続やモデル側の失敗は修正指示で直る問題ではないので再送しない。
			return domain.Interpretation{}, fmt.Errorf("%w (attempt %d): %w", ErrLLMUnavailable, attempt, err)
		}

		in, err := parseInterpretation(resp.Text, a.Granularity)
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
func parseInterpretation(text string, g domain.Granularity) (domain.Interpretation, error) {
	in, err := domain.ParseInterpretation([]byte(extractJSON(text)))
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
func buildUserMessage(a domain.Annotation, texts []domain.TextElement) string {
	var b strings.Builder

	b.WriteString("囲みに含まれるテキスト:\n")
	if len(texts) == 0 {
		// テキストが無くても解釈は試みる。図形と矢印だけの囲みがありうるため
		// で、その中身は画像でしか渡せない（#9）。ここで打ち切ると、画像を
		// 足したときに解禁し直すことになる。
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

	b.WriteString("\n")
	b.WriteString(granularityInstruction(a.Granularity))
	b.WriteString("\n")

	return b.String()
}

// granularityInstruction は開発者の粒度指定を指示文にする。
func granularityInstruction(g domain.Granularity) string {
	switch g {
	case domain.GranularityEpic:
		return "開発者はこの範囲を epic 相当と指定しています。epic を少なくとも 1 件含めてください。"
	case domain.GranularityIssue:
		return "開発者はこの範囲を issue 相当と指定しています。epic は作らず issue だけを出力してください。"
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
// 制約は domain.Interpretation.Validate と対応している。片方だけ変えると、
// 守れない指示を出したり、指示していない制約で弾いたりすることになる。
const interpretationSystemPrompt = `あなたはホワイトボード上のブレスト内容を読み、開発タスクへ整理する担当です。

与えられるのは、ホワイトボード上で開発者が囲んだ 1 つの範囲に含まれるテキストです。
ブレスト中に書かれたものなので、粒度も表現もばらばらです。これを GitHub の
draft issue に落とせる形へ整理してください。

出力は次の形の JSON だけにしてください。前置き・説明文・コードフェンスを付けないでください。

{
  "summary": "この範囲をどう解釈したかの説明",
  "items": [
    {"localId": "e1", "kind": "epic",  "title": "...", "body": "...", "parentLocalId": null},
    {"localId": "i1", "kind": "issue", "title": "...", "body": "...", "parentLocalId": "e1"}
  ]
}

制約:

- kind は "epic" か "issue" のどちらかです。ほかの値は使えません。
- 階層は epic と issue の 2 段だけです。epic の下に epic は作れません。
- epic の parentLocalId は必ず null です。
- issue の parentLocalId には親となる epic の localId を書きます。どの epic にも
  属さない issue は null にします。
- localId は出力の中で一意にしてください。GitHub 上の ID は作成後にしか決まらない
  ため、親子関係はこの一時 ID で表します。
- title は空にできません。
- summary は GitHub には作りません。解釈が意図どおりかを開発者が確かめるための
  ものなので、何をどうまとめたのかが分かる文にしてください。
- 上に挙げたフィールド以外を出力しないでください。
`
