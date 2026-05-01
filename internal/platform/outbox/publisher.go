package outbox

import (
	"context"
	"fmt"
	"time"

	"go-sunabar-payments/internal/platform/transaction"
)

// Publisher Outbox にイベントを書き込む。
// ビジネスデータの更新と同一トランザクション内で Publish を呼ぶことで原子性を担保する。
type Publisher interface {
	Publish(ctx context.Context, tx transaction.Tx, event Event) error
}

// SQLPublisher database/sql に対する Publisher 実装。
type SQLPublisher struct {
	now func() time.Time
}

// NewSQLPublisher デフォルトの now 関数 ( time.Now ) で SQLPublisher を返す。
func NewSQLPublisher() *SQLPublisher {
	return &SQLPublisher{now: time.Now}
}

// NewSQLPublisherWithClock テスト容易性のため now 関数を差し込めるコンストラクタ。
// 渡された関数の戻り値はそのままレコードに書き込まれる ( UTC 変換は呼び出し側で揃える ) 。
func NewSQLPublisherWithClock(now func() time.Time) *SQLPublisher {
	if now == nil {
		now = time.Now
	}
	return &SQLPublisher{now: now}
}

// Publish 与えられたトランザクション上で outbox_events に 1 行 INSERT する。
// status='PENDING', attempt_count=0, next_attempt_at=now, created_at=now で固定する。
func (p *SQLPublisher) Publish(ctx context.Context, tx transaction.Tx, event Event) error {
	const q = `INSERT INTO outbox_events
		(id, aggregate_type, aggregate_id, event_type, payload, status, attempt_count, next_attempt_at, created_at)
		VALUES (?, ?, ?, ?, ?, 'PENDING', 0, ?, ?)`

	now := p.now().UTC()
	if _, err := tx.SQL().ExecContext(ctx, q,
		event.ID, event.AggregateType, event.AggregateID, event.EventType,
		event.Payload, now, now,
	); err != nil {
		return fmt.Errorf("outbox publish: %w", err)
	}
	return nil
}
