package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// DefaultLLMCallTimeout は LLM の 1 回の呼び出しを待つ上限の既定値（ADR 0104）。
//
// thinking が有効なモデルは応答までに分単位かかることがある。短く切ると正常な
// 応答を取りこぼす。手元の LLM ではこれでも足りないことがあるので、設定で
// 延ばせるようにしてある。
const DefaultLLMCallTimeout = 5 * time.Minute

// errCallTimedOut は、1 回の呼び出しの上限に当たったことを ctx の原因として持つ印。
//
// 外へは出さない。ジョブの上限や取り消しで切れたのかと分けて、文言を足すために
// だけ使う。
var errCallTimedOut = errors.New("etoki: llm call timed out")

// LLM の呼び出しが失敗した原因のうち、打ち手が違うもの（ADR 0087）。
//
// 解釈と図のドラフト生成の両方で返る。原因が分からない失敗は
// ErrLLMUnavailable（interpretation.go）のまま。
var (
	// ErrLLMTruncated は応答が出力の上限で打ち切られたことを表す。
	//
	// 頼み方では直らない。向こうのコンテキスト長か出力の上限を広げる話で、
	// ErrInterpretationFailed / ErrDiagramFailed（出力の形が違った）とも別。
	ErrLLMTruncated = errors.New("etoki: llm output was truncated")

	// ErrLLMRefused はモデルが応答を拒んだことを表す。設定の誤りではない。
	ErrLLMRefused = errors.New("etoki: llm refused to answer")

	// ErrLLMOverloaded は上流の LLM が混んでいるか、一時的に応答できないことを表す。
	//
	// **ErrRateLimited / ErrConcurrencyLimited とは相手が違う。** あちらは etoki
	// 自身が置いた上限（ADR 0044）で、こちらは向こうが断った。畳むと、画面が
	// 「etoki の設定で決まる上限」と「向こうの混雑」を言い分けられない。
	ErrLLMOverloaded = errors.New("etoki: llm upstream is busy")
)

// llmCallFailures は「呼び出しそのものが失敗した」ことを表す sentinel。
//
// 画面に返す code は原因ごとに分けてある（ADR 0087）が、**記録の出し分けは
// 分けない。** どれも出力を受け取れなかった上流の失敗で、分ける前の
// ErrLLMUnavailable と同じ扱いにする。ここに足し忘れると、その原因だけ記録から
// 消える。
var llmCallFailures = []error{
	ErrLLMUnavailable,
	ErrLLMTruncated,
	ErrLLMRefused,
	ErrLLMOverloaded,
}

// IsLLMCallFailure は err が LLM の呼び出しそのものの失敗かを返す。
//
// 記録を残すのは、解釈と図のドラフト生成で共有するジョブの置き場
// （jobTable.logFailure、ADR 0085 / 0091）。**判定はここに置く。** 記録の側で
// 列挙し直すと、足した原因が記録から消える。
func IsLLMCallFailure(err error) bool {
	for _, s := range llmCallFailures {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

// llmCauses は port の sentinel を、ユースケース層の sentinel に写す表。
var llmCauses = []struct {
	port, usecase error
}{
	{port.ErrLLMTruncated, ErrLLMTruncated},
	{port.ErrLLMRefused, ErrLLMRefused},
	{port.ErrLLMOverloaded, ErrLLMOverloaded},
}

// llmCallError は llm.Complete の失敗を、ユースケース層のエラーに包み直す。
//
// 解釈と図のドラフト生成は**どちらもここを通す。** 片方だけ写すと、同じ
// 打ち切りが解釈では llm_truncated、図では llm_unavailable になる。
//
// **sentinel を入れ子にしない。** port の sentinel を名乗るなら、ユースケース層の
// sentinel だけを %w で包み、元のエラーは %v で文言だけ残す。両方を %w で包むと
// errors.Is が port の sentinel まで辿れてしまい、httpapi の表が「sentinel は
// 入れ子にならない」前提で 1 つに決めている写し先が並び順しだいになる。
//
// 名乗らなければ ErrLLMUnavailable に落とす。外部の LLMClient 実装は名乗らなくて
// よい（port の doc コメント）。こちらは元のエラーを %w のまま包む。
// context.Canceled を辿って、取り消したジョブを障害として記録しないのに使っている
// （jobTable.finish）。
func llmCallError(attempt int, err error) error {
	for _, c := range llmCauses {
		if errors.Is(err, c.port) {
			return fmt.Errorf("%w (attempt %d): %v", c.usecase, attempt, err) //nolint:errorlint // 入れ子にしないため %v で文言だけ残す
		}
	}
	return fmt.Errorf("%w (attempt %d): %w", ErrLLMUnavailable, attempt, err)
}

// llmRetry は 1 回の実行（解釈 1 回、図のドラフト生成 1 回）で LLM を呼ぶ手順。
// T は読み取った結果。
//
// **解釈と図のドラフト生成はどちらもここを通す**（llmCallError と同じ理由）。
// 「呼ぶ前に数える」「1 回ごとに上限を掛ける」「呼び出しの失敗は再送しない」を
// 2 つのループに書くと、片方だけ直したときに、図だけが実績を少なく出す、再送で
// 課金を重ねる、という食い違いになる（ADR 0031 / 0044 / 0087 / 0104）。種類ごとに
// 渡すのは下のものだけで、骨組みは持たせない。
type llmRetry[T any] struct {
	// maxAttempts は呼ぶ回数の上限。初回を含む。
	maxAttempts int
	// callTimeout は 1 回の呼び出しを待つ上限（ADR 0104）。
	//
	// **呼び出しごとに掛ける。** 実行全体に掛けると、1 回ずつは収まる遅い応答が、
	// 再送を重ねたところで切れる。LLMClient の実装に任せると、差し込んだ実装では
	// 効かない。
	callTimeout time.Duration
	// parse は応答を読む。読めなければ、直させる指摘をエラーで返す。
	parse func(text string) (T, error)
	// retryText は直前の出力と指摘を添えた、次に送る本文を組み立てる。
	//
	// VisionRequest は 1 往復ぶんしか運べないので、会話履歴の代わりに元の指示ごと
	// 組み立て直して自己完結させる（ADR 0005）。
	retryText func(previous string, err error) string
	// exhausted は上限まで直らなかったときに包む sentinel。LLM への接続自体は
	// 成功しているので、ErrLLMUnavailable とは分ける。
	exhausted error
}

// run は読める出力が得られるまで、指摘を添えて呼び直す。
//
// 払ったぶんは成否によらず返す。呼び出し側がそれを記録する（logLLMUsage）。
func (r llmRetry[T]) run(
	ctx context.Context, llm port.LLMClient, req port.VisionRequest,
) (T, llmUsage, error) {
	var (
		zero  T
		last  error
		usage llmUsage
	)

	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		// 呼ぶ前に数える。呼び出しが失敗した回も「呼んだ」に含めないと、上限に
		// 対して実際に何回叩いているのかが読めない。
		usage.attempts = attempt

		resp, err := r.callOnce(ctx, llm, req)
		if err != nil {
			// 接続やモデル側の失敗は指摘で直る問題ではないので再送しない。
			return zero, usage, llmCallError(attempt, err)
		}
		usage.addTokens(resp.Usage)

		v, err := r.parse(resp.Text)
		if err == nil {
			return v, usage, nil
		}

		last = err
		req.Text = r.retryText(resp.Text, err)
	}

	return zero, usage, fmt.Errorf("%w (%d attempts): %w", r.exhausted, r.maxAttempts, last)
}

// callOnce は 1 回の上限を掛けて LLM を 1 回呼ぶ。
//
// 上限に当たったら、その値を文言に足す。待ちきれなかったのか向こうが断ったのかを、
// 畳んだ詳細から見分けられるようにするため（どれだけ延ばせばよいかの手掛かりにも
// なる）。元のエラーは %w のまま包み、context.DeadlineExceeded を辿れるように
// しておく。**ジョブの上限や取り消しで切れたときは足さない。** 原因で見分ける。
func (r llmRetry[T]) callOnce(
	ctx context.Context, llm port.LLMClient, req port.VisionRequest,
) (port.VisionResponse, error) {
	callCtx, cancel := context.WithTimeoutCause(ctx, r.callTimeout, errCallTimedOut)
	defer cancel()

	resp, err := llm.Complete(callCtx, req)
	if err != nil && errors.Is(context.Cause(callCtx), errCallTimedOut) {
		return resp, fmt.Errorf("no response within %s (the limit for one call): %w", r.callTimeout, err)
	}
	return resp, err
}

// llmUsage は 1 回の実行で LLM に払ったぶん。
//
// 呼び出し 1 回ぶんではなく**実行 1 回ぶん**を積む。1 回の実行は再送を含めて
// 最大 maxAttempts 回 LLM を呼ぶので、1 回ぶんだけを見ても払った量にならない。
type llmUsage struct {
	// attempts は LLM を呼んだ回数。呼んだ時点で数えるので、その呼び出しが
	// 失敗した場合も含む。
	attempts     int
	inputTokens  int
	outputTokens int
}

// addTokens は 1 回ぶんの報告を積む。**attempts はここでは増やさない。**
// 呼び出しが失敗した回にも数える必要があり、その回はここを通らない。
//
// port.Usage は埋めるかどうかが実装の任意なので、ここに 0 が来るのは
// 「使っていない」ではなく「報告が無い」を意味する（ADR 0031）。足しても
// 変わらないだけなので、区別せずそのまま積む。
func (u *llmUsage) addTokens(usage port.Usage) {
	u.inputTokens += usage.InputTokens
	u.outputTokens += usage.OutputTokens
}

// logLLMUsage は 1 回の実行で LLM に払ったぶんを 1 行で残す。attrs はその実行を
// 名指す属性（ボードと、注釈か図の種類）で、行の先頭に並ぶ。
//
// **保存はしない**（ADR 0031）。表を持つと「誰の実績か」を決める必要があり、
// それは絞り方（ボード単位 / 利用者単位）と揃えて決めるべき問いになる。
//
// **金額にしない。** モデルごとの単価は外の世界の値で、抱えると差し替える
// たびに古くなる。残すのはトークン数と回数まで。
func logLLMUsage(
	ctx context.Context, logger *slog.Logger, msg string, u llmUsage, err error, attrs ...slog.Attr,
) {
	args := make([]any, 0, len(attrs)+4)
	for _, a := range attrs {
		args = append(args, a)
	}
	args = append(args,
		slog.Int("attempts", u.attempts),
		slog.Int("inputTokens", u.inputTokens),
		slog.Int("outputTokens", u.outputTokens),
		// 失敗したぶんも課金されている。成否を載せないと、払った量のうち
		// どれだけが無駄だったのかが読めない。
		slog.Bool("ok", err == nil),
	)
	logger.InfoContext(ctx, msg, args...)
}
