// Package application Notification モジュールのユースケースとポートを定義する。
package application

import (
	"context"

	"go-sunabar-payments/internal/modules/notification/domain"
)

// Sender 通知の送信抽象。 stdout ベース、 メール、 Slack、 LINE などに差し替え可能。
type Sender interface {
	Send(ctx context.Context, notice domain.Notice) error
}
