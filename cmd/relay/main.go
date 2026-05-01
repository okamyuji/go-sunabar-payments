// Package main Outbox リレーワーカーの起動エントリポイント。
// outbox.Relay を起動し、 Transfer モジュールの 2 ハンドラ ( SendToSunabar / CheckStatus ) を登録する。
// M5 で Notification ハンドラが追加されると、 ここに登録が増える。
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go-sunabar-payments/internal/modules/notification"
	"go-sunabar-payments/internal/modules/transfer"
	transferdomain "go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/database"
	"go-sunabar-payments/internal/platform/idempotency"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("relay fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	db, err := database.Open(database.FromEnv())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	authToken := os.Getenv("SUNABAR_ACCESS_TOKEN")
	baseURL := os.Getenv("SUNABAR_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.sunabar.gmo-aozora.com"
	}
	auth, err := sunabar.NewStaticTokenSource(envOr(authToken, "placeholder-token"))
	if err != nil {
		return err
	}
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: baseURL, Auth: auth})
	if err != nil {
		return err
	}

	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	transferMod := transfer.New(db, txMgr, pub, client, idGen, time.Now)

	cfg := outbox.RelayConfig{
		PollInterval: durationEnv("OUTBOX_POLL_INTERVAL", 2*time.Second),
		BatchSize:    intEnv("OUTBOX_BATCH_SIZE", 100),
		MaxAttempt:   intEnv("OUTBOX_MAX_ATTEMPT", 50),
		Consumer:     envOr(os.Getenv("OUTBOX_CONSUMER"), "gsp-relay"),
	}
	relay := outbox.NewRelay(db, cfg, logger)
	relay.Register(transferdomain.EventTransferRequested, transferMod.SendToSunabarHandler)
	relay.Register(transferdomain.EventTransferStatusCheck, transferMod.CheckStatusHandler)

	notifMod := notification.New(db, logger, time.Now)
	for _, et := range notifMod.SubscribedEventTypes() {
		relay.Register(et, notifMod.TransferEventHandler)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("relay started", "poll_interval", cfg.PollInterval, "batch", cfg.BatchSize)
	if err := relay.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("relay stopped")
	return nil
}

// envOr str が空文字列なら def を返す。
func envOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// durationEnv 環境変数を time.Duration として読む。 失敗 / 未設定はデフォルト。
func durationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// intEnv 環境変数を int として読む。 失敗 / 未設定はデフォルト。
func intEnv(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
