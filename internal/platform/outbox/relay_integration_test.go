//go:build integration

package outbox_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/transaction"
)

// silentLogger テストノイズを抑えるため /dev/null 相当の slog ロガーを返す。
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// testLogger デバッグ用に t.Log に出すロガーを返す。
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	w := &testWriter{t: t}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type testWriter struct{ t *testing.T }

func (w *testWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Log(string(p))
	return len(p), nil
}

// insertPendingEvent outbox_events に PENDING の行を 1 件作る ( 現在時刻ベース ) 。
func insertPendingEvent(t *testing.T, db *sql.DB, eventType string) string {
	t.Helper()
	ev := makeEvent(t, eventType)
	mgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	if err := mgr.Do(context.Background(), func(ctx context.Context, tx transaction.Tx) error {
		return pub.Publish(ctx, tx, ev)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return ev.ID
}

// insertPendingEventAt 任意の時刻で next_attempt_at と created_at を固定して INSERT する。
// テストで Relay の SetClock と組み合わせて時刻順序を確実に再現するために使う。
func insertPendingEventAt(t *testing.T, db *sql.DB, eventType string, at time.Time) string {
	t.Helper()
	ev := makeEvent(t, eventType)
	mgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisherWithClock(func() time.Time { return at })
	if err := mgr.Do(context.Background(), func(ctx context.Context, tx transaction.Tx) error {
		return pub.Publish(ctx, tx, ev)
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return ev.ID
}

func TestRelay_DispatchesPendingToHandler(t *testing.T) {
	db := setupMySQL(t)
	id := insertPendingEvent(t, db, "TransferRequested")

	r := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 3, Consumer: "test"}, silentLogger())
	var called atomic.Int32
	r.Register("TransferRequested", outbox.HandlerFunc(func(ctx context.Context, e outbox.Event) error {
		if e.ID != id {
			t.Errorf("event id = %q, want %q", e.ID, id)
		}
		called.Add(1)
		return nil
	}))

	if err := r.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if got := called.Load(); got != 1 {
		t.Errorf("handler called = %d, want 1", got)
	}

	var (
		status string
		sentAt sql.NullTime
	)
	if err := db.QueryRow(`SELECT status, sent_at FROM outbox_events WHERE id=?`, id).Scan(&status, &sentAt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "SENT" {
		t.Errorf("status = %q, want SENT", status)
	}
	if !sentAt.Valid {
		t.Errorf("sent_at が NULL")
	}
}

// TestRelay_SkipAttempt_DoesNotIncrementAttemptCount Handler が outbox.ErrSkipAttempt を返した場合、
// attempt_count を増やさず last_error と next_attempt_at だけを更新する。 circuit breaker OPEN 中の
// 再試行で MaxAttempt を消費しないことを保証するために重要。
func TestRelay_SkipAttempt_DoesNotIncrementAttemptCount(t *testing.T) {
	db := setupMySQL(t)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	relayNow := t0.Add(time.Hour)
	id := insertPendingEventAt(t, db, "TransferRequested", t0)

	r := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 5, Consumer: "test"}, silentLogger())
	r.SetClock(func() time.Time { return relayNow })
	r.Register("TransferRequested", outbox.HandlerFunc(func(ctx context.Context, e outbox.Event) error {
		return outbox.ErrSkipAttempt
	}))

	if err := r.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	var (
		status  string
		attempt int
		nextAt  time.Time
		lastErr sql.NullString
	)
	if err := db.QueryRow(`SELECT status, attempt_count, next_attempt_at, last_error FROM outbox_events WHERE id=?`, id).
		Scan(&status, &attempt, &nextAt, &lastErr); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "PENDING" {
		t.Errorf("status = %q, want PENDING", status)
	}
	if attempt != 0 {
		t.Errorf("attempt_count = %d, want 0 ( ErrSkipAttempt は attempt を増やさない ) ", attempt)
	}
	if !nextAt.After(relayNow) {
		t.Errorf("next_attempt_at = %v は %v より未来になっているべき", nextAt, relayNow)
	}
	if !lastErr.Valid || lastErr.String == "" {
		t.Errorf("last_error が空")
	}
}

func TestRelay_BackoffOnHandlerError(t *testing.T) {
	db := setupMySQL(t)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	relayNow := t0.Add(time.Hour)
	id := insertPendingEventAt(t, db, "TransferRequested", t0)

	r := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 5, Consumer: "test"}, testLogger(t))
	r.SetClock(func() time.Time { return relayNow })
	r.Register("TransferRequested", outbox.HandlerFunc(func(ctx context.Context, e outbox.Event) error {
		return errors.New("handler boom")
	}))

	if err := r.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	var (
		status  string
		attempt int
		nextAt  time.Time
		lastErr sql.NullString
	)
	if err := db.QueryRow(`SELECT status, attempt_count, next_attempt_at, last_error FROM outbox_events WHERE id=?`, id).
		Scan(&status, &attempt, &nextAt, &lastErr); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "PENDING" {
		t.Errorf("status = %q, want PENDING ( バックオフ後はまだ PENDING ) ", status)
	}
	if attempt != 1 {
		t.Errorf("attempt = %d, want 1", attempt)
	}
	if !nextAt.After(relayNow) {
		t.Errorf("next_attempt_at = %v は %v より未来になっているべき", nextAt, relayNow)
	}
	if !lastErr.Valid || lastErr.String == "" {
		t.Errorf("last_error が空")
	}
}

func TestRelay_MarksFailedAfterMaxAttempt(t *testing.T) {
	db := setupMySQL(t)

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	id := insertPendingEventAt(t, db, "TransferRequested", t0)

	cfg := outbox.RelayConfig{BatchSize: 10, MaxAttempt: 2, Consumer: "test"}
	r := outbox.NewRelay(db, cfg, silentLogger())
	// 時刻はループごとに進める ( バックオフを待たずに連続で取り出すため ) 。
	tick := atomic.Int64{}
	tick.Store(t0.Add(time.Hour).UnixNano())
	r.SetClock(func() time.Time {
		// 1 回呼ばれるごとに 1 時間進める。
		v := tick.Add(int64(time.Hour))
		return time.Unix(0, v).UTC()
	})
	r.Register("TransferRequested", outbox.HandlerFunc(func(ctx context.Context, e outbox.Event) error {
		return errors.New("always fail")
	}))

	for i := 0; i < cfg.MaxAttempt; i++ {
		if err := r.ProcessBatch(context.Background()); err != nil {
			t.Fatalf("ProcessBatch %d: %v", i, err)
		}
	}

	var (
		status  string
		attempt int
	)
	if err := db.QueryRow(`SELECT status, attempt_count FROM outbox_events WHERE id=?`, id).
		Scan(&status, &attempt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "FAILED" {
		t.Errorf("status = %q, want FAILED", status)
	}
	if attempt < cfg.MaxAttempt {
		t.Errorf("attempt = %d, want >= %d", attempt, cfg.MaxAttempt)
	}
}

func TestRelay_UnregisteredEventTypeIsSkipped(t *testing.T) {
	db := setupMySQL(t)
	id := insertPendingEvent(t, db, "Unknown")

	r := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 3, Consumer: "test"}, silentLogger())
	if err := r.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM outbox_events WHERE id=?`, id).Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "PENDING" {
		t.Errorf("status = %q, want PENDING ( 未登録は据え置き ) ", status)
	}
}

func TestRelay_ConcurrentWorkersProcessExactlyOnce(t *testing.T) {
	db := setupMySQL(t)

	const total = 100
	mgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	ids := make(map[string]struct{}, total)

	if err := mgr.Do(context.Background(), func(ctx context.Context, tx transaction.Tx) error {
		for i := 0; i < total; i++ {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			agg, err := uuid.NewV7()
			if err != nil {
				return err
			}
			ev := outbox.Event{
				ID:            id.String(),
				AggregateType: "transfer",
				AggregateID:   agg.String(),
				EventType:     "ConcurrentTest",
				Payload:       []byte(`{"i":1}`),
				OccurredAt:    time.Now().UTC(),
			}
			if err := pub.Publish(ctx, tx, ev); err != nil {
				return err
			}
			ids[id.String()] = struct{}{}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var calls atomic.Int32
	var mu sync.Mutex
	seen := make(map[string]int, total)
	handler := outbox.HandlerFunc(func(ctx context.Context, e outbox.Event) error {
		calls.Add(1)
		mu.Lock()
		seen[e.ID]++
		mu.Unlock()
		return nil
	})

	makeRelay := func() *outbox.Relay {
		r := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 3, Consumer: "test"}, silentLogger())
		r.Register("ConcurrentTest", handler)
		return r
	}
	r1 := makeRelay()
	r2 := makeRelay()

	var wg sync.WaitGroup
	wg.Add(2)
	loop := func(r *outbox.Relay) {
		defer wg.Done()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if err := r.ProcessBatch(context.Background()); err != nil {
				t.Errorf("ProcessBatch: %v", err)
				return
			}
			if calls.Load() >= int32(total) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	go loop(r1)
	go loop(r2)
	wg.Wait()

	if got := calls.Load(); got != int32(total) {
		t.Errorf("total handler calls = %d, want %d", got, total)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != total {
		t.Errorf("ユニーク処理イベント数 = %d, want %d", len(seen), total)
	}
	for id, c := range seen {
		if _, ok := ids[id]; !ok {
			t.Errorf("未登録の id を処理: %s", id)
		}
		if c != 1 {
			t.Errorf("event %s 処理回数 = %d, want 1", id, c)
		}
	}

	var pending, sent int
	if err := db.QueryRow(`SELECT
			SUM(CASE WHEN status='PENDING' THEN 1 ELSE 0 END),
			SUM(CASE WHEN status='SENT' THEN 1 ELSE 0 END)
		FROM outbox_events WHERE event_type='ConcurrentTest'`).Scan(&pending, &sent); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if pending != 0 {
		t.Errorf("PENDING 残 = %d, want 0", pending)
	}
	if sent != total {
		t.Errorf("SENT 件数 = %d, want %d", sent, total)
	}
}

func TestMarkProcessed_DuplicateReturnsErrAlreadyProcessed(t *testing.T) {
	db := setupMySQL(t)
	ctx := context.Background()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	now := time.Now().UTC()
	if err := outbox.MarkProcessed(ctx, db, id.String(), "consumer-a", now); err != nil {
		t.Fatalf("first MarkProcessed: %v", err)
	}
	err = outbox.MarkProcessed(ctx, db, id.String(), "consumer-a", now)
	if !errors.Is(err, outbox.ErrAlreadyProcessed) {
		t.Errorf("err = %v, want ErrAlreadyProcessed", err)
	}
	// consumer が違えば別レコードとして登録できる。
	if err := outbox.MarkProcessed(ctx, db, id.String(), "consumer-b", now); err != nil {
		t.Errorf("different consumer should succeed: %v", err)
	}
}
