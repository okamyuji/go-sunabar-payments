// Package infrastructure Account モジュールの永続化実装を提供する。
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"go-sunabar-payments/internal/modules/account/application"
	"go-sunabar-payments/internal/modules/account/domain"
	"go-sunabar-payments/internal/platform/transaction"
)

// AccountSQLRepository accounts テーブルへの永続化実装。
type AccountSQLRepository struct{ db *sql.DB }

// NewAccountSQLRepository コンストラクタ。
func NewAccountSQLRepository(db *sql.DB) *AccountSQLRepository {
	return &AccountSQLRepository{db: db}
}

var _ application.AccountRepository = (*AccountSQLRepository)(nil)

// UpsertByAccountID account_id をキーに UPSERT する。 既存があれば各列を更新する。
func (r *AccountSQLRepository) UpsertByAccountID(ctx context.Context, tx transaction.Tx, a *domain.Account) error {
	const q = `INSERT INTO accounts
		(id, account_id, branch_code, account_number, account_type_code, account_name, primary_flag, label, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
		  branch_code=VALUES(branch_code),
		  account_number=VALUES(account_number),
		  account_type_code=VALUES(account_type_code),
		  account_name=VALUES(account_name),
		  primary_flag=VALUES(primary_flag),
		  updated_at=VALUES(updated_at)`
	_, err := tx.SQL().ExecContext(ctx, q,
		a.ID, a.AccountID, a.BranchCode, a.AccountNumber, a.AccountTypeCode, a.AccountName,
		boolToInt(a.PrimaryFlag), a.Label, a.CreatedAt, a.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert account: %w", err)
	}
	return nil
}

// FindByAccountID account_id で 1 件取得する。
func (r *AccountSQLRepository) FindByAccountID(ctx context.Context, accountID string) (*domain.Account, error) {
	const q = `SELECT id, account_id, branch_code, account_number, account_type_code, account_name, primary_flag, label, created_at, updated_at
		FROM accounts WHERE account_id=?`
	row := r.db.QueryRowContext(ctx, q, accountID)
	return scanAccount(row)
}

// ListAll 全件取得する。
func (r *AccountSQLRepository) ListAll(ctx context.Context) ([]*domain.Account, error) {
	const q = `SELECT id, account_id, branch_code, account_number, account_type_code, account_name, primary_flag, label, created_at, updated_at
		FROM accounts ORDER BY created_at`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// scanRow QueryRow と Rows 両方をサポートする最小スキャナ。
type scanRow interface {
	Scan(dst ...any) error
}

func scanAccount(row scanRow) (*domain.Account, error) {
	var (
		a         domain.Account
		primary   int
		label     sql.NullString
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&a.ID, &a.AccountID, &a.BranchCode, &a.AccountNumber, &a.AccountTypeCode,
		&a.AccountName, &primary, &label, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan account: %w", err)
	}
	a.PrimaryFlag = primary != 0
	if label.Valid {
		s := label.String
		a.Label = &s
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	return &a, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// VirtualAccountSQLRepository virtual_accounts テーブルへの永続化実装。
type VirtualAccountSQLRepository struct{ db *sql.DB }

// NewVirtualAccountSQLRepository コンストラクタ。
func NewVirtualAccountSQLRepository(db *sql.DB) *VirtualAccountSQLRepository {
	return &VirtualAccountSQLRepository{db: db}
}

var _ application.VirtualAccountRepository = (*VirtualAccountSQLRepository)(nil)

// Insert バーチャル口座を 1 件 INSERT する。
func (r *VirtualAccountSQLRepository) Insert(ctx context.Context, tx transaction.Tx, va *domain.VirtualAccount) error {
	const q = `INSERT INTO virtual_accounts
		(id, virtual_account_id, branch_code, account_number, memo, expires_on, invoice_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := tx.SQL().ExecContext(ctx, q,
		va.ID, va.VirtualAccountID, va.BranchCode, va.AccountNumber, va.Memo,
		dateOrNil(va.ExpiresOn), va.InvoiceID, va.CreatedAt, va.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert virtual account: %w", err)
	}
	return nil
}

// FindByVirtualAccountID virtual_account_id で 1 件取得する。
func (r *VirtualAccountSQLRepository) FindByVirtualAccountID(ctx context.Context, vaID string) (*domain.VirtualAccount, error) {
	const q = `SELECT id, virtual_account_id, branch_code, account_number, memo, expires_on, invoice_id, created_at, updated_at
		FROM virtual_accounts WHERE virtual_account_id=?`
	row := r.db.QueryRowContext(ctx, q, vaID)
	return scanVA(row)
}

// ListAll 全件取得する。
func (r *VirtualAccountSQLRepository) ListAll(ctx context.Context) ([]*domain.VirtualAccount, error) {
	const q = `SELECT id, virtual_account_id, branch_code, account_number, memo, expires_on, invoice_id, created_at, updated_at
		FROM virtual_accounts ORDER BY created_at`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list virtual accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.VirtualAccount
	for rows.Next() {
		va, err := scanVA(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, va)
	}
	return out, rows.Err()
}

func scanVA(row scanRow) (*domain.VirtualAccount, error) {
	var (
		va        domain.VirtualAccount
		memo      sql.NullString
		expiresOn sql.NullTime
		invoiceID sql.NullString
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&va.ID, &va.VirtualAccountID, &va.BranchCode, &va.AccountNumber,
		&memo, &expiresOn, &invoiceID, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan virtual account: %w", err)
	}
	if memo.Valid {
		s := memo.String
		va.Memo = &s
	}
	if expiresOn.Valid {
		t := expiresOn.Time
		va.ExpiresOn = &t
	}
	if invoiceID.Valid {
		s := invoiceID.String
		va.InvoiceID = &s
	}
	va.CreatedAt = createdAt
	va.UpdatedAt = updatedAt
	return &va, nil
}

func dateOrNil(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Format("2006-01-02")
}
