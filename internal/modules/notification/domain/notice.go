// Package domain Notification モジュールのドメインを提供する。
// Notice はユーザに送る案内文の不変データ。
package domain

// Kind 通知の種類。
type Kind string

const (
	// KindTransferAccepted 振込が銀行に受け付けられた旨の案内。
	KindTransferAccepted Kind = "TRANSFER_ACCEPTED"
	// KindTransferAwaiting メールトークン承認待ちの案内 ( 承認 URL を含む ) 。
	KindTransferAwaiting Kind = "TRANSFER_AWAITING_APPROVAL"
	// KindTransferSettled 振込完了の案内。
	KindTransferSettled Kind = "TRANSFER_SETTLED"
	// KindTransferFailed 振込失敗の案内。
	KindTransferFailed Kind = "TRANSFER_FAILED"
)

// Notice 1 件の通知。 Sender の実装 ( stdout / メール / Slack 等 ) はこの形を消費する。
type Notice struct {
	Kind        Kind
	TransferID  string
	Subject     string
	Body        string
	ApprovalURL string // AwaitingApproval のときのみ非空
}
