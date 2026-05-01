// Package domain Reconciliation 集約のドメインを提供する。 Invoice と IncomingTransaction 、 マッチング判定など。
package domain

import (
	"errors"
	"time"
)

// InvoiceStatus 請求の入金状況。
type InvoiceStatus string

const (
	// InvoiceOpen 入金未着 ( 未消込 ) 。
	InvoiceOpen InvoiceStatus = "OPEN"
	// InvoicePartial 一部入金あり、 残額未消込。
	InvoicePartial InvoiceStatus = "PARTIAL"
	// InvoiceCleared 入金完了 ( 請求額と入金合計が一致 ) 。
	InvoiceCleared InvoiceStatus = "CLEARED"
	// InvoiceExcess 入金が請求額を超過 ( 過入金 ) 。 運用判断のため Outbox イベントを発行する。
	InvoiceExcess InvoiceStatus = "EXCESS"
)

// Invoice 請求集約。 ステータスは入金合計と請求額の関係で機械的に決まる。
type Invoice struct {
	ID               string
	Amount           int64
	VirtualAccountID string
	Status           InvoiceStatus
	PaidAmount       int64
	Memo             *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewInvoice 新規請求を作る。 status は OPEN 、 paid_amount は 0 で初期化する。
func NewInvoice(id string, amount int64, vaID string, memo *string, now time.Time) *Invoice {
	return &Invoice{
		ID:               id,
		Amount:           amount,
		VirtualAccountID: vaID,
		Status:           InvoiceOpen,
		PaidAmount:       0,
		Memo:             memo,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// Apply 入金を反映し、 ステータスを再計算する。 入金額がマイナスでも処理は通すが副作用は限定的。
func (i *Invoice) Apply(paidDelta int64, now time.Time) {
	i.PaidAmount += paidDelta
	switch {
	case i.PaidAmount == 0:
		i.Status = InvoiceOpen
	case i.PaidAmount < i.Amount:
		i.Status = InvoicePartial
	case i.PaidAmount == i.Amount:
		i.Status = InvoiceCleared
	default:
		i.Status = InvoiceExcess
	}
	i.UpdatedAt = now
}

// IncomingTransaction 入金明細。 sunabar の入出金明細 API から得たデータ。
type IncomingTransaction struct {
	ID               string
	VirtualAccountID string
	ItemKey          string
	Amount           int64
	Remarks          *string
	OccurredAt       time.Time
	MatchedInvoiceID *string
	CreatedAt        time.Time
}

// NewIncomingTransaction 入金明細値オブジェクトを生成する。
func NewIncomingTransaction(id, vaID, itemKey string, amount int64, remarks *string, occurredAt, now time.Time) *IncomingTransaction {
	return &IncomingTransaction{
		ID:               id,
		VirtualAccountID: vaID,
		ItemKey:          itemKey,
		Amount:           amount,
		Remarks:          remarks,
		OccurredAt:       occurredAt,
		CreatedAt:        now,
	}
}

// ドメインエラー。
var (
	// ErrInvoiceNotFound 指定 invoice_id の請求が見つからない。
	ErrInvoiceNotFound = errors.New("invoice not found")
	// ErrInvoiceAlreadyExists 同一バーチャル口座に紐づく請求が既にある。
	ErrInvoiceAlreadyExists = errors.New("invoice already exists for the virtual account")
)
