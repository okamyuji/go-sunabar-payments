// Package main go-sunabar-payments の HTTP API サーバ起動エントリポイント。
// chi 等のサードパーティルーターは使わず、 標準 net/http の ServeMux で構成する。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go-sunabar-payments/internal/modules/transfer"
	"go-sunabar-payments/internal/platform/database"
	"go-sunabar-payments/internal/platform/idempotency"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("api fatal", "err", err)
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

	mod := transfer.New(db, txMgr, pub, client, idGen, time.Now)

	mux := http.NewServeMux()
	transfer.NewHTTPHandler(mod.Service).Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := envOr(os.Getenv("API_ADDR"), ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("api shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("api stopped")
	return nil
}

// envOr str が空文字列なら def を返す ( os.Getenv のフォールバック共通化 ) 。
func envOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
