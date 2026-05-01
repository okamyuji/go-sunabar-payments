package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	"go-sunabar-payments/internal/platform/transaction"
)

// SendToSunabarHandler TransferRequested を受信して sunabar.RequestTransfer を呼ぶハンドラ。
// 成功なら Transfer を REQUESTED に遷移させ、 TransferAcceptedToBank と TransferStatusCheckScheduled を発行する。
// 失敗なら Transfer を FAILED に遷移させ、 TransferFailed を発行する。
type SendToSunabarHandler struct {
	txMgr  transaction.Manager
	repo   Repository
	outbox outbox.Publisher
	client sunabar.Client
	idGen  IDGenerator
	now    func() time.Time
}

// NewSendToSunabarHandler ハンドラを生成する。
func NewSendToSunabarHandler(
	txMgr transaction.Manager,
	repo Repository,
	pub outbox.Publisher,
	client sunabar.Client,
	idGen IDGenerator,
	now func() time.Time,
) *SendToSunabarHandler {
	if now == nil {
		now = time.Now
	}
	return &SendToSunabarHandler{txMgr: txMgr, repo: repo, outbox: pub, client: client, idGen: idGen, now: now}
}

// Handle 1 イベントを処理する。
// エラーを返した場合、 Relay が next_attempt_at を更新して再投入する ( 指数バックオフ ) 。
func (h *SendToSunabarHandler) Handle(ctx context.Context, evt outbox.Event) error {
	var p domain.TransferRequestedPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	t, err := h.repo.FindByID(ctx, p.TransferID)
	if err != nil {
		return fmt.Errorf("find transfer: %w", err)
	}
	// 既に PENDING 以外なら冪等処理として何もしない。
	if t.Status != domain.StatusPending {
		return nil
	}

	res, apiErr := h.client.RequestTransfer(ctx, sunabar.TransferRequest{
		IdempotencyKey:  t.APIIdempotencyKey,
		SourceAccountID: t.SourceAccountID,
		Amount:          t.Amount,
		DestBankCode:    t.DestBankCode,
		DestBranchCode:  t.DestBranchCode,
		DestAccountType: t.DestAccountType,
		DestAccountNum:  t.DestAccountNum,
		DestAccountName: t.DestAccountName,
		TransferDate:    h.now().Format("2006-01-02"),
	})

	// 5xx / 接続エラーはリトライ可能なのでハンドラ自身がエラーを返し、 Relay に再投入させる。
	// 4xx はクライアント側の不具合なので即 MarkFailed に倒す ( ADR-008 ) 。
	if apiErr != nil && isRetryable(apiErr) {
		return apiErr
	}

	return h.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		now := h.now()
		if apiErr != nil {
			if err := t.MarkFailed(apiErr.Error(), now); err != nil {
				return fmt.Errorf("mark failed: %w", err)
			}
			if err := h.repo.Update(ctx, tx, t); err != nil {
				return fmt.Errorf("update transfer ( failed ) : %w", err)
			}
			return h.publishStateChanged(ctx, tx, t, domain.StatusPending, domain.StatusFailed, "", apiErr.Error())
		}

		if err := t.MarkRequested(res.ApplyNo, now); err != nil {
			return fmt.Errorf("mark requested: %w", err)
		}
		if err := h.repo.Update(ctx, tx, t); err != nil {
			return fmt.Errorf("update transfer ( requested ) : %w", err)
		}

		// 通知向けイベント。
		if err := h.publishStateChanged(ctx, tx, t, domain.StatusPending, domain.StatusRequested, res.ApplyNo, ""); err != nil {
			return err
		}

		// 結果照会のスケジュール。
		schedPayload, err := json.Marshal(domain.TransferStatusCheckPayload{
			TransferID: t.ID,
			ApplyNo:    res.ApplyNo,
		})
		if err != nil {
			return fmt.Errorf("marshal status check payload: %w", err)
		}
		schedEvt := outbox.Event{
			ID:            h.idGen.NewTransferID(),
			AggregateType: "transfer",
			AggregateID:   t.ID,
			EventType:     domain.EventTransferStatusCheck,
			Payload:       schedPayload,
			OccurredAt:    now,
		}
		return h.outbox.Publish(ctx, tx, schedEvt)
	})
}

// isRetryable sunabar 呼び出しエラーがリトライ可能かを判定する。
// - sunabar.APIError かつ StatusCode 5xx ならリトライ可
// - APIError でないラップ済みエラー ( 接続失敗 / タイムアウト ) もリトライ可
// - APIError かつ 4xx は不可 ( クライアント側不具合 )
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *sunabar.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return true
}

// publishStateChanged 遷移先 status に対応する通知用イベントを Outbox に書き込む。
func (h *SendToSunabarHandler) publishStateChanged(
	ctx context.Context, tx transaction.Tx, t *domain.Transfer,
	from, to domain.Status, applyNo, errMsg string,
) error {
	eventType := mapStateChangeEvent(to)
	if eventType == "" {
		return nil
	}
	payload, err := json.Marshal(domain.TransferStateChangedPayload{
		TransferID:   t.ID,
		FromStatus:   string(from),
		ToStatus:     string(to),
		ApplyNo:      applyNo,
		ErrorMessage: errMsg,
	})
	if err != nil {
		return fmt.Errorf("marshal state changed payload: %w", err)
	}
	return h.outbox.Publish(ctx, tx, outbox.Event{
		ID:            h.idGen.NewTransferID(),
		AggregateType: "transfer",
		AggregateID:   t.ID,
		EventType:     eventType,
		Payload:       payload,
		OccurredAt:    h.now(),
	})
}
