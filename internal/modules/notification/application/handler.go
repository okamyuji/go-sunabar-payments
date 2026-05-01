package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go-sunabar-payments/internal/modules/notification/domain"
	transferdomain "go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
)

// ConsumerName event_processed テーブルでの consumer 名。 モジュール名固定。
const ConsumerName = "notification"

// TransferEventHandler Transfer 関連の Outbox イベントを受信して通知を送る。
// 受信側冪等性は event_processed テーブルで担保し、 同一 event_id を 2 回処理しても 1 回しか送信しない。
type TransferEventHandler struct {
	db     *sql.DB
	sender Sender
	now    func() time.Time
}

// NewTransferEventHandler ハンドラを生成する。
func NewTransferEventHandler(db *sql.DB, sender Sender, now func() time.Time) *TransferEventHandler {
	if now == nil {
		now = time.Now
	}
	return &TransferEventHandler{db: db, sender: sender, now: now}
}

// Handle 1 イベントを処理する。 冪等性チェック -> Notice 構築 -> Send の順に実行する。
// 知らない event_type は no-op で nil を返す ( Relay 側で SENT に遷移させる ) 。
func (h *TransferEventHandler) Handle(ctx context.Context, evt outbox.Event) error {
	if err := outbox.MarkProcessed(ctx, h.db, evt.ID, ConsumerName, h.now()); err != nil {
		if errors.Is(err, outbox.ErrAlreadyProcessed) {
			return nil
		}
		return fmt.Errorf("mark processed: %w", err)
	}

	notice, ok := buildNotice(evt)
	if !ok {
		return nil
	}
	return h.sender.Send(ctx, notice)
}

// buildNotice event_type と payload から Notice を組み立てる。
// AWAITING_APPROVAL の場合は承認 URL の案内を含める ( 値は固定文言 ) 。
func buildNotice(evt outbox.Event) (domain.Notice, bool) {
	var p transferdomain.TransferStateChangedPayload
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		return domain.Notice{}, false
	}
	switch evt.EventType {
	case transferdomain.EventTransferAcceptedToBank:
		return domain.Notice{
			Kind:       domain.KindTransferAccepted,
			TransferID: p.TransferID,
			Subject:    fmt.Sprintf("[ 振込受付 ] %s", p.TransferID),
			Body:       fmt.Sprintf("振込依頼を銀行に受付けました。 applyNo=%s", p.ApplyNo),
		}, true
	case transferdomain.EventTransferAwaitingApproval:
		return domain.Notice{
			Kind:        domain.KindTransferAwaiting,
			TransferID:  p.TransferID,
			Subject:     fmt.Sprintf("[ 要承認 ] %s", p.TransferID),
			Body:        fmt.Sprintf("sunabar サービスサイトのお知らせから「取引内容承認ページ」を開き、 取引パスワード入力で承認してください。 applyNo=%s", p.ApplyNo),
			ApprovalURL: "https://portal.sunabar.gmo-aozora.com/",
		}, true
	case transferdomain.EventTransferSettled:
		return domain.Notice{
			Kind:       domain.KindTransferSettled,
			TransferID: p.TransferID,
			Subject:    fmt.Sprintf("[ 振込完了 ] %s", p.TransferID),
			Body:       fmt.Sprintf("振込が完了しました。 applyNo=%s", p.ApplyNo),
		}, true
	case transferdomain.EventTransferFailed:
		return domain.Notice{
			Kind:       domain.KindTransferFailed,
			TransferID: p.TransferID,
			Subject:    fmt.Sprintf("[ 振込失敗 ] %s", p.TransferID),
			Body:       fmt.Sprintf("振込が失敗しました。 理由=%s", p.ErrorMessage),
		}, true
	}
	return domain.Notice{}, false
}
