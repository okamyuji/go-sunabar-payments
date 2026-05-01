package application_test

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// fakeTx 統合 DB を使わずに transaction.Tx シグネチャを満たすためのスタブ。
// SQL は呼ばれない前提 ( inMemoryRepo を使う ) 。 万一呼ばれたら nil 返却。
type fakeTx struct{}

func (fakeTx) SQL() *sql.Tx { return nil }

// fakeTxManager fn を即時実行するだけの transaction.Manager 。
// 失敗パスのテストでは fakeTxManagerFailing を使う。
type fakeTxManager struct{}

func (fakeTxManager) Do(ctx context.Context, fn func(ctx context.Context, tx transaction.Tx) error) error {
	return fn(ctx, fakeTx{})
}

// inMemoryRepo Repository のメモリ実装。
type inMemoryRepo struct {
	mu        sync.Mutex
	byID      map[string]*domain.Transfer
	byAppReq  map[string]*domain.Transfer
	insertErr error
	updateErr error
}

func newInMemoryRepo() *inMemoryRepo {
	return &inMemoryRepo{
		byID:     make(map[string]*domain.Transfer),
		byAppReq: make(map[string]*domain.Transfer),
	}
}

func (r *inMemoryRepo) Insert(_ context.Context, _ transaction.Tx, t *domain.Transfer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.insertErr != nil {
		return r.insertErr
	}
	if _, ok := r.byAppReq[t.AppRequestID]; ok {
		return domain.ErrAlreadyExists
	}
	cp := *t
	r.byID[t.ID] = &cp
	r.byAppReq[t.AppRequestID] = &cp
	return nil
}

func (r *inMemoryRepo) Update(_ context.Context, _ transaction.Tx, t *domain.Transfer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.updateErr != nil {
		return r.updateErr
	}
	cur, ok := r.byID[t.ID]
	if !ok {
		return domain.ErrNotFound
	}
	if cur.Version != t.Version {
		return domain.ErrConcurrentUpdate
	}
	cp := *t
	cp.Version = t.Version + 1
	r.byID[t.ID] = &cp
	r.byAppReq[t.AppRequestID] = &cp
	t.Version++
	return nil
}

func (r *inMemoryRepo) FindByID(_ context.Context, id string) (*domain.Transfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.byID[id]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}

func (r *inMemoryRepo) FindByAppRequestID(_ context.Context, appReq string) (*domain.Transfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v, ok := r.byAppReq[appReq]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}

// inMemoryPublisher Outbox.Publisher のメモリ実装。
type inMemoryPublisher struct {
	mu     sync.Mutex
	events []outbox.Event
}

func newInMemoryPublisher() *inMemoryPublisher { return &inMemoryPublisher{} }

func (p *inMemoryPublisher) Publish(_ context.Context, _ transaction.Tx, e outbox.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
	return nil
}

func (p *inMemoryPublisher) Events() []outbox.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]outbox.Event, len(p.events))
	copy(out, p.events)
	return out
}

// fakeIDGen 連番ベースの ID Generator 。 36 文字 CHAR 列に合わせて UUID 風に整形する。
type fakeIDGen struct {
	transferSeq atomic.Int64
	idemSeq     atomic.Int64
}

func (g *fakeIDGen) NewTransferID() string {
	n := g.transferSeq.Add(1)
	return "00000000-0000-7000-8000-" + pad12(n)
}

func (g *fakeIDGen) NewIdempotencyKey() string {
	n := g.idemSeq.Add(1)
	return "00000000-0000-4000-8000-" + pad12(n)
}

func pad12(n int64) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < 12 {
		s = "0" + s
	}
	return s
}

// stubSunabarClient sunabar.Client のスタブ。 関数フィールドで挙動を差し替える。
type stubSunabarClient struct {
	requestFn  func(ctx context.Context, req sunabar.TransferRequest) (*sunabar.TransferResult, error)
	statusFn   func(ctx context.Context, applyNo string) (*sunabar.TransferStatusResult, error)
	requestN   atomic.Int32
	statusCall atomic.Int32
}

func (s *stubSunabarClient) GetAccounts(_ context.Context) ([]sunabar.Account, error) {
	return nil, nil
}

func (s *stubSunabarClient) GetBalance(_ context.Context, _ string) (*sunabar.Balance, error) {
	return nil, nil
}

func (s *stubSunabarClient) ListTransactions(_ context.Context, _ string, _ sunabar.ListTransactionsParams) (*sunabar.TransactionList, error) {
	return nil, nil
}

func (s *stubSunabarClient) RequestTransfer(ctx context.Context, req sunabar.TransferRequest) (*sunabar.TransferResult, error) {
	s.requestN.Add(1)
	if s.requestFn != nil {
		return s.requestFn(ctx, req)
	}
	return &sunabar.TransferResult{ApplyNo: "AP-stub", Status: "AcceptedToBank", AcceptAt: time.Now().UTC()}, nil
}

func (s *stubSunabarClient) GetTransferStatus(ctx context.Context, applyNo string) (*sunabar.TransferStatusResult, error) {
	s.statusCall.Add(1)
	if s.statusFn != nil {
		return s.statusFn(ctx, applyNo)
	}
	return &sunabar.TransferStatusResult{ApplyNo: applyNo, Status: "Settled", UpdatedAt: time.Now().UTC()}, nil
}

func (s *stubSunabarClient) IssueVirtualAccount(_ context.Context, _ sunabar.VirtualAccountRequest) (*sunabar.VirtualAccount, error) {
	return nil, nil
}
