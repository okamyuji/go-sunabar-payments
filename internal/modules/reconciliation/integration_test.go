//go:build integration

package reconciliation_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"go-sunabar-payments/internal/modules/reconciliation"
	"go-sunabar-payments/internal/modules/reconciliation/application"
	"go-sunabar-payments/internal/modules/reconciliation/domain"
	"go-sunabar-payments/internal/platform/idempotency"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	mocksunabar "go-sunabar-payments/internal/platform/sunabar/mock"
	"go-sunabar-payments/internal/platform/transaction"
)

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
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	dir := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
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

func newReconciliationModule(t *testing.T, db *sql.DB) *reconciliation.Module {
	t.Helper()
	mock := mocksunabar.NewServer()
	t.Cleanup(mock.Close)

	auth, err := sunabar.NewStaticTokenSource("test-token")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: mock.URL, Auth: auth})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	return reconciliation.New(db, txMgr, pub, client, idGen, time.Now)
}

func TestReconciliation_IssueInvoice(t *testing.T) {
	db := setupMySQL(t)
	mod := newReconciliationModule(t, db)

	inv, err := mod.Service.IssueInvoice(context.Background(), application.IssueInvoiceCommand{
		Amount:           1000,
		VirtualAccountID: "VA-test-1",
		Memo:             "memo",
	})
	if err != nil {
		t.Fatalf("IssueInvoice: %v", err)
	}
	if inv.Status != domain.InvoiceOpen {
		t.Errorf("status = %s, want OPEN", inv.Status)
	}

	// 同一 VA への 2 度目の発行は失敗。
	_, err = mod.Service.IssueInvoice(context.Background(), application.IssueInvoiceCommand{
		Amount:           1000,
		VirtualAccountID: "VA-test-1",
	})
	if !errors.Is(err, domain.ErrInvoiceAlreadyExists) {
		t.Errorf("err = %v, want ErrInvoiceAlreadyExists", err)
	}
}

func TestReconciliation_ReconcileWithMockSunabar(t *testing.T) {
	db := setupMySQL(t)
	mod := newReconciliationModule(t, db)

	// 請求を 10000 円で発行 ( mock の handleTransactions は 10000 円の credit を返す ) 。
	inv, err := mod.Service.IssueInvoice(context.Background(), application.IssueInvoiceCommand{
		Amount:           10000,
		VirtualAccountID: "VA-test-r",
	})
	if err != nil {
		t.Fatalf("IssueInvoice: %v", err)
	}

	res, err := mod.Service.Reconcile(context.Background(), inv.VirtualAccountID)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.NewIncomings != 1 {
		t.Errorf("NewIncomings = %d, want 1", res.NewIncomings)
	}
	if res.MatchedInvoice == nil {
		t.Fatalf("MatchedInvoice = nil")
	}
	if res.MatchedInvoice.Status != domain.InvoiceCleared {
		t.Errorf("status = %s, want CLEARED ( 請求 == 入金 ) ", res.MatchedInvoice.Status)
	}

	// Outbox に ReconciliationCompleted が積まれている。
	row := db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type=?`, domain.EventReconciliationCompleted)
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 1 {
		t.Errorf("Outbox event = %d, want 1", n)
	}

	// 2 回目の Reconcile は incoming が重複しないので NewIncomings = 0 。
	res2, err := mod.Service.Reconcile(context.Background(), inv.VirtualAccountID)
	if err != nil {
		t.Fatalf("Reconcile 2 nd: %v", err)
	}
	if res2.NewIncomings != 0 {
		t.Errorf("2 回目 NewIncomings = %d, want 0 ( item_key 重複 ) ", res2.NewIncomings)
	}
}
