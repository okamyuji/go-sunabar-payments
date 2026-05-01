package domain

// Outbox イベント名。 消込結果を他モジュール ( Notification など ) へ伝える。
const (
	// EventReconciliationCompleted 請求が CLEARED に到達したときに発行する。
	EventReconciliationCompleted = "ReconciliationCompleted"
	// EventReconciliationExcess 過入金 ( EXCESS ) を検知したときに発行する。 手動確認用。
	EventReconciliationExcess = "ReconciliationExcess"
	// EventReconciliationPartial 一部入金 ( PARTIAL ) を検知したときに発行する。 続報用。
	EventReconciliationPartial = "ReconciliationPartial"
)

// ReconciliationStatusPayload 消込結果の Outbox ペイロード。
type ReconciliationStatusPayload struct {
	InvoiceID        string `json:"invoiceId"`
	VirtualAccountID string `json:"virtualAccountId"`
	Amount           int64  `json:"amount"`
	PaidAmount       int64  `json:"paidAmount"`
	Status           string `json:"status"`
}
