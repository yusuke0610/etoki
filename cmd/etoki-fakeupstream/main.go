// Command etoki-fakeupstream は、etoki を手元で通しで動かすための偽の LLM と
// 偽の GitHub を起動する（ADR 0050）。make try-fake から使う。
//
// 状態はメモリにだけ持つ。再起動すると作った draft issue は消えるので、
// etoki の DB も毎回作り直す前提。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yusuke0610/etoki/internal/fakeupstream"
)

const defaultAddr = "127.0.0.1:8090"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "etoki-fakeupstream: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("FAKE_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           fakeupstream.New(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	slog.InfoContext(ctx, "fake upstream listening",
		slog.String("addr", addr),
		slog.String("items", "http://"+addr+"/_fake/items"),
	)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
