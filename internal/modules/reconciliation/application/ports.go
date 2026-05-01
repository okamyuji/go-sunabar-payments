// Package application Reconciliation モジュールのユースケースとポートを定義する。
package application

import (
	"context"

	"go-sunabar-payments/internal/modules/reconciliation/domain"
	"go-sunabar-payments/internal/platform/transaction"
)

// InvoiceRepository invoices テーブルへの永続化抽象。
type InvoiceRepository interface {
	Insert(ctx context.Context, tx transaction.Tx, inv *domain.Invoice) error
	Update(ctx context.Context, tx transaction.Tx, inv *domain.Invoice) error
	FindByVirtualAccountID(ctx context.Context, vaID string) (*domain.Invoice, error)
	FindByID(ctx context.Context, id string) (*domain.Invoice, error)
}

// IncomingTransactionRepository incoming_transactions テーブルへの永続化抽象。
type IncomingTransactionRepository interface {
	// InsertIfNotExists 同一 ( virtual_account_id, item_key ) 既存ならスキップする。 戻り値は新規挿入したか否か。
	InsertIfNotExists(ctx context.Context, tx transaction.Tx, t *domain.IncomingTransaction) (bool, error)
	// UpdateMatchedInvoice 入金行に invoice_id を紐付ける。
	UpdateMatchedInvoice(ctx context.Context, tx transaction.Tx, txID, invoiceID string) error
	// ListByVirtualAccount バーチャル口座 ID で全件取得する。
	ListByVirtualAccount(ctx context.Context, vaID string) ([]*domain.IncomingTransaction, error)
}

// IDGenerator ID 生成抽象。
type IDGenerator interface {
	NewInvoiceID() string
	NewIncomingTransactionID() string
	NewTransferID() string // Outbox イベント用
}
