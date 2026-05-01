//go:build integration

package application_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"go-sunabar-payments/internal/modules/notification/application"
	"go-sunabar-payments/internal/modules/notification/domain"
	transferdomain "go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
)

// inMemorySender テスト用 Sender 実装。 受け取った Notice を蓄積する。
type inMemorySender struct {
	mu      sync.Mutex
	notices []domain.Notice
}

func (s *inMemorySender) Send(_ context.Context, n domain.Notice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notices = append(s.notices, n)
	return nil
}

func (s *inMemorySender) Notices() []domain.Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Notice, len(s.notices))
	copy(out, s.notices)
	return out
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
	// notification/application/<file> から 4 階層上 ( application -> notification -> modules -> internal -> root )
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
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

func makeStateChangedEvent(t *testing.T, eventID, eventType, transferID, applyNo, errMsg string) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(transferdomain.TransferStateChangedPayload{
		TransferID:   transferID,
		FromStatus:   "PENDING",
		ToStatus:     "REQUESTED",
		ApplyNo:      applyNo,
		ErrorMessage: errMsg,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return outbox.Event{
		ID:            eventID,
		AggregateType: "transfer",
		AggregateID:   transferID,
		EventType:     eventType,
		Payload:       payload,
		OccurredAt:    time.Now().UTC(),
	}
}

func TestTransferEventHandler_AcceptedDeliversNotice(t *testing.T) {
	db := setupMySQL(t)
	sender := &inMemorySender{}
	h := application.NewTransferEventHandler(db, sender, time.Now)

	evt := makeStateChangedEvent(t, "11111111-1111-7000-8000-000000000001", transferdomain.EventTransferAcceptedToBank, "tr-1", "AP1", "")
	if err := h.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := sender.Notices()
	if len(got) != 1 {
		t.Fatalf("notices = %d, want 1", len(got))
	}
	if got[0].Kind != domain.KindTransferAccepted {
		t.Errorf("kind = %s, want TRANSFER_ACCEPTED", got[0].Kind)
	}
	if got[0].TransferID != "tr-1" {
		t.Errorf("transfer_id = %s, want tr-1", got[0].TransferID)
	}
}

func TestTransferEventHandler_AwaitingHasApprovalURL(t *testing.T) {
	db := setupMySQL(t)
	sender := &inMemorySender{}
	h := application.NewTransferEventHandler(db, sender, time.Now)

	evt := makeStateChangedEvent(t, "22222222-2222-7000-8000-000000000002", transferdomain.EventTransferAwaitingApproval, "tr-2", "AP2", "")
	if err := h.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := sender.Notices()
	if len(got) != 1 {
		t.Fatalf("notices = %d, want 1", len(got))
	}
	if got[0].ApprovalURL == "" {
		t.Errorf("ApprovalURL 空")
	}
}

func TestTransferEventHandler_DuplicateEventNotSentTwice(t *testing.T) {
	db := setupMySQL(t)
	sender := &inMemorySender{}
	h := application.NewTransferEventHandler(db, sender, time.Now)

	evt := makeStateChangedEvent(t, "33333333-3333-7000-8000-000000000003", transferdomain.EventTransferSettled, "tr-3", "AP3", "")
	if err := h.Handle(context.Background(), evt); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := h.Handle(context.Background(), evt); err != nil {
		t.Fatalf("second: %v", err)
	}
	if got := sender.Notices(); len(got) != 1 {
		t.Errorf("notices = %d, want 1 ( 2 回呼んでも 1 件 ) ", len(got))
	}
}

func TestTransferEventHandler_UnknownEventTypeIsNoOp(t *testing.T) {
	db := setupMySQL(t)
	sender := &inMemorySender{}
	h := application.NewTransferEventHandler(db, sender, time.Now)

	evt := makeStateChangedEvent(t, "44444444-4444-7000-8000-000000000004", "UnknownEventType", "tr-4", "AP4", "")
	if err := h.Handle(context.Background(), evt); err != nil {
		t.Errorf("Handle: %v", err)
	}
	if got := sender.Notices(); len(got) != 0 {
		t.Errorf("notices = %d, want 0 ( 未知 event_type は no-op ) ", len(got))
	}
}

func TestTransferEventHandler_FailedIncludesReason(t *testing.T) {
	db := setupMySQL(t)
	sender := &inMemorySender{}
	h := application.NewTransferEventHandler(db, sender, time.Now)

	evt := makeStateChangedEvent(t, "55555555-5555-7000-8000-000000000005", transferdomain.EventTransferFailed, "tr-5", "AP5", "limit exceeded")
	if err := h.Handle(context.Background(), evt); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := sender.Notices()
	if len(got) != 1 {
		t.Fatalf("notices = %d, want 1", len(got))
	}
	if !strings.Contains(got[0].Body, "limit exceeded") {
		t.Errorf("body = %q ( limit exceeded を含むべき ) ", got[0].Body)
	}
}
