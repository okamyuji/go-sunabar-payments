package observability

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
)

// Metrics アプリ全体で参照する観測指標のホルダ。
// 単純なインメモリカウンタで、 後で Prometheus / OpenTelemetry に差し替え可能な形にしている。
type Metrics struct {
	outboxPendingDepth atomic.Int64
	outboxFailedDepth  atomic.Int64
	transferStatus     sync.Map // key: status string -> *atomic.Int64
}

// NewMetrics ゼロ値の Metrics を返す。
func NewMetrics() *Metrics { return &Metrics{} }

// SetOutboxPendingDepth Outbox の PENDING 件数をセットする。
func (m *Metrics) SetOutboxPendingDepth(n int64) { m.outboxPendingDepth.Store(n) }

// SetOutboxFailedDepth Outbox の FAILED 件数をセットする。
func (m *Metrics) SetOutboxFailedDepth(n int64) { m.outboxFailedDepth.Store(n) }

// SetTransferStatusCount status ごとの Transfer 件数を上書きする。
func (m *Metrics) SetTransferStatusCount(status string, n int64) {
	v, _ := m.transferStatus.LoadOrStore(status, &atomic.Int64{})
	v.(*atomic.Int64).Store(n)
}

// Snapshot 現在のメトリクス値をマップで返す ( /metrics エンドポイントの JSON 出力に使う ) 。
func (m *Metrics) Snapshot() map[string]int64 {
	out := map[string]int64{
		"outbox_pending_depth": m.outboxPendingDepth.Load(),
		"outbox_failed_depth":  m.outboxFailedDepth.Load(),
	}
	m.transferStatus.Range(func(k, v any) bool {
		out["transfer_status_"+k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

// CollectFromDB DB の状態を読み取って Metrics を更新する。 Relay やヘルスチェックから定期的に呼ぶ。
func (m *Metrics) CollectFromDB(ctx context.Context, db *sql.DB) error {
	var pending, failed int64
	row := db.QueryRowContext(ctx, `SELECT
		SUM(CASE WHEN status='PENDING' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='FAILED' THEN 1 ELSE 0 END)
		FROM outbox_events`)
	if err := row.Scan(&pendingNullable{n: &pending}, &pendingNullable{n: &failed}); err != nil {
		return err
	}
	m.SetOutboxPendingDepth(pending)
	m.SetOutboxFailedDepth(failed)

	rows, err := db.QueryContext(ctx, `SELECT status, COUNT(*) FROM transfers GROUP BY status`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return err
		}
		m.SetTransferStatusCount(status, n)
	}
	return rows.Err()
}

// pendingNullable SUM 結果の NULL ( テーブル空 ) を 0 に丸める Scanner ヘルパ。
type pendingNullable struct{ n *int64 }

// Scan SUM が NULL の場合 0 をセットする。
func (p *pendingNullable) Scan(v any) error {
	if v == nil {
		*p.n = 0
		return nil
	}
	switch x := v.(type) {
	case int64:
		*p.n = x
	case []byte:
		var n int64
		for _, b := range x {
			if b < '0' || b > '9' {
				continue
			}
			n = n*10 + int64(b-'0')
		}
		*p.n = n
	default:
		*p.n = 0
	}
	return nil
}
