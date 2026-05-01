package sunabar

import "context"

// Client sunabar / 本番 GMO あおぞら BaaS の主要 API を抽象化したインターフェース。
// 実装は HTTPClient ( 本物の API 接続 ) と httptest ベースのモックを用意する。
// テストではモックを注入し、 実機検証時は HTTPClient を使う。
type Client interface {
	// GetAccounts 口座情報を取得する。
	GetAccounts(ctx context.Context) ([]Account, error)
	// GetBalance 残高を取得する。
	GetBalance(ctx context.Context, accountID string) (*Balance, error)
	// ListTransactions 入出金明細を取得する。
	ListTransactions(ctx context.Context, accountID string, params ListTransactionsParams) (*TransactionList, error)
	// RequestTransfer 振込を依頼する。 IdempotencyKey ヘッダで重複抑止する。
	RequestTransfer(ctx context.Context, req TransferRequest) (*TransferResult, error)
	// GetTransferStatus 振込結果を照会する。 AcceptedToBank / Settled / Failed などの状態が返る。
	GetTransferStatus(ctx context.Context, applyNo string) (*TransferStatusResult, error)
	// IssueVirtualAccount バーチャル口座 ( 振込入金口座 ) を発行する。
	IssueVirtualAccount(ctx context.Context, req VirtualAccountRequest) (*VirtualAccount, error)
}
