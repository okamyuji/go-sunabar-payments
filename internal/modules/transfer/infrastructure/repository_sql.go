// Package infrastructure Transfer モジュールの永続化実装を提供する。
package infrastructure

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-sunabar-payments/internal/modules/transfer/application"
	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/transaction"
)

// SQLRepository database/sql で実装した Transfer Repository 。
type SQLRepository struct {
	db *sql.DB
}

// NewSQLRepository SQLRepository を生成する。
func NewSQLRepository(db *sql.DB) *SQLRepository {
	return &SQLRepository{db: db}
}

// 静的に application.Repository インターフェース充足を確認する。
var _ application.Repository = (*SQLRepository)(nil)

// Insert 新規 Transfer を挿入する。 app_request_id の重複時は ErrAlreadyExists を返す。
func (r *SQLRepository) Insert(ctx context.Context, tx transaction.Tx, t *domain.Transfer) error {
	const q = `INSERT INTO transfers
		(id, app_request_id, api_idempotency_key, status, amount, source_account_id,
		 dest_bank_code, dest_branch_code, dest_account_type, dest_account_num, dest_account_name,
		 apply_no, last_error, version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := tx.SQL().ExecContext(ctx, q,
		t.ID, t.AppRequestID, t.APIIdempotencyKey, string(t.Status), t.Amount, t.SourceAccountID,
		t.DestBankCode, t.DestBranchCode, t.DestAccountType, t.DestAccountNum, t.DestAccountName,
		t.ApplyNo, t.LastError, t.Version, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		if isDuplicate(err) {
			return domain.ErrAlreadyExists
		}
		return fmt.Errorf("insert transfer: %w", err)
	}
	return nil
}

// Update 楽観ロックで Transfer を更新する。 version 不一致なら ErrConcurrentUpdate を返す。
func (r *SQLRepository) Update(ctx context.Context, tx transaction.Tx, t *domain.Transfer) error {
	const q = `UPDATE transfers SET
		status=?, apply_no=?, last_error=?, version=version+1, updated_at=?
		WHERE id=? AND version=?`

	res, err := tx.SQL().ExecContext(ctx, q,
		string(t.Status), t.ApplyNo, t.LastError, t.UpdatedAt,
		t.ID, t.Version,
	)
	if err != nil {
		return fmt.Errorf("update transfer: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return domain.ErrConcurrentUpdate
	}
	t.Version++
	return nil
}

// FindByID ID で検索する。
func (r *SQLRepository) FindByID(ctx context.Context, id string) (*domain.Transfer, error) {
	const q = `SELECT id, app_request_id, api_idempotency_key, status, amount, source_account_id,
		dest_bank_code, dest_branch_code, dest_account_type, dest_account_num, dest_account_name,
		apply_no, last_error, version, created_at, updated_at
		FROM transfers WHERE id=?`
	return r.scanOne(ctx, q, id)
}

// FindByAppRequestID app_request_id で検索する。
func (r *SQLRepository) FindByAppRequestID(ctx context.Context, appRequestID string) (*domain.Transfer, error) {
	const q = `SELECT id, app_request_id, api_idempotency_key, status, amount, source_account_id,
		dest_bank_code, dest_branch_code, dest_account_type, dest_account_num, dest_account_name,
		apply_no, last_error, version, created_at, updated_at
		FROM transfers WHERE app_request_id=?`
	return r.scanOne(ctx, q, appRequestID)
}

// scanOne 1 行 SELECT をスキャンして domain.Transfer に詰める。
func (r *SQLRepository) scanOne(ctx context.Context, q string, args ...any) (*domain.Transfer, error) {
	row := r.db.QueryRowContext(ctx, q, args...)
	var (
		t                domain.Transfer
		status           string
		applyNo, lastErr sql.NullString
		createdAt        time.Time
		updatedAt        time.Time
	)
	if err := row.Scan(
		&t.ID, &t.AppRequestID, &t.APIIdempotencyKey, &status, &t.Amount, &t.SourceAccountID,
		&t.DestBankCode, &t.DestBranchCode, &t.DestAccountType, &t.DestAccountNum, &t.DestAccountName,
		&applyNo, &lastErr, &t.Version, &createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan transfer: %w", err)
	}
	t.Status = domain.Status(status)
	if applyNo.Valid {
		s := applyNo.String
		t.ApplyNo = &s
	}
	if lastErr.Valid {
		s := lastErr.String
		t.LastError = &s
	}
	t.CreatedAt = createdAt
	t.UpdatedAt = updatedAt
	return &t, nil
}

// isDuplicate MySQL の重複キーエラーを判定する。 ドライバ依存を避けるため文字列マッチで判定する。
func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "Duplicate entry")
}
