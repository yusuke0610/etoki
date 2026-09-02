// Package etoki は etoki サーバーの組み立て口を提供する。
//
// このパッケージと port パッケージだけが internal/ の外に置かれている。
// クラウド固有の認証が必要な環境では、利用者が port のインターフェースを
// 実装したうえで自前の main からこのパッケージを呼び、独自のアダプタを
// 差し込む想定になっている。Go の internal/ は他モジュールから import
// できないため、公開面をここに切り出す必要がある。
package etoki

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/yusuke0610/etoki/internal/httpapi"
	"github.com/yusuke0610/etoki/internal/usecase"
	"github.com/yusuke0610/etoki/port"
)

// 種別・親を表すカスタムフィールド名の既定値。
//
// usecase の定数を公開面に出している。cmd/etoki を写して独自の main を書く
// 利用者は internal/ を import できないため、ここから参照できる必要がある。
const (
	DefaultKindFieldName   = usecase.DefaultKindFieldName
	DefaultParentFieldName = usecase.DefaultParentFieldName
)

// LLMLimits は LLM を叩く実行の上限（ADR 0044）。
//
// 実体はユースケース層の型。別名で公開しているのは、cmd/etoki を写して独自の
// main を書く利用者が internal/ を import できないため（ADR 0001）。
type LLMLimits = usecase.LLMLimits

// 実行の上限の既定値。**回数の上限には既定値が無い。** 未設定なら無制限で、
// 妥当な値は料金プランと使い方という外の世界の値で決まる（ADR 0044）。
const (
	DefaultLLMMaxConcurrent = usecase.DefaultLLMMaxConcurrent
	DefaultLLMRateWindow    = usecase.DefaultLLMRateWindow
)

// DefaultAddr は Options.Addr が空のときに使うリッスンアドレス。
//
// etoki は単一ユーザーがローカルで動かすツールであり認証機構を持たない。
// 既定でループバックだけにバインドするのはそのためで、公開インターフェースに
// バインドしたい場合は利用者が明示的に Addr を指定する必要がある。
const DefaultAddr = "127.0.0.1:8080"

// shutdownTimeout は graceful shutdown で処理中のリクエストを待つ上限。
const shutdownTimeout = 10 * time.Second

// Options は Server の組み立てに必要な設定と依存を束ねる。
//
// リポジトリを引数で受け取るのは、利用者が独自の実装を差し込めるようにする
// ため。etoki 本体は SQLite を前提としない。
type Options struct {
	// Addr はリッスンアドレス。空なら DefaultAddr を使う。
	Addr string
	// Boards はボードの永続化。必須。
	Boards port.BoardRepository
	// Mappings は注釈と draft issue の対応の永続化。必須。
	Mappings port.MappingRepository

	// LLM は注釈を解釈させる Vision LLM。任意。
	//
	// nil でも起動する。その場合、解釈のエンドポイントだけが「設定されていない」
	// と返し、ボードの編集と状態表示は使える。ブレストだけ先にやる使い方を
	// 潰さないため（ADR 0008）。
	LLM port.LLMClient

	// GitHub は draft issue を作る先。任意。
	//
	// nil でも起動する。その場合、作成と作成先の一覧だけが「設定されていない」
	// と返す。LLM と同じ扱い（ADR 0008）。
	//
	// どの Projects v2 に作るかはボードが持つ（ADR 0014）。ここでは指定しない。
	GitHub port.GitHubClient

	// KindFieldName と ParentFieldName は種別・親を表すカスタムフィールドの名前。
	// 空なら usecase の既定を使う。
	KindFieldName   string
	ParentFieldName string

	// Auth はログインとセッション。任意。NewAuthenticator で作る。
	//
	// nil なら認証しない。いまの単一ユーザー動作のままになる。指定すると
	// 全 API がログインを要求し、GitHub は利用者ごとのトークンで叩く
	// （ADR 0015）。
	//
	// GitHub クライアントの TokenSource にも同じものを渡す。呼び出し側で
	// 組み立ててもらうのはそのためで、ここで作ると 2 つできてしまう。
	Auth *Authenticator

	// WebDir はビルド済みフロントエンド（web/dist）の置き場所。任意。
	//
	// 空なら画面を配らない。make dev では Vite が同じものを持っているので、
	// 両方が配ると画面の出どころが構成によって変わる（ADR 0032）。
	//
	// 指定するとその場で index.html の有無を確かめ、無ければ New が落ちる。
	// 「設定したのに配られない」を実行時の 404 に持ち越さないため。
	WebDir string

	// PublicURL は認可から戻ってくる先の組み立てに使う。任意。
	//
	// 空ならリクエストの Host から組む。リバースプロキシの背後など、
	// 届く Host が外から見た URL と違う場合に指定する。
	PublicURL string

	// Logger はリクエストとエラーの記録先。nil なら slog の既定を使う。
	Logger *slog.Logger

	// LLMLimits は LLM を叩く実行の上限（ADR 0044）。任意。
	//
	// 解釈と図のドラフト生成で 1 つの枠を共有し、利用者ごとに数える。
	// MaxConcurrent の 0 は既定（DefaultLLMMaxConcurrent）、RateLimit の 0 は
	// 無制限。**矛盾した設定はここで落とす。** 負の値と、上限を伴わない窓が
	// 該当する。黙って受けると、設定したつもりの上限が外れる。
	LLMLimits LLMLimits

	// AllowedOrigins はループバック以外に追加で許すオリジン。任意。
	//
	// ブラウザ由来の cross-site リクエストは既定で弾く。ループバックは常に
	// 許すので、既定の構成では空でよい。Addr で公開インターフェースに
	// バインドしたときだけ、そのオリジンを足す必要がある（ADR 0013）。
	AllowedOrigins []string
}

// Authenticator はログインとセッションを担う。
//
// 実体はユースケース層の型。別名で公開しているのは、cmd/etoki を写して独自の
// main を書く利用者が internal/ を import できないため（ADR 0001）。
type Authenticator = usecase.AuthService

// NewAuthenticator は Authenticator を作る。
//
// 返り値は port.GitHubTokenSource でもある。GitHub クライアントの
// Config.TokenSource と Options.Auth の両方に、同じものを渡す。別々に作ると
// トークン更新の直列化が効かなくなる（ADR 0015）。
func NewAuthenticator(
	provider port.IdentityProvider, sessions port.SessionRepository,
) (*Authenticator, error) {
	if provider == nil {
		return nil, errors.New("etoki: identity provider is required")
	}
	if sessions == nil {
		return nil, errors.New("etoki: session repository is required")
	}
	return usecase.NewAuthService(provider, sessions), nil
}

// Server は etoki の HTTP サーバー。
type Server struct {
	addr    string
	handler http.Handler
}

// New は Options を検証し Server を組み立てる。
func New(opts Options) (*Server, error) {
	addr := opts.Addr
	if addr == "" {
		addr = DefaultAddr
	}
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return nil, fmt.Errorf("invalid addr %q: %w", addr, err)
	}
	if opts.Boards == nil {
		return nil, errors.New("etoki: Options.Boards is required")
	}
	if opts.Mappings == nil {
		return nil, errors.New("etoki: Options.Mappings is required")
	}
	if opts.WebDir != "" {
		if err := httpapi.CheckWebDir(opts.WebDir); err != nil {
			return nil, fmt.Errorf("etoki: %w", err)
		}
	}

	if err := validateLLMLimits(opts.LLMLimits); err != nil {
		return nil, err
	}

	// 作成先の固定を守るには、固定を判定する側と、判定の前提を崩す側とが
	// 同じ排他を見ている必要がある（usecase.BoardLocks）。
	locks := usecase.NewBoardLocks()

	// 解釈と図のドラフト生成は同じ枠で数える。どちらも同じ鍵で同じモデルを
	// 叩くので、片方だけ絞ると抜け道が残る（ADR 0044）。
	llmLimiter := usecase.NewLLMLimiter(opts.LLMLimits)

	deps := httpapi.Deps{
		Boards:      usecase.NewBoardService(opts.Boards, opts.Mappings, locks),
		Annotations: usecase.NewAnnotationService(opts.Boards, opts.Mappings),
		// GitHub が nil でも組み立てる。確かめられないことは「分からない」と
		// して返るので、権限の表示そのものを落とす理由が無い（ADR 0017）。
		Access:         usecase.NewBoardAccessService(opts.Boards, opts.GitHub, opts.Logger),
		WebDir:         opts.WebDir,
		PublicURL:      opts.PublicURL,
		Logger:         opts.Logger,
		AllowedOrigins: opts.AllowedOrigins,
	}
	deps.Auth = opts.Auth
	// 招待は「誰であるか」が決まって初めて意味を持つ。認証を設定していない
	// 構成は利用者 1 人なので、共有する相手がいない（ADR 0017）。
	if opts.Auth != nil {
		deps.Members = usecase.NewBoardMemberService(opts.Boards, opts.Auth, locks)
	}
	// LLM が無いときはサービスを組み立てない。nil のまま渡し、ハンドラ側で
	// 「設定されていない」と返す。
	if opts.LLM != nil {
		// Mappings も渡す。解釈の入力には「前回この囲みから何を作ったか」も
		// 含まれる（ADR 0026）。
		// ロガーも渡す。解釈は課金を伴う外部呼び出しなので、呼んだ実績は
		// リクエストログと同じ行き先に残す（ADR 0031）。
		deps.Interpretations = usecase.NewInterpretationService(
			opts.Boards, opts.Mappings, opts.LLM, llmLimiter, usecase.WithLogger(opts.Logger))
		// 図のドラフト生成。**Mappings は渡さない。** 入力はプロンプトだけで、
		// 保存済みシーンも前回作ったものも読まない（ADR 0041）。
		deps.Diagrams = usecase.NewDiagramService(
			opts.Boards, opts.LLM, llmLimiter, usecase.WithDiagramLogger(opts.Logger))
	}
	// 作成先はボードごとに持つので、ここで要るのは GitHub クライアントだけ
	// （ADR 0014）。未選択のボードは作成の手前で 422 として止まる。
	if opts.GitHub != nil {
		deps.Creations = usecase.NewCreationService(
			opts.Boards, opts.Mappings, opts.GitHub, locks,
			usecase.WithFieldNames(opts.KindFieldName, opts.ParentFieldName),
		)
		deps.Catalog = usecase.NewGitHubCatalogService(opts.GitHub)
	}

	handler := httpapi.NewRouter(deps)

	return &Server{addr: addr, handler: handler}, nil
}

// validateLLMLimits は実行の上限の設定を見る。
//
// **0 を「無制限」と読ませない**（ADR 0044）。未設定（既定 1）と 0（無制限）で
// 意味が逆向きになるため、設定するなら 1 以上を要求する。無制限にしたい人は
// 指定しないか、大きい数を書く。
//
// **窓だけの設定も落とす。** 回数の上限が無ければ窓は効かないので、受けると
// 設定したつもりの上限が黙って外れる（中核思想 3）。
func validateLLMLimits(l LLMLimits) error {
	if l.MaxConcurrent < 0 {
		return fmt.Errorf("etoki: LLMLimits.MaxConcurrent must be at least 1, got %d",
			l.MaxConcurrent)
	}
	if l.RateLimit < 0 {
		return fmt.Errorf("etoki: LLMLimits.RateLimit must be at least 1, got %d", l.RateLimit)
	}
	if l.RateWindow < 0 {
		return fmt.Errorf("etoki: LLMLimits.RateWindow must be positive, got %s", l.RateWindow)
	}
	if l.RateWindow > 0 && l.RateLimit == 0 {
		return errors.New("etoki: LLMLimits.RateWindow needs RateLimit; " +
			"a window without a limit has no effect")
	}
	return nil
}

// Addr はリッスンアドレスを返す。
func (s *Server) Addr() string { return s.addr }

// Handler は組み立て済みの HTTP ハンドラを返す。
// テストや、利用者が独自のミドルウェアで包みたい場合に使う。
func (s *Server) Handler() http.Handler { return s.handler }

// Run はサーバーを起動し、ctx がキャンセルされたら graceful shutdown する。
// 正常に停止した場合は nil を返す。
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		// ListenAndServe は Shutdown 経由でも ErrServerClosed を返すが、
		// ここに来るのは Shutdown を呼ぶ前なので実質的に起動失敗のみ。
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve on %s: %w", s.addr, err)

	case <-ctx.Done():
		// ctx はすでにキャンセル済みなので、そのまま渡すと Shutdown が
		// 即座に打ち切られる。猶予を持たせるためキャンセルを切り離す。
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
