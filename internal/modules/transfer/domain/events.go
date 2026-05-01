package domain

// Outbox の event_type 列に書き込むイベントタイプ定数。
const (
	// EventTransferRequested アプリで受付直後 ( PENDING 作成時 ) 。 Relay で sunabar へ送信する起点。
	EventTransferRequested = "TransferRequested"
	// EventTransferAcceptedToBank sunabar が振込依頼を受け付けた後 ( REQUESTED 遷移 ) 。 Notification 用。
	EventTransferAcceptedToBank = "TransferAcceptedToBank"
	// EventTransferStatusCheck 結果照会のスケジュール。 Relay が再投入することで定期照会と等価に動く。
	EventTransferStatusCheck = "TransferStatusCheckScheduled"
	// EventTransferAwaitingApproval メールトークン承認待ち遷移。 Notification 用。
	EventTransferAwaitingApproval = "TransferAwaitingApproval"
	// EventTransferSettled 振込完了。 Notification 用。
	EventTransferSettled = "TransferSettled"
	// EventTransferFailed 振込失敗。 Notification 用。
	EventTransferFailed = "TransferFailed"
)

// TransferRequestedPayload TransferRequested イベントのペイロード。
// 受信側 ( SendToSunabarHandler ) が sunabar.RequestTransfer を呼ぶのに必要な情報を持つ。
type TransferRequestedPayload struct {
	TransferID        string `json:"transferId"`
	APIIdempotencyKey string `json:"apiIdempotencyKey"`
	Amount            int64  `json:"amount"`
	SourceAccountID   string `json:"sourceAccountId"`
	DestBankCode      string `json:"destBankCode"`
	DestBranchCode    string `json:"destBranchCode"`
	DestAccountType   string `json:"destAccountType"`
	DestAccountNum    string `json:"destAccountNum"`
	DestAccountName   string `json:"destAccountName"`
}

// TransferStatusCheckPayload 結果照会イベントのペイロード。
// 受信側 ( CheckStatusHandler ) が sunabar.GetTransferStatus を呼ぶのに使う。
type TransferStatusCheckPayload struct {
	TransferID string `json:"transferId"`
	ApplyNo    string `json:"applyNo"`
}

// TransferStateChangedPayload 状態変化を通知するイベントのペイロード。
// Notification モジュールがこれを購読してメール送信などに使う。
type TransferStateChangedPayload struct {
	TransferID   string `json:"transferId"`
	FromStatus   string `json:"fromStatus"`
	ToStatus     string `json:"toStatus"`
	ApplyNo      string `json:"applyNo,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}
