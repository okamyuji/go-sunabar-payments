// Package transfer Transfer モジュールの公開 API を提供する。
// 他モジュール / cmd からは本 package 経由でのみ Transfer モジュールを使用する。
package transfer

import (
	"context"
	"database/sql"
	"time"

	"go-sunabar-payments/internal/modules/transfer/application"
	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/modules/transfer/infrastructure"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// Service Transfer モジュールの公開 API 。
type Service interface {
	RequestTransfer(ctx context.Context, cmd application.RequestTransferCommand) (*domain.Transfer, error)
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)
}

// Module Transfer モジュールの全コンポーネントをまとめる。
// 配線 ( cmd/api, cmd/relay ) から本構造体を参照する。
type Module struct {
	Service              Service
	SendToSunabarHandler outbox.Handler
	CheckStatusHandler   outbox.Handler
	Repository           application.Repository
}

// New モジュールを構築する。 依存はすべて引数で受け取り、 内部で配線する。
func New(
	db *sql.DB,
	txMgr transaction.Manager,
	pub outbox.Publisher,
	client sunabar.Client,
	idGen application.IDGenerator,
	now func() time.Time,
) *Module {
	if now == nil {
		now = time.Now
	}
	repo := infrastructure.NewSQLRepository(db)
	requestUC := application.NewRequestTransferUseCase(txMgr, repo, pub, idGen, now)
	send := application.NewSendToSunabarHandler(txMgr, repo, pub, client, idGen, now)
	check := application.NewCheckStatusHandler(txMgr, repo, pub, client, idGen, now)
	return &Module{
		Service:              &service{uc: requestUC, repo: repo},
		SendToSunabarHandler: outbox.HandlerFunc(send.Handle),
		CheckStatusHandler:   outbox.HandlerFunc(check.Handle),
		Repository:           repo,
	}
}

// service Service interface の実装。 アプリ層 UseCase に委譲する。
type service struct {
	uc   *application.RequestTransferUseCase
	repo application.Repository
}

// RequestTransfer 振込依頼ユースケースを実行する。
func (s *service) RequestTransfer(ctx context.Context, cmd application.RequestTransferCommand) (*domain.Transfer, error) {
	return s.uc.Execute(ctx, cmd)
}

// GetTransfer ID で Transfer を取得する。
func (s *service) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.repo.FindByID(ctx, id)
}
