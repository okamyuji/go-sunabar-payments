// Package main go-sunabar-payments の HTTP API サーバ起動エントリポイント。
// M1 段階では骨組みのみ。M4 以降で chi 相当 ( 標準 net/http の ServeMux ) と
// Transfer モジュールの HTTP ハンドラを組み込む。
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
	logger.Info("api: boot ( M1 placeholder ) ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("api: shutdown ( M1 placeholder ) ")
}
