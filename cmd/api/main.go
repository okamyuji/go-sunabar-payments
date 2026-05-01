// Package main go-sunabar-payments の HTTP API サーバ起動エントリポイント。
// chi 等のサードパーティルーターは使わず、 標準 net/http の ServeMux で構成する。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"go-sunabar-payments/internal/platform/observability"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

func main() {
	logger := observability.NewLogger(os.Stdout)
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
	metrics := observability.NewMetrics()

	mod := transfer.New(db, txMgr, pub, client, idGen, time.Now)

	mux := http.NewServeMux()
	transfer.NewHTTPHandler(mod.Service).Register(mux)
	mux.Handle("GET /healthz", healthzHandler(db))
	mux.Handle("GET /metrics", metricsJSONHandler(db, metrics))

	addr := envOr(os.Getenv("API_ADDR"), ":8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           observability.HTTPMiddleware(logger)(mux),
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

// healthzHandler DB 接続の生死を返す軽量エンドポイント。 DB 障害時に 503 を返す。
func healthzHandler(db *sql.DB) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("db unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

// metricsJSONHandler 現在のメトリクス値を JSON で返す。 Prometheus 形式は本マイルストーンでは見送る。
func metricsJSONHandler(db *sql.DB, m *observability.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := m.CollectFromDB(ctx, db); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("collect failed: " + err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/json;charset=UTF-8")
		_ = json.NewEncoder(w).Encode(m.Snapshot())
	})
}

// envOr str が空文字列なら def を返す。
func envOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
