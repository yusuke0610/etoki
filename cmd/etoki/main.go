// Command etoki は etoki サーバーの起動とマイグレーション適用を行う。
//
// クラウド固有のアダプタが必要な場合、利用者はこの main を写して
// 独自の実装を差し込む。そのためここは配線だけに留め、ロジックを置かない。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"github.com/yusuke0610/etoki"
	"github.com/yusuke0610/etoki/internal/adapter/llm"
	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
	"github.com/yusuke0610/etoki/port"
)

// defaultDBPath は ETOKI_DB_PATH が未設定のときに使う SQLite ファイル。
const defaultDBPath = "etoki.db"

const usage = `usage:
  etoki           サーバーを起動する
  etoki migrate   マイグレーションを適用する

environment:
  ETOKI_ADDR            リッスンアドレス（既定: ` + etoki.DefaultAddr + `）
  ETOKI_DB_PATH         SQLite ファイルのパス（既定: ` + defaultDBPath + `）
  ETOKI_LLM_BASE_URL    LLM のエンドポイント（既定: ` + llm.DefaultBaseURL + `）
  ETOKI_LLM_API_KEY     LLM の API キー（認証不要なら未設定でよい）
  ETOKI_LLM_MODEL       モデル ID（既定: ` + llm.DefaultModel + `）

LLM を未設定のままでも起動する。その場合、解釈のエンドポイントだけが
「設定されていない」と返し、ボードの編集と状態表示は使える。
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "etoki: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// サブコマンドは 1 つだけなので flag パッケージは使わない。増えたら見直す。
	args := os.Args[1:]
	switch {
	case len(args) == 0:
		return serve(ctx)
	case args[0] == "migrate":
		return migrate(ctx)
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func serve(ctx context.Context) error {
	// gin 既定のリクエストログは使わず slog に寄せるので、debug 出力も止める。
	gin.SetMode(gin.ReleaseMode)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// SQLite は接続時にファイルを作るため、未初期化でも起動できてしまう。
	// そのまま動かすと全 API が 500 を返し続けて原因が分かりにくいので、
	// ここで落とす。適用は明示的な操作にしておきたいので自動では行わない。
	if err := sqlite.EnsureMigrated(ctx, db); err != nil {
		if errors.Is(err, sqlite.ErrNotMigrated) {
			return fmt.Errorf("%w\n  先に `make migrate` を実行してください (%s)", err, path)
		}
		return err
	}

	llmClient, err := newLLMClient()
	if err != nil {
		return err
	}

	srv, err := etoki.New(etoki.Options{
		Addr:     os.Getenv("ETOKI_ADDR"),
		Boards:   sqlite.NewBoardRepository(db),
		Mappings: sqlite.NewMappingRepository(db),
		LLM:      llmClient,
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "listening",
		slog.String("addr", srv.Addr()),
		slog.String("db", path),
		slog.Bool("llm", llmClient != nil),
	)

	return srv.Run(ctx)
}

// newLLMClient は環境変数から LLM クライアントを組み立てる。
//
// エンドポイントも鍵も未設定なら nil を返す。「設定していない」と「設定を
// 間違えた」を区別したいため。nil のとき解釈は 503 で「設定されていない」と
// 返り、鍵だけ誤っている場合は呼び出し時に 401 として返る。
//
// 鍵の有無では判断できない。認証不要のローカルエンドポイントに向けるとき、
// 鍵は空のままで正しい（ADR 0008）。
func newLLMClient() (port.LLMClient, error) {
	cfg := llm.ConfigFromEnv()
	if cfg.BaseURL == "" && cfg.APIKey == "" {
		return nil, nil
	}

	// BaseURL の綴り間違いはここで落ちる。実行時まで持ち越さない。
	c, err := llm.New(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func migrate(ctx context.Context) error {
	path := dbPath()

	db, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := sqlite.Migrate(ctx, db); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki: migrated %s\n", path)

	return nil
}

func dbPath() string {
	if p := os.Getenv("ETOKI_DB_PATH"); p != "" {
		return p
	}
	return defaultDBPath
}
