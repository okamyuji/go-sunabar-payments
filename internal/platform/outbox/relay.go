package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrSkipAttempt は Handler が「再投入は望むが attempt_count は増やさないでほしい」 と Relay に伝えるための
// sentinel エラー。 例えば circuit breaker が OPEN で外部呼び出し自体が起きていないケース。
// errors.Is(err, outbox.ErrSkipAttempt) で識別する。
var ErrSkipAttempt = errors.New("outbox: skip attempt count")

// skipAttemptBackoff ErrSkipAttempt 時の再投入遅延。 通常の指数バックオフより短く、
// circuit breaker の reset timeout より十分小さくして HALF_OPEN 復帰を素早く拾う。
const skipAttemptBackoff = 5 * time.Second

// RelayConfig Relay の動作パラメータ。
type RelayConfig struct {
	// PollInterval ポーリング間隔。 0 以下なら 2 秒に補正される。
	PollInterval time.Duration
	// BatchSize 1 ポーリングで取り出す最大件数。 0 以下なら 100 に補正される。
	BatchSize int
	// MaxAttempt この回数 ( 1 始まり ) 以上に失敗したら status='FAILED' に遷移させる。
	MaxAttempt int
	// Consumer event_processed テーブルで識別する受信者名。
	Consumer string
}

// Relay Outbox からイベントを取り出し Handler に配信するワーカー。
type Relay struct {
	db       *sql.DB
	cfg      RelayConfig
	logger   *slog.Logger
	now      func() time.Time
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRelay Relay を生成する。 cfg に未指定値があればデフォルトに補正する。
func NewRelay(db *sql.DB, cfg RelayConfig, logger *slog.Logger) *Relay {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.MaxAttempt <= 0 {
		cfg.MaxAttempt = 10
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Relay{
		db:       db,
		cfg:      cfg,
		logger:   logger,
		now:      time.Now,
		handlers: make(map[string]Handler),
	}
}

// SetClock テスト容易性のため now 関数を差し替える。 nil 渡しは無視される。
func (r *Relay) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	r.now = now
}

// Register event_type に対する Handler を登録する。 同じキーの再登録は上書きする。
func (r *Relay) Register(eventType string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[eventType] = h
}

// Run ポーリングループを開始する。 ctx がキャンセルされるまで動作する。
func (r *Relay) Run(ctx context.Context) error {
	t := time.NewTicker(r.cfg.PollInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := r.ProcessBatch(ctx); err != nil {
				r.logger.Error("relay process batch failed", "err", err)
			}
		}
	}
}

// ProcessBatch 1 回分のバッチを取り出して処理する。 テストから直接呼べるように公開する。
// SELECT ... FOR UPDATE SKIP LOCKED で他ワーカーとの競合を避ける。
// 分離レベルは READ COMMITTED に固定し、 InnoDB の gap lock を抑える。
// gap lock があると handler 内の outbox_events への新規 INSERT が同テーブルへの範囲ロックと衝突して
// Lock wait timeout を起こす ( デフォルトの REPEATABLE READ で発生する典型問題 ) 。
func (r *Relay) ProcessBatch(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("relay begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				r.logger.Error("relay rollback failed", "err", rbErr)
			}
		}
	}()

	const selectQ = `SELECT id, aggregate_type, aggregate_id, event_type, payload, attempt_count, next_attempt_at, created_at
		FROM outbox_events
		WHERE status='PENDING' AND next_attempt_at <= ?
		ORDER BY next_attempt_at
		LIMIT ?
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.QueryContext(ctx, selectQ, r.now().UTC(), r.cfg.BatchSize)
	if err != nil {
		return fmt.Errorf("relay select pending: %w", err)
	}

	type item struct {
		Event   Event
		Attempt int
	}
	var batch []item
	for rows.Next() {
		var (
			e       Event
			attempt int
			next    time.Time
			payload json.RawMessage
		)
		if scanErr := rows.Scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.EventType, &payload, &attempt, &next, &e.OccurredAt); scanErr != nil {
			if closeErr := rows.Close(); closeErr != nil {
				r.logger.Error("relay rows close after scan err", "err", closeErr)
			}
			return fmt.Errorf("relay scan: %w", scanErr)
		}
		e.Payload = []byte(payload)
		batch = append(batch, item{Event: e, Attempt: attempt})
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		if closeErr := rows.Close(); closeErr != nil {
			r.logger.Error("relay rows close after iter err", "err", closeErr)
		}
		return fmt.Errorf("relay rows iter: %w", rowsErr)
	}
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("relay rows close: %w", closeErr)
	}

	for _, b := range batch {
		r.dispatch(ctx, tx, b.Event, b.Attempt)
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("relay commit: %w", commitErr)
	}
	committed = true
	return nil
}

// dispatch 1 イベントを Handler に渡し、結果に応じて status を更新する。
func (r *Relay) dispatch(ctx context.Context, tx *sql.Tx, e Event, attempt int) {
	r.mu.RLock()
	h, ok := r.handlers[e.EventType]
	r.mu.RUnlock()

	if !ok {
		// 未登録 event_type はスキップして PENDING のまま残す。
		r.logger.Warn("relay no handler", "event_id", e.ID, "event_type", e.EventType)
		return
	}

	handleErr := h.Handle(ctx, e)
	if handleErr == nil {
		const upd = `UPDATE outbox_events SET status='SENT', sent_at=?, attempt_count=attempt_count+1
			WHERE id=? AND status='PENDING'`
		if _, err := tx.ExecContext(ctx, upd, r.now().UTC(), e.ID); err != nil {
			r.logger.Error("relay mark sent failed", "event_id", e.ID, "err", err)
		}
		return
	}

	// Handler が ErrSkipAttempt を返した場合は attempt_count を増やさず再投入する。
	// 外部呼び出しが circuit breaker の OPEN で実際には行われていないケースなど、
	// 再試行予算 ( MaxAttempt ) を消費すべきでない場合に使う。
	if errors.Is(handleErr, ErrSkipAttempt) {
		const skipQ = `UPDATE outbox_events SET last_error=?, next_attempt_at=?
			WHERE id=? AND status='PENDING'`
		if _, err := tx.ExecContext(ctx, skipQ, handleErr.Error(), r.now().UTC().Add(skipAttemptBackoff), e.ID); err != nil {
			r.logger.Error("relay skip-attempt update failed", "event_id", e.ID, "err", err)
		}
		r.logger.Warn("relay skipped attempt", "event_id", e.ID, "err", handleErr)
		return
	}

	nextAttempt := attempt + 1
	if nextAttempt >= r.cfg.MaxAttempt {
		const failQ = `UPDATE outbox_events SET status='FAILED', attempt_count=?, last_error=?
			WHERE id=? AND status='PENDING'`
		if _, err := tx.ExecContext(ctx, failQ, nextAttempt, handleErr.Error(), e.ID); err != nil {
			r.logger.Error("relay mark failed update failed", "event_id", e.ID, "err", err)
		}
		r.logger.Error("relay event marked FAILED", "event_id", e.ID, "err", handleErr)
		return
	}

	backoff := backoffDuration(nextAttempt)
	const retryQ = `UPDATE outbox_events SET attempt_count=?, last_error=?, next_attempt_at=?
		WHERE id=? AND status='PENDING'`
	if _, err := tx.ExecContext(ctx, retryQ, nextAttempt, handleErr.Error(), r.now().UTC().Add(backoff), e.ID); err != nil {
		r.logger.Error("relay backoff update failed", "event_id", e.ID, "err", err)
	}
}

// backoffDuration 指数バックオフを返す ( 上限 10 分 ) 。
func backoffDuration(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 30 {
		attempt = 30
	}
	d := time.Duration(1<<attempt) * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// ErrAlreadyProcessed イベントが既に処理済みであることを示す。
var ErrAlreadyProcessed = errors.New("event already processed")

// MarkProcessed 受信側冪等性管理用ヘルパ。 Handler 先頭での呼び出しを想定する。
// ( event_id, consumer ) の主キー重複なら ErrAlreadyProcessed を返す。
func MarkProcessed(ctx context.Context, db *sql.DB, eventID, consumer string, now time.Time) error {
	res, err := db.ExecContext(ctx,
		`INSERT IGNORE INTO event_processed (event_id, consumer, processed_at) VALUES (?, ?, ?)`,
		eventID, consumer, now.UTC())
	if err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("mark processed rows affected: %w", err)
	}
	if n == 0 {
		return ErrAlreadyProcessed
	}
	return nil
}
