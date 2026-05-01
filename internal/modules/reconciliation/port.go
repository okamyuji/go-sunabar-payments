// Package reconciliation Reconciliation モジュールの公開 API を提供する。
package reconciliation

import (
	"context"
	"database/sql"
	"time"

	"go-sunabar-payments/internal/modules/reconciliation/application"
	"go-sunabar-payments/internal/modules/reconciliation/domain"
	"go-sunabar-payments/internal/modules/reconciliation/infrastructure"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// Service Reconciliation モジュールの公開 API 。
type Service interface {
	IssueInvoice(ctx context.Context, cmd application.IssueInvoiceCommand) (*domain.Invoice, error)
	Reconcile(ctx context.Context, virtualAccountID string) (*application.ReconcileResult, error)
	GetInvoice(ctx context.Context, id string) (*domain.Invoice, error)
}

// Module Reconciliation モジュールの全コンポーネント。
type Module struct {
	Service Service
}

// ReconcileResult Reconcile 1 回の結果。 application パッケージの型を再エクスポートする。
type ReconcileResult = application.ReconcileResult

// New モジュールを構築する。
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
	invRepo := infrastructure.NewInvoiceSQLRepository(db)
	incRepo := infrastructure.NewIncomingTransactionSQLRepository(db)
	issueUC := application.NewIssueInvoiceUseCase(txMgr, invRepo, idGen, now)
	reconcileUC := application.NewReconcileVirtualAccountUseCase(txMgr, invRepo, incRepo, pub, client, idGen, now)
	return &Module{Service: &service{issue: issueUC, reconcile: reconcileUC, invRepo: invRepo}}
}

type service struct {
	issue     *application.IssueInvoiceUseCase
	reconcile *application.ReconcileVirtualAccountUseCase
	invRepo   application.InvoiceRepository
}

func (s *service) IssueInvoice(ctx context.Context, cmd application.IssueInvoiceCommand) (*domain.Invoice, error) {
	return s.issue.Execute(ctx, cmd)
}
func (s *service) Reconcile(ctx context.Context, virtualAccountID string) (*application.ReconcileResult, error) {
	return s.reconcile.Execute(ctx, virtualAccountID)
}
func (s *service) GetInvoice(ctx context.Context, id string) (*domain.Invoice, error) {
	return s.invRepo.FindByID(ctx, id)
}
