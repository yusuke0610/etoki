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
	// nil でも起動する。その場合、作成のエンドポイントだけが「設定されていない」
	// と返す。LLM と同じ扱い（ADR 0008）。
	GitHub port.GitHubClient

	// ProjectID は draft issue を作る Projects v2 の node ID。
	// GitHub を指定するなら必須。
	ProjectID string

	// KindFieldName と ParentFieldName は種別・親を表すカスタムフィールドの名前。
	// 空なら usecase の既定を使う。
	KindFieldName   string
	ParentFieldName string

	// Logger はリクエストとエラーの記録先。nil なら slog の既定を使う。
	Logger *slog.Logger
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

	deps := httpapi.Deps{
		Boards:      usecase.NewBoardService(opts.Boards),
		Annotations: usecase.NewAnnotationService(opts.Boards, opts.Mappings),
		Logger:      opts.Logger,
	}
	// LLM が無いときはサービスを組み立てない。nil のまま渡し、ハンドラ側で
	// 「設定されていない」と返す。
	if opts.LLM != nil {
		deps.Interpretations = usecase.NewInterpretationService(opts.Boards, opts.LLM)
	}
	// 作成先が決まっていなければ組み立てない。projectID が空のまま作ると、
	// 呼ばれてから GitHub 側のエラーになり、原因が設定不足だと分かりにくい。
	if opts.GitHub != nil && opts.ProjectID != "" {
		deps.Creations = usecase.NewCreationService(
			opts.Boards, opts.Mappings, opts.GitHub, opts.ProjectID,
			usecase.WithFieldNames(opts.KindFieldName, opts.ParentFieldName),
		)
	}

	handler := httpapi.NewRouter(deps)

	return &Server{addr: addr, handler: handler}, nil
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
