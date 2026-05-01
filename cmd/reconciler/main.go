// Package main 消込ワーカーの起動エントリポイント。
// M1 段階では骨組みのみ。M8 で Reconciliation モジュールのワーカーを組み込む。
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
	logger.Info("reconciler: boot ( M1 placeholder ) ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("reconciler: shutdown ( M1 placeholder ) ")
}
