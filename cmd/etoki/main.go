// Command etoki は etoki サーバーを起動する。
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
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "etoki: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, err := etoki.New(etoki.Options{
		Addr: os.Getenv("ETOKI_ADDR"),
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "etoki listening on http://%s\n", srv.Addr())

	return srv.Run(ctx)
}
