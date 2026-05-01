package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/transaction"
)

// RequestTransferCommand HTTP API 層から渡される入力。
type RequestTransferCommand struct {
	AppRequestID    string
	Amount          int64
	SourceAccountID string
	DestBankCode    string
	DestBranchCode  string
	DestAccountType string
	DestAccountNum  string
	DestAccountName string
}

// RequestTransferUseCase 新規振込依頼を受け付けるユースケース。
// app_request_id で重複検査し、 新規なら Transfer と TransferRequested イベントを同一トランザクションで永続化する。
type RequestTransferUseCase struct {
	txMgr  transaction.Manager
	repo   Repository
	outbox outbox.Publisher
	idGen  IDGenerator
	now    func() time.Time
}

// NewRequestTransferUseCase RequestTransferUseCase を生成する。
func NewRequestTransferUseCase(
	txMgr transaction.Manager,
	repo Repository,
	pub outbox.Publisher,
	idGen IDGenerator,
	now func() time.Time,
) *RequestTransferUseCase {
	if now == nil {
		now = time.Now
	}
	return &RequestTransferUseCase{txMgr: txMgr, repo: repo, outbox: pub, idGen: idGen, now: now}
}

// errAlreadyExistsRetry トランザクション内で「既存あり」を検知してロールバックさせるための内部センチネル。
var errAlreadyExistsRetry = errors.New("transfer already exists, retry outside tx")

// Execute 振込依頼を実行する。 同一 app_request_id の再呼び出しは既存 Transfer を返す ( 冪等 ) 。
func (u *RequestTransferUseCase) Execute(ctx context.Context, cmd RequestTransferCommand) (*domain.Transfer, error) {
	// 高速パス 既存チェック ( トランザクション外で読み取る ) 。
	if existing, err := u.repo.FindByAppRequestID(ctx, cmd.AppRequestID); err == nil {
		return existing, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, fmt.Errorf("find by app request id: %w", err)
	}

	id := u.idGen.NewTransferID()
	idemKey := u.idGen.NewIdempotencyKey()
	t := domain.NewTransfer(id, cmd.AppRequestID, idemKey, domain.TransferParams{
		Amount:          cmd.Amount,
		SourceAccountID: cmd.SourceAccountID,
		DestBankCode:    cmd.DestBankCode,
		DestBranchCode:  cmd.DestBranchCode,
		DestAccountType: cmd.DestAccountType,
		DestAccountNum:  cmd.DestAccountNum,
		DestAccountName: cmd.DestAccountName,
	}, u.now())

	err := u.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		if err := u.repo.Insert(ctx, tx, t); err != nil {
			if errors.Is(err, domain.ErrAlreadyExists) {
				// 競合検知 トランザクションを巻き戻して既存を読み直す。
				return errAlreadyExistsRetry
			}
			return fmt.Errorf("insert transfer: %w", err)
		}

		payload, err := json.Marshal(domain.TransferRequestedPayload{
			TransferID:        t.ID,
			APIIdempotencyKey: t.APIIdempotencyKey,
			Amount:            t.Amount,
			SourceAccountID:   t.SourceAccountID,
			DestBankCode:      t.DestBankCode,
			DestBranchCode:    t.DestBranchCode,
			DestAccountType:   t.DestAccountType,
			DestAccountNum:    t.DestAccountNum,
			DestAccountName:   t.DestAccountName,
		})
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		evt := outbox.Event{
			ID:            u.idGen.NewTransferID(),
			AggregateType: "transfer",
			AggregateID:   t.ID,
			EventType:     domain.EventTransferRequested,
			Payload:       payload,
			OccurredAt:    u.now(),
		}
		return u.outbox.Publish(ctx, tx, evt)
	})

	if errors.Is(err, errAlreadyExistsRetry) {
		// トランザクション外で再検索する ( 別プロセスが先に書いた状態を取得 ) 。
		return u.repo.FindByAppRequestID(ctx, cmd.AppRequestID)
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}
