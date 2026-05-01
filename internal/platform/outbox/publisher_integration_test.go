//go:build integration

package outbox_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/transaction"
)

// makeEvent テスト用の Event を 1 件返す。 ID と AggregateID は UUID v7 で発番する。
func makeEvent(t *testing.T, eventType string) outbox.Event {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid v7: %v", err)
	}
	agg, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid v7: %v", err)
	}
	return outbox.Event{
		ID:            id.String(),
		AggregateType: "transfer",
		AggregateID:   agg.String(),
		EventType:     eventType,
		Payload:       []byte(`{"amount":1000}`),
		OccurredAt:    time.Now().UTC(),
	}
}

func TestSQLPublisher_Publish_InsertsRow(t *testing.T) {
	db := setupMySQL(t)
	mgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()

	ctx := context.Background()
	ev := makeEvent(t, "TransferRequested")

	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return pub.Publish(ctx, tx, ev)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var (
		gotStatus  string
		gotAttempt int
		gotPayload []byte
		gotNext    time.Time
	)
	row := db.QueryRowContext(ctx, `SELECT status, attempt_count, payload, next_attempt_at FROM outbox_events WHERE id=?`, ev.ID)
	if err := row.Scan(&gotStatus, &gotAttempt, &gotPayload, &gotNext); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if gotStatus != "PENDING" {
		t.Errorf("status = %q, want PENDING", gotStatus)
	}
	if gotAttempt != 0 {
		t.Errorf("attempt_count = %d, want 0", gotAttempt)
	}
	if string(gotPayload) == "" {
		t.Errorf("payload empty")
	}
	if time.Since(gotNext) > 10*time.Second {
		t.Errorf("next_attempt_at が古すぎる: %v", gotNext)
	}
}

func TestSQLPublisher_Publish_RolledBackOnError(t *testing.T) {
	db := setupMySQL(t)
	mgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()

	ctx := context.Background()
	ev := makeEvent(t, "TransferRequested")

	wantErr := errors.New("intentional rollback")
	gotErr := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		if err := pub.Publish(ctx, tx, ev); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("err = %v, want wraps %v", gotErr, wantErr)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id=?`, ev.ID).Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 0 {
		t.Errorf("ロールバック後に行が残っている: %d", n)
	}
}

func TestSQLPublisher_Publish_MultipleInOneTx(t *testing.T) {
	db := setupMySQL(t)
	mgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()

	ctx := context.Background()
	e1 := makeEvent(t, "TransferRequested")
	e2 := makeEvent(t, "TransferSettled")

	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		if err := pub.Publish(ctx, tx, e1); err != nil {
			return err
		}
		return pub.Publish(ctx, tx, e2)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id IN (?, ?)`, e1.ID, e2.ID).Scan(&n); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if n != 2 {
		t.Errorf("inserted = %d, want 2", n)
	}
}

func TestSQLPublisher_Publish_DefaultsAreSane(t *testing.T) {
	db := setupMySQL(t)
	mgr := transaction.NewSQLManager(db)

	fixed := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pub := outbox.NewSQLPublisherWithClock(func() time.Time { return fixed })

	ctx := context.Background()
	ev := makeEvent(t, "TransferRequested")
	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return pub.Publish(ctx, tx, ev)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	var (
		status, lastErr sql.NullString
		attempt         int
		next, created   time.Time
		sentAt          sql.NullTime
	)
	row := db.QueryRowContext(ctx,
		`SELECT status, attempt_count, next_attempt_at, created_at, sent_at, last_error FROM outbox_events WHERE id=?`, ev.ID)
	if err := row.Scan(&status, &attempt, &next, &created, &sentAt, &lastErr); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !status.Valid || status.String != "PENDING" {
		t.Errorf("status = %v, want PENDING", status)
	}
	if attempt != 0 {
		t.Errorf("attempt = %d, want 0", attempt)
	}
	if !next.Equal(fixed) {
		t.Errorf("next_attempt_at = %v, want %v", next, fixed)
	}
	if !created.Equal(fixed) {
		t.Errorf("created_at = %v, want %v", created, fixed)
	}
	if sentAt.Valid {
		t.Errorf("sent_at は NULL のはず: %+v", sentAt)
	}
	if lastErr.Valid {
		t.Errorf("last_error は NULL のはず: %+v", lastErr)
	}
}
