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

// ErrStillInFlight 未確定状態を示すエラー。 Relay がこれを受け取った場合、 next_attempt_at を未来に更新して再投入する。
var ErrStillInFlight = errors.New("transfer still in flight, will retry")

// CheckStatusHandler TransferStatusCheckScheduled を受信して sunabar.GetTransferStatus を呼ぶハンドラ。
// 確定 ( SETTLED / FAILED ) まで状態を進める。 未確定 ( AwaitingApproval / Approved ) なら状態更新後に
// ErrStillInFlight を返して Relay に再ポーリングさせる。
type CheckStatusHandler struct {
	txMgr  transaction.Manager
	repo   Repository
	outbox outbox.Publisher
	client sunabar.Client
	idGen  IDGenerator
	now    func() time.Time
}

// NewCheckStatusHandler ハンドラを生成する。
func NewCheckStatusHandler(
	txMgr transaction.Manager,
	repo Repository,
	pub outbox.Publisher,
	client sunabar.Client,
	idGen IDGenerator,
	now func() time.Time,
) *CheckStatusHandler {
	if now == nil {
		now = time.Now
	}
	return &CheckStatusHandler{txMgr: txMgr, repo: repo, outbox: pub, client: client, idGen: idGen, now: now}
}

// Handle 結果照会を 1 回実行し、 状態を進める。
// - 終端 ( SETTLED / FAILED ) なら nil を返す
// - 未確定なら状態を更新したうえで ErrStillInFlight を返して再投入させる
// - sunabar API 呼び出し自体が失敗したら通常のエラー ( バックオフ対象 ) を返す
func (h *CheckStatusHandler) Handle(ctx context.Context, evt outbox.Event) error {
	var p domain.TransferStatusCheckPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	res, apiErr := h.client.GetTransferStatus(ctx, p.ApplyNo)
	if apiErr != nil {
		return fmt.Errorf("get transfer status: %w", apiErr)
	}

	to, ok := mapSunabarStatus(res.Status)
	if !ok {
		return fmt.Errorf("unknown sunabar status: %q", res.Status)
	}

	t, err := h.repo.FindByID(ctx, p.TransferID)
	if err != nil {
		return fmt.Errorf("find transfer: %w", err)
	}

	// 既に終端なら nil 。 同じ未確定状態の再観測なら ErrStillInFlight 。
	if t.Status.IsTerminal() {
		return nil
	}
	if t.Status == to {
		if to.IsTerminal() {
			return nil
		}
		return ErrStillInFlight
	}

	if err := h.applyStatus(ctx, t, to, res.StatusDetail, p.ApplyNo); err != nil {
		return err
	}
	if !to.IsTerminal() {
		return ErrStillInFlight
	}
	return nil
}

// applyStatus 状態を to に進めて Update し、 通知イベントを発行する処理を 1 トランザクションで行う。
func (h *CheckStatusHandler) applyStatus(ctx context.Context, t *domain.Transfer, to domain.Status, detail, applyNo string) error {
	return h.txMgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		now := h.now()
		from := t.Status
		if to == domain.StatusFailed {
			if mErr := t.MarkFailed(detail, now); mErr != nil {
				return fmt.Errorf("mark failed: %w", mErr)
			}
		} else {
			if tErr := t.Transition(to, now); tErr != nil {
				return fmt.Errorf("transition: %w", tErr)
			}
		}
		if uErr := h.repo.Update(ctx, tx, t); uErr != nil {
			return fmt.Errorf("update transfer: %w", uErr)
		}

		eventType := mapStateChangeEvent(to)
		if eventType == "" {
			return nil
		}
		payload, mErr := json.Marshal(domain.TransferStateChangedPayload{
			TransferID:   t.ID,
			FromStatus:   string(from),
			ToStatus:     string(to),
			ApplyNo:      applyNo,
			ErrorMessage: derefString(t.LastError),
		})
		if mErr != nil {
			return fmt.Errorf("marshal state changed payload: %w", mErr)
		}
		return h.outbox.Publish(ctx, tx, outbox.Event{
			ID:            h.idGen.NewTransferID(),
			AggregateType: "transfer",
			AggregateID:   t.ID,
			EventType:     eventType,
			Payload:       payload,
			OccurredAt:    now,
		})
	})
}

// derefString *string をゼロ値安全に値で返す。
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// mapSunabarStatus sunabar の status 文字列を domain.Status にマップする。
// 実際の文字列は sunabar API 仕様書に従う ( M9 の実機検証フェーズで精緻化 ) 。
func mapSunabarStatus(s string) (domain.Status, bool) {
	switch s {
	case "AcceptedToBank":
		return domain.StatusRequested, true
	case "AwaitingApproval":
		return domain.StatusAwaitingApproval, true
	case "Approved":
		return domain.StatusApproved, true
	case "Settled":
		return domain.StatusSettled, true
	case "Failed":
		return domain.StatusFailed, true
	}
	return "", false
}
