// Package infrastructure Reconciliation モジュールの永続化実装を提供する。
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-sunabar-payments/internal/modules/reconciliation/application"
	"go-sunabar-payments/internal/modules/reconciliation/domain"
	"go-sunabar-payments/internal/platform/transaction"
)

// InvoiceSQLRepository invoices テーブルへの実装。
type InvoiceSQLRepository struct{ db *sql.DB }

// NewInvoiceSQLRepository コンストラクタ。
func NewInvoiceSQLRepository(db *sql.DB) *InvoiceSQLRepository { return &InvoiceSQLRepository{db: db} }

var _ application.InvoiceRepository = (*InvoiceSQLRepository)(nil)

// Insert 新規 Invoice を作る。 同一バーチャル口座は ErrInvoiceAlreadyExists を返す。
func (r *InvoiceSQLRepository) Insert(ctx context.Context, tx transaction.Tx, inv *domain.Invoice) error {
	const q = `INSERT INTO invoices (id, amount, virtual_account_id, status, paid_amount, memo, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.SQL().ExecContext(ctx, q, inv.ID, inv.Amount, inv.VirtualAccountID, string(inv.Status), inv.PaidAmount, inv.Memo, inv.CreatedAt, inv.UpdatedAt)
	if err != nil {
		if isDuplicate(err) {
			return domain.ErrInvoiceAlreadyExists
		}
		return fmt.Errorf("insert invoice: %w", err)
	}
	return nil
}

// Update Invoice の status / paid_amount / updated_at を更新する。
func (r *InvoiceSQLRepository) Update(ctx context.Context, tx transaction.Tx, inv *domain.Invoice) error {
	const q = `UPDATE invoices SET status=?, paid_amount=?, updated_at=? WHERE id=?`
	_, err := tx.SQL().ExecContext(ctx, q, string(inv.Status), inv.PaidAmount, inv.UpdatedAt, inv.ID)
	if err != nil {
		return fmt.Errorf("update invoice: %w", err)
	}
	return nil
}

// FindByVirtualAccountID 指定バーチャル口座の請求を取得する。
func (r *InvoiceSQLRepository) FindByVirtualAccountID(ctx context.Context, vaID string) (*domain.Invoice, error) {
	const q = `SELECT id, amount, virtual_account_id, status, paid_amount, memo, created_at, updated_at
		FROM invoices WHERE virtual_account_id=?`
	row := r.db.QueryRowContext(ctx, q, vaID)
	return scanInvoice(row)
}

// FindByID id で 1 件取得する。
func (r *InvoiceSQLRepository) FindByID(ctx context.Context, id string) (*domain.Invoice, error) {
	const q = `SELECT id, amount, virtual_account_id, status, paid_amount, memo, created_at, updated_at
		FROM invoices WHERE id=?`
	row := r.db.QueryRowContext(ctx, q, id)
	return scanInvoice(row)
}

type rowScanner interface{ Scan(...any) error }

func scanInvoice(row rowScanner) (*domain.Invoice, error) {
	var (
		inv       domain.Invoice
		status    string
		memo      sql.NullString
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&inv.ID, &inv.Amount, &inv.VirtualAccountID, &status, &inv.PaidAmount, &memo, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("scan invoice: %w", err)
	}
	inv.Status = domain.InvoiceStatus(status)
	if memo.Valid {
		s := memo.String
		inv.Memo = &s
	}
	inv.CreatedAt = createdAt
	inv.UpdatedAt = updatedAt
	return &inv, nil
}

// IncomingTransactionSQLRepository incoming_transactions テーブルへの実装。
type IncomingTransactionSQLRepository struct{ db *sql.DB }

// NewIncomingTransactionSQLRepository コンストラクタ。
func NewIncomingTransactionSQLRepository(db *sql.DB) *IncomingTransactionSQLRepository {
	return &IncomingTransactionSQLRepository{db: db}
}

var _ application.IncomingTransactionRepository = (*IncomingTransactionSQLRepository)(nil)

// InsertIfNotExists ( virtual_account_id, item_key ) の重複は INSERT IGNORE で吸収する。
// 戻り値は新規挿入したか否か。
func (r *IncomingTransactionSQLRepository) InsertIfNotExists(ctx context.Context, tx transaction.Tx, t *domain.IncomingTransaction) (bool, error) {
	const q = `INSERT IGNORE INTO incoming_transactions
		(id, virtual_account_id, item_key, amount, remarks, occurred_at, matched_invoice_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	res, err := tx.SQL().ExecContext(ctx, q, t.ID, t.VirtualAccountID, t.ItemKey, t.Amount, t.Remarks, t.OccurredAt, t.MatchedInvoiceID, t.CreatedAt)
	if err != nil {
		return false, fmt.Errorf("insert incoming: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

// UpdateMatchedInvoice 入金行に invoice_id を紐付ける。
func (r *IncomingTransactionSQLRepository) UpdateMatchedInvoice(ctx context.Context, tx transaction.Tx, txID, invoiceID string) error {
	const q = `UPDATE incoming_transactions SET matched_invoice_id=? WHERE id=?`
	_, err := tx.SQL().ExecContext(ctx, q, invoiceID, txID)
	if err != nil {
		return fmt.Errorf("update matched invoice: %w", err)
	}
	return nil
}

// ListByVirtualAccount バーチャル口座 ID で全件取得する。
func (r *IncomingTransactionSQLRepository) ListByVirtualAccount(ctx context.Context, vaID string) ([]*domain.IncomingTransaction, error) {
	const q = `SELECT id, virtual_account_id, item_key, amount, remarks, occurred_at, matched_invoice_id, created_at
		FROM incoming_transactions WHERE virtual_account_id=? ORDER BY occurred_at`
	rows, err := r.db.QueryContext(ctx, q, vaID)
	if err != nil {
		return nil, fmt.Errorf("list incoming: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.IncomingTransaction
	for rows.Next() {
		var (
			t         domain.IncomingTransaction
			remarks   sql.NullString
			matchedID sql.NullString
		)
		if err := rows.Scan(&t.ID, &t.VirtualAccountID, &t.ItemKey, &t.Amount, &remarks, &t.OccurredAt, &matchedID, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan incoming: %w", err)
		}
		if remarks.Valid {
			s := remarks.String
			t.Remarks = &s
		}
		if matchedID.Valid {
			s := matchedID.String
			t.MatchedInvoiceID = &s
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Duplicate entry")
}
