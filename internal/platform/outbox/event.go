// Package outbox Transactional Outbox パターンの実装を提供する。
// アプリケーションは Publisher を通じてビジネスデータの更新と同一トランザクションで
// イベントを書き込む。 Relay が非同期にイベントを取り出し、登録された Handler に配信する。
package outbox

import "time"

// Event Outbox に書き込まれる 1 件のイベントを表す。
// Payload JSON 形式のバイト列で持ち、 AggregateType と AggregateID で発生元を識別する。
type Event struct {
	// ID UUID v7 を推奨する ( 順序性を保つため ) 。
	ID string
	// AggregateType 発生元集約の種別。 例 "transfer", "incoming_transaction" 。
	AggregateType string
	// AggregateID 集約ルートの ID 。
	AggregateID string
	// EventType イベント種別。 例 "TransferRequested", "TransferSettled" 。
	EventType string
	// Payload JSON バイト列。
	Payload []byte
	// OccurredAt イベント発生時刻 ( DB 側の created_at に対応 ) 。
	OccurredAt time.Time
}
