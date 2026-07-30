// Command etoki は etoki サーバーの起動とマイグレーション適用を行う。
//
// クラウド固有のアダプタが必要な場合、利用者はこの main を写して
// 独自の実装を差し込む。そのためここは配線だけに留め、ロジックを置かない。
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	srv, err := etoki.New(etoki.Options{
		Addr: os.Getenv("ETOKI_ADDR"),
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki listening on http://%s\n", srv.Addr())

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
