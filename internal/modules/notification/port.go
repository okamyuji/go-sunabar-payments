// Package notification Notification モジュールの公開 API を提供する。
// 他モジュール / cmd からは本 package 経由でのみ利用する。
package notification

import (
	"database/sql"
	"log/slog"
	"time"

	"go-sunabar-payments/internal/modules/notification/application"
	"go-sunabar-payments/internal/modules/notification/infrastructure"
	transferdomain "go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
)

// Module Notification モジュールの全コンポーネント。
type Module struct {
	TransferEventHandler outbox.Handler
}

// New モジュールを構築する。 sender を別途差し替えたいテスト用に NewWithSender も用意する。
func New(db *sql.DB, logger *slog.Logger, now func() time.Time) *Module {
	return NewWithSender(db, infrastructure.NewStdoutSender(logger), now)
}

// NewWithSender 任意の Sender を注入してモジュールを構築する。
func NewWithSender(db *sql.DB, sender application.Sender, now func() time.Time) *Module {
	if now == nil {
		now = time.Now
	}
	h := application.NewTransferEventHandler(db, sender, now)
	return &Module{TransferEventHandler: outbox.HandlerFunc(h.Handle)}
}

// SubscribedEventTypes 本モジュールが購読する Outbox イベントタイプの一覧。
// cmd/relay 等の配線で `for _, et := range mod.SubscribedEventTypes() { relay.Register(et, h) }` のように使う。
func (m *Module) SubscribedEventTypes() []string {
	return []string{
		transferdomain.EventTransferAcceptedToBank,
		transferdomain.EventTransferAwaitingApproval,
		transferdomain.EventTransferSettled,
		transferdomain.EventTransferFailed,
	}
}
