// Package infrastructure Notification モジュールの送信実装を提供する。
package infrastructure

import (
	"context"
	"log/slog"

	"go-sunabar-payments/internal/modules/notification/domain"
)

// StdoutSender 通知を log/slog で出力する送信実装。
// 個人開発・ローカル動作確認向け。 本番ではメールや Slack 送信に差し替える。
type StdoutSender struct {
	logger *slog.Logger
}

// NewStdoutSender StdoutSender を生成する。 logger が nil なら slog.Default を使う。
func NewStdoutSender(logger *slog.Logger) *StdoutSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &StdoutSender{logger: logger}
}

// Send 通知を構造化ログとして出力する。
func (s *StdoutSender) Send(_ context.Context, n domain.Notice) error {
	s.logger.Info("notification",
		"kind", string(n.Kind),
		"transfer_id", n.TransferID,
		"subject", n.Subject,
		"body", n.Body,
		"approval_url", n.ApprovalURL,
	)
	return nil
}
