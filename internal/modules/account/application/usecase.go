package application

import (
	"context"
	"fmt"
	"time"

	"go-sunabar-payments/internal/modules/account/domain"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// SyncAccountsUseCase sunabar から最新の口座一覧を取得し、 アプリ DB のキャッシュを更新する。
type SyncAccountsUseCase struct {
	txMgr  transaction.Manager
	repo   AccountRepository
	client sunabar.Client
	idGen  IDGenerator
	now    func() time.Time
}

// NewSyncAccountsUseCase コンストラクタ。
func NewSyncAccountsUseCase(txMgr transaction.Manager, repo AccountRepository, client sunabar.Client, idGen IDGenerator, now func() time.Time) *SyncAccountsUseCase {
	if now == nil {
		now = time.Now
	}
	return &SyncAccountsUseCase{txMgr: txMgr, repo: repo, client: client, idGen: idGen, now: now}
}

// Execute sunabar から取得した口座を逐次 UPSERT する。
func (u *SyncAccountsUseCase) Execute(ctx context.Context) ([]*domain.Account, error) {
	accounts, err := u.client.GetAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("get accounts: %w", err)
	}
	out := make([]*domain.Account, 0, len(accounts))
	if err := u.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		now := u.now()
		for _, a := range accounts {
			id := u.idGen.NewAccountID()
			d := domain.NewAccount(id, a.AccountID, a.BranchCode, a.AccountNumber, a.AccountTypeCode, a.AccountName, a.PrimaryAccountFlag, now)
			if err := u.repo.UpsertByAccountID(ctx, tx, d); err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// GetBalanceUseCase sunabar から残高を取得する。 自前 DB にはキャッシュしない ( リアルタイム性優先 ) 。
type GetBalanceUseCase struct {
	client sunabar.Client
}

// NewGetBalanceUseCase コンストラクタ。
func NewGetBalanceUseCase(client sunabar.Client) *GetBalanceUseCase {
	return &GetBalanceUseCase{client: client}
}

// Execute accountID の残高を返す。
func (u *GetBalanceUseCase) Execute(ctx context.Context, accountID string) (*domain.Balance, error) {
	b, err := u.client.GetBalance(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return &domain.Balance{
		AccountID:    b.AccountID,
		CurrencyCode: b.CurrencyCode,
		Amount:       b.Balance,
		BaseDate:     b.BaseDate,
		BaseTime:     b.BaseTime,
	}, nil
}

// IssueVirtualAccountCommand バーチャル口座発行のコマンド。
// sunabar 実 API は法人 API 専用の /corporation/v1/va/issue を使うため、
// raId / vaTypeCode / vaHolderNameKana 等のフィールドが必要。
type IssueVirtualAccountCommand struct {
	Memo              string     // アプリ側のメモ ( DB に保存 )
	ExpiresOn         *time.Time // アプリ側の有効期限 ( sunabar API 側には現状送らない )
	RaID              string     // 親契約 ID ( sunabar VA 契約から取得した raId )
	VaTypeCode        string     // 任意 ( 既定 "2" )
	IssueRequestCount string     // 任意 ( 既定 "1" )
	VaContractAuthKey string     // 任意 ( VA 契約承認キー )
	VaHolderNameKana  string     // 半角カナの口座名義
	VaHolderNamePos   string     // 任意 ( 既定 "1" )
}

// IssueVirtualAccountUseCase バーチャル口座を sunabar で発行し、 アプリ DB に登録する。
type IssueVirtualAccountUseCase struct {
	txMgr  transaction.Manager
	repo   VirtualAccountRepository
	client sunabar.Client
	idGen  IDGenerator
	now    func() time.Time
}

// NewIssueVirtualAccountUseCase コンストラクタ。
func NewIssueVirtualAccountUseCase(txMgr transaction.Manager, repo VirtualAccountRepository, client sunabar.Client, idGen IDGenerator, now func() time.Time) *IssueVirtualAccountUseCase {
	if now == nil {
		now = time.Now
	}
	return &IssueVirtualAccountUseCase{txMgr: txMgr, repo: repo, client: client, idGen: idGen, now: now}
}

// Execute バーチャル口座を発行する。 sunabar 側の API 呼び出しが先で、 成功してからアプリ DB に登録する。
// sunabar の API 失敗時は DB 副作用なし、 アプリ DB INSERT 失敗時は sunabar 側に行が残るが
// 同じ idempotency_key で再実行すれば同じバーチャル口座を取得できる。
func (u *IssueVirtualAccountUseCase) Execute(ctx context.Context, cmd IssueVirtualAccountCommand) (*domain.VirtualAccount, error) {
	idemKey := u.idGen.NewIdempotencyKey()
	req := sunabar.VirtualAccountRequest{
		IdempotencyKey:    idemKey,
		Memo:              cmd.Memo,
		RaID:              cmd.RaID,
		VaTypeCode:        cmd.VaTypeCode,
		IssueRequestCount: cmd.IssueRequestCount,
		VaContractAuthKey: cmd.VaContractAuthKey,
		VaHolderNameKana:  cmd.VaHolderNameKana,
		VaHolderNamePos:   cmd.VaHolderNamePos,
	}
	if cmd.ExpiresOn != nil {
		req.ExpiresOn = *cmd.ExpiresOn
	}
	res, err := u.client.IssueVirtualAccount(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("issue virtual account: %w", err)
	}

	id := u.idGen.NewAccountID()
	memo := cmd.Memo
	var memoPtr *string
	if memo != "" {
		memoPtr = &memo
	}
	var expPtr *time.Time
	if !res.ExpiresOn.IsZero() {
		expPtr = &res.ExpiresOn
	} else if cmd.ExpiresOn != nil {
		expPtr = cmd.ExpiresOn
	}
	va := domain.NewVirtualAccount(id, res.VirtualAccountID, res.BranchCode, res.AccountNumber, memoPtr, expPtr, u.now())

	if err := u.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return u.repo.Insert(ctx, tx, va)
	}); err != nil {
		return nil, err
	}
	return va, nil
}
