// Package sunabar GMO あおぞらネット銀行のオープン API ( sunabar サンドボックスおよび本番 BaaS ) に対する
// Go クライアントを提供する。 Client インターフェースを通じて呼び出し、 AuthSource で認可方式を差し替える。
package sunabar

import "time"

// Account 口座情報を表す。
type Account struct {
	AccountID          string
	BranchCode         string
	AccountNumber      string
	AccountTypeCode    string
	AccountName        string
	PrimaryAccountFlag bool
}

// Balance 残高情報を表す。 Balance は円単位の整数で保持する。
type Balance struct {
	AccountID    string
	CurrencyCode string
	Balance      int64
	BaseDate     string
	BaseTime     string
}

// Transaction 1 件の入出金明細を表す。
type Transaction struct {
	TransactionDate string
	ValueDate       string
	TransactionType string
	Amount          int64
	Balance         int64
	Remarks         string
	ItemKey         string
}

// ListTransactionsParams 入出金明細照会のクエリパラメータ。
type ListTransactionsParams struct {
	From time.Time
	To   time.Time
}

// TransactionList 入出金明細の一覧。
type TransactionList struct {
	AccountID    string
	CurrencyCode string
	BaseDate     string
	BaseTime     string
	HasNext      bool
	Count        int
	Transactions []Transaction
}

// TransferRequest 振込依頼のリクエストパラメータ。
type TransferRequest struct {
	IdempotencyKey  string
	SourceAccountID string
	Amount          int64
	DestBankCode    string
	DestBranchCode  string
	DestAccountType string
	DestAccountNum  string
	DestAccountName string
	TransferDate    string
	Remarks         string
}

// TransferResult 振込依頼レスポンス。 ApplyNo は結果照会で使用する。
type TransferResult struct {
	ApplyNo  string
	Status   string
	AcceptAt time.Time
}

// TransferStatusResult 結果照会のレスポンス。
type TransferStatusResult struct {
	ApplyNo      string
	Status       string
	StatusDetail string
	UpdatedAt    time.Time
}

// VirtualAccountRequest バーチャル口座発行リクエスト。
type VirtualAccountRequest struct {
	IdempotencyKey string
	Memo           string
	ExpiresOn      time.Time
}

// VirtualAccount バーチャル口座情報。
type VirtualAccount struct {
	VirtualAccountID string
	BranchCode       string
	AccountNumber    string
	ExpiresOn        time.Time
}
