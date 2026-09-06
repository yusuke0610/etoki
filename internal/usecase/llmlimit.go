package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 実行の上限に固有のエラー。**2 つに分けてある。**
//
// ステータスはどちらも 429 だが、打ち手が違う。時間をおく話と、いま走っている
// ぶんの終わりを待つ話で、待つ長さも次にすることも違う（ADR 0034 / 0044）。
var (
	// ErrRateLimited は窓の中の実行回数が上限に達したことを表す。
	ErrRateLimited = errors.New("etoki: llm call rate limit reached")

	// ErrConcurrencyLimited は同時に走っている実行が上限に達したことを表す。
	//
	// **上流の LLM が混んでいることではない。** そちらは ErrLLMUnavailable。
	// 混同すると、画面が「設定を確かめる」と「少し待つ」を言い分けられない。
	ErrConcurrencyLimited = errors.New("etoki: too many concurrent llm calls")
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
}

// LLMLimiter は LLM を叩く実行の上限を、利用者ごとに見る。
//
// **絞る軸は利用者**（ADR 0044）。ボード単位だと招待した相手が自分のボードを
// 作れば素通りし、プロセス全体だと他人の使いすぎで自分が止まる。空文字は
// 「認証なしの操作者」1 人なので（ADR 0016）、認証を設定していない構成では
// 結果としてプロセス全体の上限と一致する。
//
// **解釈と図のドラフト生成で 1 つを共有する。** どちらも同じ鍵で同じモデルを
// 叩き、同じ請求に乗るので、片方だけ絞ると抜け道が残る。
//
// **状態はメモリだけに持つ。** 再起動すれば消える。依っているのは BoardLocks と
// 同じ前提、つまり SQLite のファイルを複数プロセスで共有する構成を想定して
// いないこと。窓から出た回数は捨てるので、これは実績の記録ではない。実績を
// 保存しない判断（ADR 0031）はそのまま続く。
//
// **BoardLocks を流用しない。** あれは作成先の固定を守るためのボード単位の
// 排他で、軸も目的も違う。
type LLMLimiter struct {
	limits LLMLimits
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
// 見る順は同時実行 → 回数。逆にすると、同時実行で断ったリクエストが回数だけ
// 消費する。**弾いたリクエストはどちらの枠も消費しない。**
func (l *LLMLimiter) Acquire(ctx context.Context) (func(), error) {
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
