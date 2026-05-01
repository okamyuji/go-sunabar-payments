package application

import "go-sunabar-payments/internal/modules/transfer/domain"

// mapStateChangeEvent 遷移先 status に対応する通知イベント名を返す。 空文字列なら通知不要。
// 受信側 ( Notification モジュール ) は本マップに含まれる event_type のみ購読する。
func mapStateChangeEvent(to domain.Status) string {
	switch to {
	case domain.StatusRequested:
		return domain.EventTransferAcceptedToBank
	case domain.StatusAwaitingApproval:
		return domain.EventTransferAwaitingApproval
	case domain.StatusSettled:
		return domain.EventTransferSettled
	case domain.StatusFailed:
		return domain.EventTransferFailed
	case domain.StatusPending, domain.StatusApproved:
		return ""
	default:
		return ""
	}
}
