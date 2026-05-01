// Package account Account モジュールの公開 API を提供する。
package account

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"go-sunabar-payments/internal/modules/account/application"
	"go-sunabar-payments/internal/modules/account/domain"
	"go-sunabar-payments/internal/modules/account/infrastructure"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// Service Account モジュールの公開 API 。
type Service interface {
	SyncAccounts(ctx context.Context) ([]*domain.Account, error)
	GetBalance(ctx context.Context, accountID string) (*domain.Balance, error)
	IssueVirtualAccount(ctx context.Context, cmd application.IssueVirtualAccountCommand) (*domain.VirtualAccount, error)
	ListAccounts(ctx context.Context) ([]*domain.Account, error)
	ListVirtualAccounts(ctx context.Context) ([]*domain.VirtualAccount, error)
}

// Module Account モジュールの全コンポーネント。
type Module struct {
	Service     Service
	HTTPHandler *HTTPHandler
}

// New モジュールを構築する。
func New(db *sql.DB, txMgr transaction.Manager, client sunabar.Client, idGen application.IDGenerator, now func() time.Time) *Module {
	if now == nil {
		now = time.Now
	}
	accRepo := infrastructure.NewAccountSQLRepository(db)
	vaRepo := infrastructure.NewVirtualAccountSQLRepository(db)

	syncUC := application.NewSyncAccountsUseCase(txMgr, accRepo, client, idGen, now)
	balanceUC := application.NewGetBalanceUseCase(client)
	issueUC := application.NewIssueVirtualAccountUseCase(txMgr, vaRepo, client, idGen, now)

	svc := &service{syncUC: syncUC, balanceUC: balanceUC, issueUC: issueUC, accRepo: accRepo, vaRepo: vaRepo}
	return &Module{
		Service:     svc,
		HTTPHandler: NewHTTPHandler(svc),
	}
}

// service Service 実装。
type service struct {
	syncUC    *application.SyncAccountsUseCase
	balanceUC *application.GetBalanceUseCase
	issueUC   *application.IssueVirtualAccountUseCase
	accRepo   application.AccountRepository
	vaRepo    application.VirtualAccountRepository
}

func (s *service) SyncAccounts(ctx context.Context) ([]*domain.Account, error) {
	return s.syncUC.Execute(ctx)
}
func (s *service) GetBalance(ctx context.Context, accountID string) (*domain.Balance, error) {
	return s.balanceUC.Execute(ctx, accountID)
}
func (s *service) IssueVirtualAccount(ctx context.Context, cmd application.IssueVirtualAccountCommand) (*domain.VirtualAccount, error) {
	return s.issueUC.Execute(ctx, cmd)
}
func (s *service) ListAccounts(ctx context.Context) ([]*domain.Account, error) {
	return s.accRepo.ListAll(ctx)
}
func (s *service) ListVirtualAccounts(ctx context.Context) ([]*domain.VirtualAccount, error) {
	return s.vaRepo.ListAll(ctx)
}

// HTTPHandler は標準 net/http のハンドラ。
type HTTPHandler struct{ svc Service }

// NewHTTPHandler コンストラクタ。
func NewHTTPHandler(svc Service) *HTTPHandler { return &HTTPHandler{svc: svc} }

// Register ServeMux にエンドポイントを登録する。
//
//	POST /accounts/sync                -> sunabar から口座を同期
//	GET  /accounts                     -> アプリ DB の口座一覧
//	GET  /accounts/{id}/balance        -> 残高取得 ( accountId 指定 )
//	POST /virtual-accounts             -> バーチャル口座発行
//	GET  /virtual-accounts             -> 発行済みバーチャル口座一覧
func (h *HTTPHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /accounts/sync", http.HandlerFunc(h.sync))
	mux.Handle("GET /accounts", http.HandlerFunc(h.list))
	mux.Handle("GET /accounts/{id}/balance", http.HandlerFunc(h.balance))
	mux.Handle("POST /virtual-accounts", http.HandlerFunc(h.issueVA))
	mux.Handle("GET /virtual-accounts", http.HandlerFunc(h.listVA))
}
