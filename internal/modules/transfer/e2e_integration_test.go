//go:build integration

package transfer_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"go-sunabar-payments/internal/modules/transfer"
	transferdomain "go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/idempotency"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	mocksunabar "go-sunabar-payments/internal/platform/sunabar/mock"
	"go-sunabar-payments/internal/platform/transaction"
)

// tlogWriter slog の出力を t.Log にリダイレクトする io.Writer 。
type tlogWriter struct{ t *testing.T }

func (w *tlogWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(string(p))
	return len(p), nil
}

func setupMySQL(t *testing.T) *sql.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	var dsn string
	var cleanup func()
	if env := os.Getenv("INTEGRATION_DB_DSN"); env != "" {
		dsn = env
		cleanup = func() {}
	} else {
		container, err := tcmysql.Run(ctx, "mysql:8.0",
			tcmysql.WithDatabase("payments"),
			tcmysql.WithUsername("app"),
			tcmysql.WithPassword("app"),
		)
		if err != nil {
			t.Skipf("testcontainers mysql 起動失敗: %v", err)
		}
		dsn, err = container.ConnectionString(ctx, "parseTime=true", "multiStatements=true", "loc=UTC")
		if err != nil {
			_ = container.Terminate(context.Background())
			t.Fatalf("connection string: %v", err)
		}
		cleanup = func() { _ = container.Terminate(context.Background()) }
	}
	t.Cleanup(cleanup)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	deadline := time.Now().Add(60 * time.Second)
	for {
		if pingErr := db.PingContext(ctx); pingErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping deadline 超過")
		}
		time.Sleep(500 * time.Millisecond)
	}

	resetSchema(t, db)
	applyMigrations(t, db)
	return db
}

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, s := range []string{
		"DROP TABLE IF EXISTS incoming_transactions",
		"DROP TABLE IF EXISTS invoices",
		"DROP TABLE IF EXISTS virtual_accounts",
		"DROP TABLE IF EXISTS accounts",
		"DROP TABLE IF EXISTS transfers",
		"DROP TABLE IF EXISTS event_processed",
		"DROP TABLE IF EXISTS outbox_events",
		"DROP TABLE IF EXISTS schema_migrations",
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	// internal/modules/transfer/<file> から 3 階層上がリポジトリルート。
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	dir := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	for i := 0; i < len(ups); i++ {
		for j := i + 1; j < len(ups); j++ {
			if ups[i] > ups[j] {
				ups[i], ups[j] = ups[j], ups[i]
			}
		}
	}
	for _, name := range ups {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		body := strings.TrimSpace(string(raw))
		if body == "" {
			continue
		}
		if _, err := db.Exec(body); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

// TestE2E_RequestThenRelaySendsToSunabar HTTP API で振込依頼を作って、
// Relay が mock sunabar に投げるところまで通す。
// 1) POST /transfers で受付 ( status=PENDING + Outbox TransferRequested )
// 2) Relay 1 回ポーリングで mock sunabar.RequestTransfer 呼び出し → status=REQUESTED + applyNo
// 3) 別の Relay ポーリングで mock sunabar.GetTransferStatus が呼ばれ、 mock の挙動で最終的に SETTLED まで進む
func TestE2E_RequestThenRelaySendsToSunabar(t *testing.T) {
	db := setupMySQL(t)

	mockSunabar := mocksunabar.NewServer()
	t.Cleanup(mockSunabar.Close)

	auth, err := sunabar.NewStaticTokenSource("test-token")
	if err != nil {
		t.Fatalf("NewStaticTokenSource: %v", err)
	}
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: mockSunabar.URL, Auth: auth})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}

	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	mod := transfer.New(db, txMgr, pub, client, idGen, time.Now)

	mux := http.NewServeMux()
	transfer.NewHTTPHandler(mod.Service).Register(mux)

	api := httptest.NewServer(mux)
	t.Cleanup(api.Close)

	// 1) HTTP API で振込依頼。
	body := map[string]any{
		"appRequestId":    "e2e-app-1",
		"amount":          1500,
		"sourceAccountId": "ACC0001",
		"destBankCode":    "0033",
		"destBranchCode":  "001",
		"destAccountType": "1",
		"destAccountNum":  "1234567",
		"destAccountName": "ヤマダ タロウ",
	}
	buf, _ := json.Marshal(body)
	res, err := http.Post(api.URL+"/transfers", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("POST /transfers: %v", err)
	}
	if res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d body=%s", res.StatusCode, raw)
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = res.Body.Close()
	if created.Status != string(transferdomain.StatusPending) {
		t.Errorf("created.Status = %q, want PENDING", created.Status)
	}

	// 2) Relay 1 回 ProcessBatch で TransferRequested → mock sunabar に送信し REQUESTED に遷移。
	logger := slog.New(slog.NewTextHandler(&tlogWriter{t: t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	_ = io.Discard
	relay := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 5, Consumer: "e2e"}, logger)
	relay.Register(transferdomain.EventTransferRequested, mod.SendToSunabarHandler)
	relay.Register(transferdomain.EventTransferStatusCheck, mod.CheckStatusHandler)

	if err := relay.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch step1: %v", err)
	}
	got, err := mod.Service.GetTransfer(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if got.Status != transferdomain.StatusRequested {
		t.Fatalf("after step1 status = %s, want REQUESTED", got.Status)
	}
	if got.ApplyNo == nil || *got.ApplyNo == "" {
		t.Errorf("ApplyNo 空")
	}

	// 3) Relay の追加ポーリングで TransferStatusCheckScheduled が消化され、 mock の状態遷移
	//    AcceptedToBank -> Approved -> Settled に従って最終的に SETTLED になる。
	//    ハンドラは未確定で ErrStillInFlight を返すが Relay は backoff+1 attempt で次回投入する。
	//    mock は呼び出しごとに進めるので、 何度か ProcessBatch を呼ぶ + バックオフ待機が必要。
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		// Outbox の next_attempt_at を全件「過去」にして再投入を強制する ( バックオフ短絡 ) 。
		// MySQL セッションタイムゾーン (+09:00) と publisher の UTC 書き込みがズレるため、
		// MySQL の NOW() ではなく UTC_TIMESTAMP(6) を使う ( DB 内の比較も UTC ベース ) 。
		if _, err := db.Exec(`UPDATE outbox_events SET next_attempt_at=UTC_TIMESTAMP(6) WHERE status='PENDING'`); err != nil {
			t.Fatalf("force reset next_attempt_at: %v", err)
		}
		if err := relay.ProcessBatch(context.Background()); err != nil {
			t.Fatalf("ProcessBatch loop: %v", err)
		}
		cur, err := mod.Service.GetTransfer(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("GetTransfer loop: %v", err)
		}
		if cur.Status.IsTerminal() {
			if cur.Status != transferdomain.StatusSettled {
				t.Fatalf("terminal status = %s, want SETTLED", cur.Status)
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("E2E が deadline までに終端へ到達せず")
}
