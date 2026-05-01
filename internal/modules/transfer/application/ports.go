// Package application Transfer モジュールのユースケースと、 それが依存するポート ( interface ) を定義する。
package application

import (
	"context"

	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/transaction"
)

// Repository Transfer の永続化抽象。
type Repository interface {
	// Insert 新規 Transfer を永続化する。 app_request_id 重複時は ErrAlreadyExists を返す。
	Insert(ctx context.Context, tx transaction.Tx, t *domain.Transfer) error
	// Update 楽観ロックで更新する。 version 不一致時は ErrConcurrentUpdate を返す。
	Update(ctx context.Context, tx transaction.Tx, t *domain.Transfer) error
	// FindByID ID で検索する。 見つからない場合は ErrNotFound を返す。
	FindByID(ctx context.Context, id string) (*domain.Transfer, error)
	// FindByAppRequestID app_request_id で検索する。 見つからない場合は ErrNotFound を返す。
	FindByAppRequestID(ctx context.Context, appRequestID string) (*domain.Transfer, error)
}

// IDGenerator ID 生成抽象。 テストでは固定値を返すモックに差し替え可能。
type IDGenerator interface {
	// NewTransferID UUID v7 を返す ( Outbox の順序性確保のため v7 を推奨 ) 。
	NewTransferID() string
	// NewIdempotencyKey UUID v4 を返す ( sunabar / BaaS の冪等キー要件 ) 。
	NewIdempotencyKey() string
}
