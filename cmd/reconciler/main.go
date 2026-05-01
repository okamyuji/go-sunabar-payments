// Package main 消込ワーカーの起動エントリポイント。
// 定期的に発行済みバーチャル口座一覧を取得し、 各口座について Reconcile を実行する。
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-sunabar-payments/internal/modules/account"
	accountdomain "go-sunabar-payments/internal/modules/account/domain"
	"go-sunabar-payments/internal/modules/reconciliation"
	"go-sunabar-payments/internal/platform/database"
	"go-sunabar-payments/internal/platform/idempotency"
	"go-sunabar-payments/internal/platform/observability"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

func main() {
	logger := observability.NewLogger(os.Stdout)
	if err := run(logger); err != nil {
		logger.Error("reconciler fatal", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	db, err := database.Open(database.FromEnv())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	auth, err := sunabar.NewStaticTokenSource(envOr(os.Getenv("SUNABAR_ACCESS_TOKEN"), "placeholder-token"))
	if err != nil {
		return err
	}
	baseURL := envOr(os.Getenv("SUNABAR_BASE_URL"), "https://api.sunabar.gmo-aozora.com")
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: baseURL, Auth: auth})
	if err != nil {
		return err
	}

	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	now := time.Now

	accMod := account.New(db, txMgr, client, idGen, now)
	recMod := reconciliation.New(db, txMgr, pub, client, idGen, now)

	interval := durationEnv("RECONCILER_INTERVAL", 1*time.Minute)
	logger.Info("reconciler started", "interval", interval)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("reconciler stopped")
			if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
			return nil
		case <-t.C:
			if err := tick(ctx, logger, accMod, recMod); err != nil {
				logger.Warn("reconcile tick failed", "err", err)
			}
		}
	}
}

// tick 1 周期分の処理。 全バーチャル口座について Reconcile を呼ぶ。
func tick(ctx context.Context, logger *slog.Logger, accMod *account.Module, recMod *reconciliation.Module) error {
	vas, err := accMod.Service.ListVirtualAccounts(ctx)
	if err != nil {
		return err
	}
	for _, va := range vas {
		runOne(ctx, logger, recMod, va)
	}
	return nil
}

func runOne(ctx context.Context, logger *slog.Logger, recMod *reconciliation.Module, va *accountdomain.VirtualAccount) {
	res, err := recMod.Service.Reconcile(ctx, va.VirtualAccountID)
	if err != nil {
		logger.Warn("reconcile failed", "va", va.VirtualAccountID, "err", err)
		return
	}
	if res.NewIncomings == 0 {
		return
	}
	logger.Info("reconcile completed",
		"va", va.VirtualAccountID,
		"new_incomings", res.NewIncomings,
		"invoice_status", invoiceStatus(res),
	)
}

func invoiceStatus(res *reconciliation.ReconcileResult) string {
	if res == nil || res.MatchedInvoice == nil {
		return ""
	}
	return string(res.MatchedInvoice.Status)
}

func envOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

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
