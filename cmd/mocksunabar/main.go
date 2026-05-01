// Package main mock sunabar サーバを固定ポートで起動する補助コマンド。
// ローカル開発で API / Relay と組み合わせて、 実機接続せずに E2E を回すために使う。
//
// 使い方:
//
//	MOCK_SUNABAR_ADDR=:9090 go run ./cmd/mocksunabar
//	SUNABAR_BASE_URL=http://localhost:9090 make run-api
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"go-sunabar-payments/internal/platform/observability"
	"go-sunabar-payments/internal/platform/sunabar/mock"
)

func main() {
	logger := observability.NewLogger(os.Stdout)
	if err := run(logger); err != nil {
		logger.Error("mocksunabar fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	addr := os.Getenv("MOCK_SUNABAR_ADDR")
	if addr == "" {
		addr = ":9090"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	srv := mock.NewServer(mock.WithListener(listener))
	defer srv.Close()

	logger.Info("mock sunabar listening", "url", srv.URL)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("mock sunabar stopping")
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
