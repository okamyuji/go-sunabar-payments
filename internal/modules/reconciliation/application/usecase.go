package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-sunabar-payments/internal/modules/reconciliation/domain"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// IssueInvoiceCommand 新規請求の作成リクエスト。
type IssueInvoiceCommand struct {
	Amount           int64
	VirtualAccountID string
	Memo             string
}

// IssueInvoiceUseCase 新規請求を作る。 同一バーチャル口座への重複は ErrInvoiceAlreadyExists で防ぐ。
type IssueInvoiceUseCase struct {
	txMgr transaction.Manager
	repo  InvoiceRepository
	idGen IDGenerator
	now   func() time.Time
}

// NewIssueInvoiceUseCase コンストラクタ。
func NewIssueInvoiceUseCase(txMgr transaction.Manager, repo InvoiceRepository, idGen IDGenerator, now func() time.Time) *IssueInvoiceUseCase {
	if now == nil {
		now = time.Now
	}
	return &IssueInvoiceUseCase{txMgr: txMgr, repo: repo, idGen: idGen, now: now}
}

// Execute 新規 Invoice を永続化する。
func (u *IssueInvoiceUseCase) Execute(ctx context.Context, cmd IssueInvoiceCommand) (*domain.Invoice, error) {
	id := u.idGen.NewInvoiceID()
	var memoPtr *string
	if cmd.Memo != "" {
		m := cmd.Memo
		memoPtr = &m
	}
	inv := domain.NewInvoice(id, cmd.Amount, cmd.VirtualAccountID, memoPtr, u.now())
	if err := u.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return u.repo.Insert(ctx, tx, inv)
	}); err != nil {
		return nil, err
	}
	return inv, nil
}

// ReconcileResult 1 回の Reconcile 実行で何件の入金を新規取り込んだかを返す。
type ReconcileResult struct {
	NewIncomings   int
	MatchedInvoice *domain.Invoice
}

// ReconcileVirtualAccountUseCase 1 つのバーチャル口座について sunabar から入出金明細を引き、 incoming_transactions に
// 重複なく登録し、 紐付く invoice の状態を再計算して必要なら Outbox イベントを発行する。
type ReconcileVirtualAccountUseCase struct {
	txMgr      transaction.Manager
	invRepo    InvoiceRepository
	incRepo    IncomingTransactionRepository
	publisher  outbox.Publisher
	client     sunabar.Client
	idGen      IDGenerator
	now        func() time.Time
	lookbackHr int
}

// NewReconcileVirtualAccountUseCase コンストラクタ。
func NewReconcileVirtualAccountUseCase(
	txMgr transaction.Manager,
	invRepo InvoiceRepository,
	incRepo IncomingTransactionRepository,
	pub outbox.Publisher,
	client sunabar.Client,
	idGen IDGenerator,
	now func() time.Time,
) *ReconcileVirtualAccountUseCase {
	if now == nil {
		now = time.Now
	}
	return &ReconcileVirtualAccountUseCase{
		txMgr: txMgr, invRepo: invRepo, incRepo: incRepo,
		publisher: pub, client: client, idGen: idGen, now: now,
		lookbackHr: 24, // 直近 24 時間ぶんを取得する
	}
}

// Execute 1 つのバーチャル口座について消込を実行する。
func (u *ReconcileVirtualAccountUseCase) Execute(ctx context.Context, vaID string) (*ReconcileResult, error) {
	now := u.now()
	from := now.Add(-time.Duration(u.lookbackHr) * time.Hour)
	list, err := u.client.ListTransactions(ctx, vaID, sunabar.ListTransactionsParams{From: from, To: now})
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}

	res := &ReconcileResult{}
	if err := u.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		inv, invErr := u.invRepo.FindByVirtualAccountID(ctx, vaID)
		if invErr != nil && !errors.Is(invErr, domain.ErrInvoiceNotFound) {
			return invErr
		}
		if err := u.applyIncomings(ctx, tx, vaID, list.Transactions, inv, now, res); err != nil {
			return err
		}
		if inv == nil || res.NewIncomings == 0 {
			return nil
		}
		if uErr := u.invRepo.Update(ctx, tx, inv); uErr != nil {
			return uErr
		}
		res.MatchedInvoice = inv
		return u.publishStateEvent(ctx, tx, inv)
	}); err != nil {
		return nil, err
	}
	return res, nil
}

// applyIncomings 入金明細を 1 件ずつ取り込み、 必要なら invoice を Apply する内側のループ処理。
func (u *ReconcileVirtualAccountUseCase) applyIncomings(
	ctx context.Context,
	tx transaction.Tx,
	vaID string,
	items []sunabar.Transaction,
	inv *domain.Invoice,
	now time.Time,
	res *ReconcileResult,
) error {
	for _, item := range items {
		if item.TransactionType != "credit" || item.Amount <= 0 {
			continue
		}
		id := u.idGen.NewIncomingTransactionID()
		occurred, _ := time.Parse("2006-01-02", item.TransactionDate)
		it := domain.NewIncomingTransaction(id, vaID, item.ItemKey, item.Amount, optString(item.Remarks), occurred, now)
		inserted, ierr := u.incRepo.InsertIfNotExists(ctx, tx, it)
		if ierr != nil {
			return ierr
		}
		if !inserted {
			continue
		}
		res.NewIncomings++
		if inv == nil {
			continue
		}
		inv.Apply(item.Amount, now)
		if mErr := u.incRepo.UpdateMatchedInvoice(ctx, tx, it.ID, inv.ID); mErr != nil {
			return mErr
		}
	}
	return nil
}

// publishStateEvent invoice の status に応じて Outbox イベントを発行する。
// OPEN は通知不要 ( 既定状態 ) なのでスキップする。
func (u *ReconcileVirtualAccountUseCase) publishStateEvent(ctx context.Context, tx transaction.Tx, inv *domain.Invoice) error {
	eventType := mapStatusToEvent(inv.Status)
	if eventType == "" {
		return nil
	}
	payload, err := json.Marshal(domain.ReconciliationStatusPayload{
		InvoiceID:        inv.ID,
		VirtualAccountID: inv.VirtualAccountID,
		Amount:           inv.Amount,
		PaidAmount:       inv.PaidAmount,
		Status:           string(inv.Status),
	})
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return u.publisher.Publish(ctx, tx, outbox.Event{
		ID:            u.idGen.NewTransferID(),
		AggregateType: "invoice",
		AggregateID:   inv.ID,
		EventType:     eventType,
		Payload:       payload,
		OccurredAt:    u.now(),
	})
}

func mapStatusToEvent(s domain.InvoiceStatus) string {
	switch s {
	case domain.InvoiceCleared:
		return domain.EventReconciliationCompleted
	case domain.InvoiceExcess:
		return domain.EventReconciliationExcess
	case domain.InvoicePartial:
		return domain.EventReconciliationPartial
	case domain.InvoiceOpen:
		return ""
	default:
		return ""
	}
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
