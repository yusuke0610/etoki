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
	"github.com/yusuke0610/etoki/internal/adapter/sqlite"
)

// defaultDBPath は ETOKI_DB_PATH が未設定のときに使う SQLite ファイル。
const defaultDBPath = "etoki.db"

const usage = `usage:
  etoki           サーバーを起動する
  etoki migrate   マイグレーションを適用する

environment:
  ETOKI_ADDR      リッスンアドレス（既定: ` + etoki.DefaultAddr + `）
  ETOKI_DB_PATH   SQLite ファイルのパス（既定: ` + defaultDBPath + `）
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

	srv, err := etoki.New(etoki.Options{
		Addr:     os.Getenv("ETOKI_ADDR"),
		Boards:   sqlite.NewBoardRepository(db),
		Mappings: sqlite.NewMappingRepository(db),
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	logger.InfoContext(ctx, "listening", slog.String("addr", srv.Addr()), slog.String("db", path))

	return srv.Run(ctx)
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
