package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/yusuke0610/etoki/internal/domain"
	"github.com/yusuke0610/etoki/port"
)

// 図のドラフト生成に固有のエラー。
var (
	// ErrDiagramFailed は上限まで再送しても mermaid が取り出せなかったことを表す。
	//
	// LLM への接続自体は成功している。ErrLLMUnavailable とは原因も打ち手も
	// 違うので分けてある（解釈の ErrInterpretationFailed と同じ切り方）。
	ErrDiagramFailed = errors.New("etoki: llm output did not contain a diagram")

	// ErrDiagramChatTooLong は会話が上限を超えたことを表す。
	//
	// **入力の誤りではない。** 送られたものは正しく、積み上がりすぎただけ。
	// 打ち手が「送った内容を直す」ではなく「会話をやり直す」になるので、
	// ErrInvalidInput に畳まない（ErrSceneTooLarge と同じ理由）。
	ErrDiagramChatTooLong = errors.New("etoki: diagram chat is too long")
)

// defaultDiagramMaxAttempts は 1 回の生成で LLM を呼ぶ回数の上限。
//
// 初回に加えて指摘つきの再送を 1 回。解釈（defaultMaxAttempts = 3）より
// 少ないのは、**直す相手が「JSON スキーマ」ではなく「mermaid を返したか」
// だけ**で、そこを外す出力は指摘しても直りにくいため。直らない出力を
// 繰り返しても課金が増えるだけで、開発者は会話でやり直せる。
const defaultDiagramMaxAttempts = 2

// DiagramTurn は過去のやりとり 1 往復。
//
// **サーバーは会話を持たない**（ADR 0041）ので、これまでの往復は毎回まるごと
// 送られてくる。組み立て直すのはユースケース層で、port.LLMClient には
// 1 回の呼び出ししか任せない（ADR 0005）。
type DiagramTurn struct {
	// Prompt はそのとき開発者が書いた指示。
	Prompt string
	// Mermaid はそれに対して返した mermaid。
	Mermaid string
}

// DiagramRequest は図のドラフトを 1 つ作るための入力。
type DiagramRequest struct {
	// Kind は何の図を描くか。**指定なしは受け付けない。**
	Kind domain.DiagramKind
	// Prompt は今回の指示。
	Prompt string
	// History はここまでのやりとり。1 回目は空。
	History []DiagramTurn
}

// DiagramDraft は生成した図のドラフト。
//
// **キャンバスには置かない。** 置くのは開発者が見てから決める（中核思想 3）。
// mermaid を Excalidraw の要素にするのはフロントの仕事（ADR 0040）。
type DiagramDraft struct {
	// Kind は要求された図の種類。どの記法で書かれているかがこれで決まる。
	Kind domain.DiagramKind
	// Mermaid は生成された mermaid。コードフェンスは外してある。
	Mermaid string
	// TurnsRemaining はこのあと何往復できるか。
	//
	// **上限に当たってから知らせるのでは遅い。** 会話は積み上がるほど直しが
	// 効きにくくなるので、あと何回かは押す前に見えている必要がある。
	TurnsRemaining int
}

// DiagramService はプロンプトから図のドラフトを生成する。
//
// **サーバーの状態を一切変えない**（ADR 0041）。DB にも GitHub にも触れず、
// 会話も保存しない。確定させるのは常に人間の保存操作。
//
// **usecase.BoardLocks を使わない。** 直列化は「作成先の固定を判定する側と、
// その前提を崩す側」を揃えるための仕組みで、ここは何も書かないので揃える
// 対象が無い。
type DiagramService struct {
	boardGuard
	llm port.LLMClient
	// limits は LLM を叩く実行の上限（ADR 0044）。InterpretationService と
	// **同じものを共有する。** 解釈だけを絞ると、こちらが抜け道として残る。
	limits      *LLMLimiter
	maxAttempts int
	maxTurns    int
	logger      *slog.Logger
}

// DiagramServiceOption は DiagramService の設定を差し替える。
type DiagramServiceOption func(*DiagramService)

// WithDiagramMaxAttempts は LLM を呼ぶ回数の上限を差し替える。0 以下は無視する。
//
// テストが既定の回数だけ無駄に往復するのを避けるために公開している。
func WithDiagramMaxAttempts(n int) DiagramServiceOption {
	return func(s *DiagramService) {
		if n > 0 {
			s.maxAttempts = n
		}
	}
}

// WithDiagramMaxTurns は会話の往復回数の上限を差し替える。0 以下は無視する。
func WithDiagramMaxTurns(n int) DiagramServiceOption {
	return func(s *DiagramService) {
		if n > 0 {
			s.maxTurns = n
		}
	}
}

// WithDiagramLogger は実績の記録先を差し替える。nil は無視する。
//
// 生成は LLM を叩く外部呼び出しで課金を伴うので、呼んだ実績はどこかに残る
// 必要がある（ADR 0031）。
func WithDiagramLogger(l *slog.Logger) DiagramServiceOption {
	return func(s *DiagramService) {
		if l != nil {
			s.logger = l
		}
	}
}

// NewDiagramService は DiagramService を作る。
//
// limits は InterpretationService と同じものを渡す（LLMLimiter を参照）。
func NewDiagramService(
	boards port.BoardRepository, llm port.LLMClient, limits *LLMLimiter,
	opts ...DiagramServiceOption,
) *DiagramService {
	s := &DiagramService{
		boardGuard:  boardGuard{boards: boards},
		llm:         llm,
		limits:      limits,
		maxAttempts: defaultDiagramMaxAttempts,
		maxTurns:    MaxDiagramTurns,
		logger:      slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Generate はプロンプトから図のドラフトを作る。
//
// **保存済みシーンを読まない。** 入力はプロンプトだけなので、解釈と違って
// 「保存してからでないと使えない」という制約が要らない（ADR 0041）。手抜き
// ではなく、副作用の有無から出る非対称。
func (s *DiagramService) Generate(
	ctx context.Context, boardID string, req DiagramRequest,
) (DiagramDraft, error) {
	// 生成は LLM を叩く外部呼び出しなので editor 以上に限る。viewer に許すのは
	// 「閲覧」ではない（ADR 0017、解釈と同じ理由）。
	if _, err := s.access(ctx, boardID, port.RoleEditor); err != nil {
		return DiagramDraft{}, err
	}

	// LLM を叩く前に弾く。弾くと決めた入力で呼ぶと、結果は捨てるのに課金だけ
	// 発生する。
	if err := s.validate(req); err != nil {
		return DiagramDraft{}, err
	}

	// **枠を取るのは LLM を叩く直前**（ADR 0044）。解釈と同じ枠を見るので、
	// 片方で使い切ればもう片方も断られる。
	release, err := s.limits.Acquire(ctx)
	if err != nil {
		return DiagramDraft{}, err
	}
	defer release()

	mermaid, usage, err := s.complete(ctx, req)
	// **失敗しても残す。** 再送して直らなかったぶんも課金されている（ADR 0031）。
	s.logUsage(ctx, boardID, req.Kind, usage, err)
	if err != nil {
		return DiagramDraft{}, err
	}

	return DiagramDraft{
		Kind:    req.Kind,
		Mermaid: mermaid,
		// 今回のぶんを引いた残り。History は「今回より前」なので +1 する。
		TurnsRemaining: s.maxTurns - (len(req.History) + 1),
	}, nil
}

// validate は LLM に渡す前に入力を見る。
func (s *DiagramService) validate(req DiagramRequest) error {
	// 指定なしは受け付けない。**既定に倒さない。** 何の図かは入力そのもので、
	// 勝手に選ぶと開発者が指定したつもりのないものが返る（中核思想 3）。
	if req.Kind == domain.DiagramKindUnspecified {
		return fmt.Errorf("%w: diagram kind is required", ErrInvalidInput)
	}
	if !req.Kind.Valid() {
		return fmt.Errorf("%w: unknown diagram kind %q", ErrInvalidInput, req.Kind)
	}
	// 正規化して空になるものは「空」として弾く。改行だけの指示を渡しても、
	// モデルは何を描けばよいか決められない。
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("%w: prompt is required", ErrInvalidInput)
	}

	// 今回のぶんを足して数える。History は「今回より前」なので、上限ちょうどの
	// 履歴が来たら、その次はもう送れない。
	if turns := len(req.History) + 1; turns > s.maxTurns {
		return fmt.Errorf("%w: %d turns, limit is %d", ErrDiagramChatTooLong, turns, s.maxTurns)
	}
	if n := chatBytes(req); n > MaxDiagramChatBytes {
		return fmt.Errorf("%w: %d bytes, limit is %d",
			ErrDiagramChatTooLong, n, MaxDiagramChatBytes)
	}

	return nil
}

// chatBytes は 1 回の呼び出しに載る会話のバイト数を数える。
//
// 数えるのは送られてきたものそのもので、組み立て後のプロンプト全体ではない。
// 指示文はこちらが持つ固定の長さなので、開発者が減らせるものだけを数える。
func chatBytes(req DiagramRequest) int {
	n := len(req.Prompt)
	for _, t := range req.History {
		n += len(t.Prompt) + len(t.Mermaid)
	}
	return n
}

// logUsage は 1 回の生成で LLM に払ったぶんを残す。
//
// **保存はしない**（ADR 0031）。金額にもしない。残すのはトークン数と回数まで。
func (s *DiagramService) logUsage(
	ctx context.Context, boardID string, kind domain.DiagramKind, u llmUsage, err error,
) {
	s.logger.InfoContext(ctx, "diagram draft llm usage",
		slog.String("boardId", boardID),
		slog.String("diagramKind", string(kind)),
		slog.Int("attempts", u.attempts),
		slog.Int("inputTokens", u.inputTokens),
		slog.Int("outputTokens", u.outputTokens),
		slog.Bool("ok", err == nil),
	)
}

// complete は mermaid が取り出せるまで、指摘を添えて呼び直す。
//
// 払ったぶんは成否によらず返す。呼び出し側がそれを記録する。
func (s *DiagramService) complete(
	ctx context.Context, req DiagramRequest,
) (string, llmUsage, error) {
	base := buildDiagramMessage(req)
	llmReq := port.VisionRequest{
		System: diagramSystemPrompt,
		Text:   base,
		// **画像は渡さない。** 入力はプロンプトだけで、保存済みシーンも
		// キャンバスも読まない（ADR 0041）。port.LLMClient は変えていない。
		Images: nil,
	}

	var last error
	var usage llmUsage

	for attempt := 1; attempt <= s.maxAttempts; attempt++ {
		// 呼ぶ前に数える。失敗した回も「呼んだ」に含めないと、上限に対して
		// 実際に何回叩いているのかが読めない。
		usage.attempts = attempt

		resp, err := s.llm.Complete(ctx, llmReq)
		if err != nil {
			// 接続やモデル側の失敗は指摘で直る問題ではないので再送しない。
			return "", usage, fmt.Errorf("%w (attempt %d): %w", ErrLLMUnavailable, attempt, err)
		}
		usage.addTokens(resp.Usage)

		mermaid, err := extractMermaid(resp.Text, req.Kind)
		if err == nil {
			return mermaid, usage, nil
		}

		last = err
		// VisionRequest は 1 往復ぶんしか運べないので、元の指示ごと組み立て
		// 直して自己完結させる（buildRetryMessage と同じ形）。
		llmReq.Text = buildDiagramRetryMessage(base, resp.Text, err)
	}

	return "", usage, fmt.Errorf("%w (%d attempts): %w", ErrDiagramFailed, s.maxAttempts, last)
}

// extractMermaid は LLM の出力から mermaid 本体を取り出し、頼んだ記法かを見る。
//
// **構文までは検べない。** mermaid のパーサを Go に持ち込むことになるうえ、
// 本当に要るのは「mermaid として読めるか」ではなく「Excalidraw の要素として
// 置けるか」で、それを知っているのは変換器だけ（ADR 0040 / 0041）。ここで
// 半端に検べると、置けないものを通す判定が置ける判定より手前に立つ。
//
// ここが見るのは、指摘すれば直る 2 つだけ。**何も返さなかった**ことと、
// **頼んだのと違う記法で書いた**こと。
func extractMermaid(text string, kind domain.DiagramKind) (string, error) {
	body := stripCodeFence(text)
	if body == "" {
		return "", errors.New("mermaid が含まれていません")
	}

	n, ok := diagramNotations[kind]
	if !ok {
		// 種類ごとの記法が無いのは配線の漏れ。TestDiagramNotations_CoverAllKinds が
		// 落とすので、ここに来るのはその表を通らない値だけ。
		return "", fmt.Errorf("no notation for diagram kind %q", kind)
	}
	if !startsWithAny(body, n.headers) {
		return "", fmt.Errorf("%s で書いてください（先頭が %q ではありません）",
			n.name, n.headers[0])
	}

	return body, nil
}

// stripCodeFence はコードフェンスの中身を取り出す。フェンスが無ければそのまま。
//
// 「mermaid だけを返せ」と指示しても ```mermaid で包まれてくることがある。
// それを指摘して再送させるのは往復の無駄なので、機械的に落とせるものは
// ここで落とす（extractJSON と同じ扱い）。
func stripCodeFence(text string) string {
	s := strings.TrimSpace(text)

	after, found := strings.CutPrefix(s, "```")
	if !found {
		return s
	}

	// 開きフェンスの言語指定（```mermaid など）は行末まで捨てる。
	if _, rest, ok := strings.Cut(after, "\n"); ok {
		s = rest
	} else {
		s = after
	}
	if before, _, ok := strings.Cut(s, "```"); ok {
		s = before
	}

	return strings.TrimSpace(s)
}

// startsWithAny は本文の最初の行が、いずれかの語で始まるかを返す。
func startsWithAny(body string, prefixes []string) bool {
	first := body
	if before, _, ok := strings.Cut(body, "\n"); ok {
		first = before
	}
	first = strings.TrimSpace(first)

	for _, p := range prefixes {
		if strings.HasPrefix(first, p) {
			return true
		}
	}
	return false
}

// buildDiagramRetryMessage は直前の出力と指摘を添えた再送メッセージを組み立てる。
func buildDiagramRetryMessage(base, previous string, err error) string {
	var b strings.Builder

	b.WriteString(base)
	b.WriteString("\n---\n\n")
	b.WriteString("直前の出力は次の点で誤っています。指摘を直したうえで、")
	b.WriteString("mermaid をもう一度出力してください。\n\n")
	b.WriteString("直前の出力:\n")
	b.WriteString(previous)
	b.WriteString("\n\n指摘:\n- ")
	b.WriteString(err.Error())
	b.WriteString("\n")

	return b.String()
}
