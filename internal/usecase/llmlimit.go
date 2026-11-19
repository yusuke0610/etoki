package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/yusuke0610/etoki/port"
)

// 実行の上限に固有のエラー。**畳まずに分けてある。**
//
// ステータスはどれも 429 だが、打ち手が違う。時間をおく話、いま走っている
// ぶんの終わりを待つ話、全員の枠が戻る境目（日・月）を待つ話で、待つ長さも次に
// することも違う（ADR 0034 / 0044 / 0102）。
var (
	// ErrRateLimited は窓の中の実行回数が上限に達したことを表す。
	ErrRateLimited = errors.New("etoki: llm call rate limit reached")

	// ErrConcurrencyLimited は同時に走っている実行が上限に達したことを表す。
	//
	// **上流の LLM が混んでいることではない。** そちらは ErrLLMOverloaded。
	// 混同すると、画面が「設定を確かめる」と「少し待つ」を言い分けられない。
	ErrConcurrencyLimited = errors.New("etoki: too many concurrent llm calls")

	// ErrLLMDailyBudgetExhausted は全員の合計で、今日（UTC）ぶんの枠を使い
	// 切ったことを表す（ADR 0102）。
	//
	// **ErrRateLimited に畳まない。** あちらは本人の使いすぎで、こちらは全員に
	// 効く。日と月でも分ける。待つ長さが違う（ADR 0034）。
	ErrLLMDailyBudgetExhausted = errors.New("etoki: llm daily budget exhausted")

	// ErrLLMMonthlyBudgetExhausted は全員の合計で、今月（UTC）ぶんの枠を使い
	// 切ったことを表す（ADR 0102）。
	ErrLLMMonthlyBudgetExhausted = errors.New("etoki: llm monthly budget exhausted")
)

// 実行の上限の既定値。
const (
	// DefaultLLMMaxConcurrent は 1 人が同時に走らせられる実行の数。
	//
	// **既定で効かせている。** 抑えているのは押し間違いの二度押しで、手で
	// 押している限り当たらない。当たっても失うものが無いので、設定を足さない
	// 利用者にも入れてある（ADR 0044）。
	DefaultLLMMaxConcurrent = 1

	// DefaultLLMRateWindow は回数を数える窓の既定。
	//
	// **回数の上限そのものには既定値を置かない。** 妥当な値は料金プランと
	// 使い方で決まる外の世界の値で、etoki が抱えると必ず誰かに合わない
	// （ADR 0044）。窓だけは、上限を設定した人が単位を書かずに済むよう
	// 既定を持つ。
	DefaultLLMRateWindow = time.Hour
)

// LLMLimits は LLM を叩く実行の上限。
//
// 0 は「指定なし」。MaxConcurrent は既定値に、RateLimit は無制限になる。
// **負の値や、上限を伴わない窓のような矛盾した設定は、ここでは弾かない。**
// 組み立て口（etoki.New）が起動時に落とす。上限が黙って外れるより、起動しない
// ほうが気づける（中核思想 3）。
type LLMLimits struct {
	// MaxConcurrent は 1 人が同時に走らせられる実行の数。0 なら
	// DefaultLLMMaxConcurrent。
	MaxConcurrent int
	// RateLimit は RateWindow のあいだに始められる実行の数。0 なら無制限。
	RateLimit int
	// RateWindow は回数を数える窓。0 なら DefaultLLMRateWindow。
	// RateLimit が 0 なら意味を持たない。
	RateWindow time.Duration
	// Budget は全員の合計で LLM に払う量の上限（ADR 0102）。上の 3 つが
	// 利用者ごとなのに対し、こちらは全員に効く。
	Budget LLMBudget
}

// LLMBudget は全員の合計で LLM に払う量の上限（ADR 0102）。0 は「上限なし」。
//
// **入力と出力を別々に持つ。** 単価が大きく違うので、合わせて数えると単価の比を
// 抱えることになる（ADR 0031）。運用者が予算から割って決める。**既定値は
// 置かない**（ADR 0044 の回数と同じ理由）。日と月の境目は UTC。
type LLMBudget struct {
	// DailyInputTokens は 1 日（UTC）に使える入力のトークン数。
	DailyInputTokens int
	// DailyOutputTokens は 1 日（UTC）に使える出力のトークン数。
	DailyOutputTokens int
	// MonthlyInputTokens は 1 か月（UTC）に使える入力のトークン数。
	MonthlyInputTokens int
	// MonthlyOutputTokens は 1 か月（UTC）に使える出力のトークン数。
	MonthlyOutputTokens int
}

// daily は日の上限を 1 つでも持つか。
func (b LLMBudget) daily() bool { return b.DailyInputTokens > 0 || b.DailyOutputTokens > 0 }

// monthly は月の上限を 1 つでも持つか。
func (b LLMBudget) monthly() bool { return b.MonthlyInputTokens > 0 || b.MonthlyOutputTokens > 0 }

// Enabled は上限を 1 つでも持つか。組み立て口（etoki.New）が、数えの置き場所を
// 渡されているかを確かめるのに使う。
func (b LLMBudget) Enabled() bool { return b.daily() || b.monthly() }

// reached は使った量が上限に届いたか。上限 0 は「上限なし」。
//
// **届いたら断る。** 次の 1 回がどれだけ使うかは呼ぶまで分からないので、
// 届く前には断らない。超えるのは、始めた実行のぶんまで（ADR 0102）。
func reached(used, limit int) bool { return limit > 0 && used >= limit }

// LLMLimiter は LLM を叩く実行の上限を見る。利用者ごとの同時実行と回数
// （ADR 0044）と、全員の合計の費用（ADR 0102）。
//
// **絞る軸は利用者**（ADR 0044）。ボード単位だと招待した相手が自分のボードを
// 作れば素通りし、プロセス全体だと他人の使いすぎで自分が止まる。空文字は
// 「認証なしの操作者」1 人なので（ADR 0016）、認証を設定していない構成では
// 結果としてプロセス全体の上限と一致する。
//
// **解釈と図のドラフト生成で 1 つを共有する。** どちらも同じ鍵で同じモデルを
// 叩き、同じ請求に乗るので、片方だけ絞ると抜け道が残る。
//
// **利用者ごとの状態はメモリだけに持つ。** 再起動すれば消える。依っているのは
// BoardLocks と同じ前提、つまり SQLite のファイルを複数プロセスで共有する構成を
// 想定していないこと。窓から出た回数は捨てるので、これは実績の記録ではない。
//
// **全員の合計（費用の上限、ADR 0102）だけは、数えを usage に置く。** 再起動や
// アイドルの停止で消えると、月の予算を守れない。持つのは日ごとの合計までで、
// 誰が使ったかは持たない。
//
// **BoardLocks を流用しない。** あれは作成先の固定を守るためのボード単位の
// 排他で、軸も目的も違う。
type LLMLimiter struct {
	limits LLMLimits
	// usage は全員の合計の数えの置き場所。nil なら数えず、費用の上限も見ない
	// （上限を持つのに nil な組み立ては etoki.New が断る）。
	usage port.LLMUsageRepository
	// now は時計。テストが窓の経過を待たずに済むよう差し替えられる。
	now func() time.Time

	mu    sync.Mutex
	users map[string]*llmUserUsage
}

// llmUserUsage は 1 人ぶんの、いまの走行数と窓の中の開始時刻。
type llmUserUsage struct {
	// running はいま走っている実行の数。
	running int
	// startedAt は窓の中で始めた実行の時刻。古いものから捨てる。
	//
	// **RateLimit が 0 なら積まない。** 上限が無いのに時刻を溜め続けると、
	// 誰も見ない配列が伸び続ける。
	startedAt []time.Time
}

// LLMLimiterOption は LLMLimiter の設定を差し替える。
type LLMLimiterOption func(*LLMLimiter)

// WithLLMLimiterClock は時計を差し替える。nil は無視する。
//
// 窓が過ぎれば通ることを、実時間を待たずに確かめるために公開している。
func WithLLMLimiterClock(now func() time.Time) LLMLimiterOption {
	return func(l *LLMLimiter) {
		if now != nil {
			l.now = now
		}
	}
}

// WithLLMUsageRepository は全員の合計の数えの置き場所を渡す（ADR 0102）。nil は
// 無視する。渡せば、上限を持たない構成でも実行ごとに積む（使った量を見てから
// 上限を決められるように）。
func WithLLMUsageRepository(usage port.LLMUsageRepository) LLMLimiterOption {
	return func(l *LLMLimiter) {
		if usage != nil {
			l.usage = usage
		}
	}
}

// NewLLMLimiter は LLMLimiter を作る。
//
// **InterpretationService と DiagramService には同じものを渡す。** 別々に
// 持たせると、絞りたいはずの 2 つが別々の枠を持ち、合計では倍叩ける。だから
// 任意の設定ではなくコンストラクタの引数にしてある（BoardLocks と同じ形）。
func NewLLMLimiter(limits LLMLimits, opts ...LLMLimiterOption) *LLMLimiter {
	if limits.MaxConcurrent <= 0 {
		limits.MaxConcurrent = DefaultLLMMaxConcurrent
	}
	if limits.RateLimit < 0 {
		limits.RateLimit = 0
	}
	if limits.RateWindow <= 0 {
		limits.RateWindow = DefaultLLMRateWindow
	}

	l := &LLMLimiter{
		limits: limits,
		now:    time.Now,
		users:  make(map[string]*llmUserUsage),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Acquire は操作者の枠を 1 つ取り、返す関数で解放する。
//
// **呼ぶのは LLM を叩く直前**（ADR 0044）。認可と入力の検証をすべて通ったあとに
// 置く。手前に置くと、404 や 403 で返るリクエストが枠を食い、他人のボード ID を
// 叩くだけで枠を潰せる。
//
// **上限に当たったら待たずに返す。** 待たせると、ブラウザ側では「止まった」のか
// 「並んでいる」のかが見えない。断って理由を返すほうが、状態を見せて選ばせる
// 形になる（中核思想 3）。
//
// 見る順は全員の合計 → 同時実行 → 回数。回数は見たときに消費するので最後に
// 置く。逆にすると、合計か同時実行で断ったリクエストが回数だけ消費する。
// **弾いたリクエストはどの枠も消費しない。** 合計の枠は読むだけで、何も消費しない。
func (l *LLMLimiter) Acquire(ctx context.Context) (func(), error) {
	// **鍵の外で読む。** 数えを読むあいだ、ほかの利用者の Acquire を待たせない。
	// 同時に読んだ実行がそろって通ると、そのぶんだけ上限を超える。始めた実行の
	// ぶんまでの超過は受け入れる（ADR 0102）。
	if err := l.checkBudget(ctx); err != nil {
		return nil, err
	}

	actor := actorOf(ctx)

	l.mu.Lock()
	defer l.mu.Unlock()

	// **時計はロックの中で読む。** 外で読むと、先に読んだ側が後から追記する
	// 順序がありえて、startedAt の並びが昇順でなくなる。
	now := l.now()

	u, ok := l.users[actor]
	if !ok {
		u = &llmUserUsage{}
		// 使い終わっても消さない。増えるのはこのプロセスが解釈を通した人数
		// までで、参照カウントを持つほうが取りこぼしたときの壊れ方が
		// 分かりにくい（BoardLocks と同じ判断）。
		l.users[actor] = u
	}

	if u.running >= l.limits.MaxConcurrent {
		return nil, fmt.Errorf("%w: %d running, limit is %d",
			ErrConcurrencyLimited, u.running, l.limits.MaxConcurrent)
	}

	if l.limits.RateLimit > 0 {
		u.startedAt = withinWindow(u.startedAt, now.Add(-l.limits.RateWindow))
		if len(u.startedAt) >= l.limits.RateLimit {
			return nil, fmt.Errorf("%w: %d calls within %s, limit is %d",
				ErrRateLimited, len(u.startedAt), l.limits.RateWindow, l.limits.RateLimit)
		}
		u.startedAt = append(u.startedAt, now)
	}

	u.running++

	// **解放で回数は戻さない。** 実行は終わっても呼んだ事実は消えないので、
	// 窓から出るまで数え続ける。戻すのは走行数だけ。
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		u.running--
	}, nil
}

// checkBudget は全員の合計が、日か月の上限に届いていないかを見る（ADR 0102）。
//
// **月を先に見る。** 両方に届いているなら、待つ長い方を言う。日の枠が戻っても、
// 月の枠が尽きていれば使えない。
//
// **数えを読めなければ断る**（誤りのまま返す。httpapi では 500）。読めないまま
// 通すと、予算を超えて払う。払ったものは戻らない。
func (l *LLMLimiter) checkBudget(ctx context.Context) error {
	if l.usage == nil {
		return nil
	}

	// 上限を持たない日と月は読まない。既定の構成に、実行ごとの読み込みを足さない。
	b := l.limits.Budget
	now := l.now().UTC()
	if b.monthly() {
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		used, err := l.usage.SumLLMUsage(ctx, month, month.AddDate(0, 1, 0))
		if err != nil {
			return fmt.Errorf("read monthly llm usage: %w", err)
		}
		if reached(used.InputTokens, b.MonthlyInputTokens) ||
			reached(used.OutputTokens, b.MonthlyOutputTokens) {
			return fmt.Errorf("%w: %s used %d input / %d output tokens, limit is %d / %d",
				ErrLLMMonthlyBudgetExhausted, month.Format("2006-01"),
				used.InputTokens, used.OutputTokens, b.MonthlyInputTokens, b.MonthlyOutputTokens)
		}
	}
	if b.daily() {
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		used, err := l.usage.SumLLMUsage(ctx, day, day.AddDate(0, 0, 1))
		if err != nil {
			return fmt.Errorf("read daily llm usage: %w", err)
		}
		if reached(used.InputTokens, b.DailyInputTokens) ||
			reached(used.OutputTokens, b.DailyOutputTokens) {
			return fmt.Errorf("%w: %s used %d input / %d output tokens, limit is %d / %d",
				ErrLLMDailyBudgetExhausted, day.Format("2006-01-02"),
				used.InputTokens, used.OutputTokens, b.DailyInputTokens, b.DailyOutputTokens)
		}
	}
	return nil
}

// spend は 1 回の実行で払ったぶんを、全員の合計に積む（ADR 0102）。
//
// **失敗した実行のぶんも積む。** 再送して直らなかったぶんも課金されている
// （ADR 0031）。
//
// **呼び出し側の ctx から切り離して書く。** 取り消された実行でも、払ったぶんは
// 戻らない（run の記録と同じ、ADR 0051）。書けなかったときは誤りを返し、
// 呼び出し側が記録に残す。結果を返すかどうかは、積めたかに左右させない。
func (l *LLMLimiter) spend(ctx context.Context, u llmUsage) error {
	if l.usage == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), spendTimeout)
	defer cancel()
	return l.usage.AddLLMUsage(ctx, l.now(), port.LLMUsageTotal{
		Executions:   1,
		InputTokens:  u.inputTokens,
		OutputTokens: u.outputTokens,
	})
}

// spendTimeout は払ったぶんを積むのを待つ上限。積むのは 1 文なので、届かない
// ときに実行の後始末をいつまでも待たせない。
const spendTimeout = 5 * time.Second

// withinWindow は境界より前の時刻を落とす。
//
// 先頭から数えるだけで済むのは、Acquire がロックの中で読んだ now を末尾に
// 追記するだけで、並びが昇順に保たれるため。
func withinWindow(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(times) && !times[i].After(cutoff) {
		i++
	}
	return times[i:]
}
