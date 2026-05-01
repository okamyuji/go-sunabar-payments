// Package main Outbox リレーワーカーの起動エントリポイント。
// M1 では骨組みのみで Outbox 接続は行わない。M2 で outbox.Relay を組み込む。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("relay: boot ( M1 placeholder ) ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("relay: shutdown ( M1 placeholder ) ")
}
