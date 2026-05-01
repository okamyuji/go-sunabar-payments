// Package application Account モジュールのユースケースとポートを定義する。
package application

import (
	"context"

	"go-sunabar-payments/internal/modules/account/domain"
	"go-sunabar-payments/internal/platform/transaction"
)

// AccountRepository Account メタ情報の永続化抽象。
type AccountRepository interface {
	// UpsertByAccountID account_id で UPSERT する。 既存が無ければ INSERT 、 あれば UPDATE 。
	UpsertByAccountID(ctx context.Context, tx transaction.Tx, a *domain.Account) error
	// FindByAccountID account_id で検索する。
	FindByAccountID(ctx context.Context, accountID string) (*domain.Account, error)
	// ListAll 全口座を取得する。
	ListAll(ctx context.Context) ([]*domain.Account, error)
}

// VirtualAccountRepository バーチャル口座メタ情報の永続化抽象。
type VirtualAccountRepository interface {
	Insert(ctx context.Context, tx transaction.Tx, va *domain.VirtualAccount) error
	FindByVirtualAccountID(ctx context.Context, vaID string) (*domain.VirtualAccount, error)
	ListAll(ctx context.Context) ([]*domain.VirtualAccount, error)
}

// IDGenerator ID 生成抽象。
type IDGenerator interface {
	NewAccountID() string
	NewIdempotencyKey() string
}
