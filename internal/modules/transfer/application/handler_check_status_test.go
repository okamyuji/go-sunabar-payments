package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/transfer/application"
	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
)

func seedRequestedTransfer(t *testing.T, repo *inMemoryRepo, id, applyNo string) *domain.Transfer {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := domain.NewTransfer(id, "app-"+id, "00000000-0000-4000-8000-000000000010", domain.TransferParams{
		Amount:          1000,
		SourceAccountID: "ACC0001",
		DestBankCode:    "0033",
		DestBranchCode:  "001",
		DestAccountType: "1",
		DestAccountNum:  "1234567",
		DestAccountName: "ヤマダ タロウ",
	}, now)
	if err := repo.Insert(context.Background(), fakeTx{}, tr); err != nil {
		t.Fatalf("seed insert: %v", err)
	}
	if err := tr.MarkRequested(applyNo, now); err != nil {
		t.Fatalf("MarkRequested: %v", err)
	}
	if err := repo.Update(context.Background(), fakeTx{}, tr); err != nil {
		t.Fatalf("seed update: %v", err)
	}
	return tr
}

func makeStatusCheckEvent(t *testing.T, transferID, applyNo string) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(domain.TransferStatusCheckPayload{
		TransferID: transferID,
		ApplyNo:    applyNo,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return outbox.Event{
		ID:            "00000000-0000-7000-8000-0000000000aa",
		AggregateType: "transfer",
		AggregateID:   transferID,
		EventType:     domain.EventTransferStatusCheck,
		Payload:       payload,
		OccurredAt:    time.Now().UTC(),
	}
}

func TestCheckStatus_SettledMarksTerminal(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	seedRequestedTransfer(t, repo, "tr-settled", "AP-1")
	stub := &stubSunabarClient{
		statusFn: func(_ context.Context, _ string) (*sunabar.TransferStatusResult, error) {
			return &sunabar.TransferStatusResult{Status: "Settled"}, nil
		},
	}

	h := application.NewCheckStatusHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	if err := h.Handle(context.Background(), makeStatusCheckEvent(t, "tr-settled", "AP-1")); err != nil {
		t.Errorf("Handle: %v ( 終端 SETTLED は nil 期待 ) ", err)
	}
	got, _ := repo.FindByID(context.Background(), "tr-settled")
	if got.Status != domain.StatusSettled {
		t.Errorf("status = %s, want SETTLED", got.Status)
	}
	events := pub.Events()
	if len(events) != 1 || events[0].EventType != domain.EventTransferSettled {
		t.Errorf("events = %+v, want TransferSettled 1 件", events)
	}
}

func TestCheckStatus_AwaitingApprovalReturnsStillInFlight(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	seedRequestedTransfer(t, repo, "tr-await", "AP-2")
	stub := &stubSunabarClient{
		statusFn: func(_ context.Context, _ string) (*sunabar.TransferStatusResult, error) {
			return &sunabar.TransferStatusResult{Status: "AwaitingApproval"}, nil
		},
	}

	h := application.NewCheckStatusHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	err := h.Handle(context.Background(), makeStatusCheckEvent(t, "tr-await", "AP-2"))
	if !errors.Is(err, application.ErrStillInFlight) {
		t.Errorf("err = %v, want ErrStillInFlight", err)
	}
	got, _ := repo.FindByID(context.Background(), "tr-await")
	if got.Status != domain.StatusAwaitingApproval {
		t.Errorf("status = %s, want AWAITING_APPROVAL", got.Status)
	}
	events := pub.Events()
	if len(events) != 1 || events[0].EventType != domain.EventTransferAwaitingApproval {
		t.Errorf("events = %+v, want TransferAwaitingApproval 1 件", events)
	}
}

func TestCheckStatus_FailedMarksTerminalWithReason(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	seedRequestedTransfer(t, repo, "tr-fail", "AP-3")
	stub := &stubSunabarClient{
		statusFn: func(_ context.Context, _ string) (*sunabar.TransferStatusResult, error) {
			return &sunabar.TransferStatusResult{Status: "Failed", StatusDetail: "rejected"}, nil
		},
	}

	h := application.NewCheckStatusHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	if err := h.Handle(context.Background(), makeStatusCheckEvent(t, "tr-fail", "AP-3")); err != nil {
		t.Errorf("Handle: %v ( FAILED は nil 期待 ) ", err)
	}
	got, _ := repo.FindByID(context.Background(), "tr-fail")
	if got.Status != domain.StatusFailed {
		t.Errorf("status = %s, want FAILED", got.Status)
	}
	if got.LastError == nil || *got.LastError != "rejected" {
		t.Errorf("LastError = %v, want rejected", got.LastError)
	}
}

func TestCheckStatus_AlreadyTerminalIsNoOp(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr := seedRequestedTransfer(t, repo, "tr-done", "AP-4")
	if err := tr.Transition(domain.StatusSettled, now); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if err := repo.Update(context.Background(), fakeTx{}, tr); err != nil {
		t.Fatalf("update: %v", err)
	}

	stub := &stubSunabarClient{
		statusFn: func(_ context.Context, _ string) (*sunabar.TransferStatusResult, error) {
			return &sunabar.TransferStatusResult{Status: "Settled"}, nil
		},
	}
	h := application.NewCheckStatusHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	if err := h.Handle(context.Background(), makeStatusCheckEvent(t, "tr-done", "AP-4")); err != nil {
		t.Errorf("Handle: %v", err)
	}
	if events := pub.Events(); len(events) != 0 {
		t.Errorf("events = %d, want 0 ( 終端は no-op ) ", len(events))
	}
}

func TestCheckStatus_SameUnconfirmedRepeatNoNewEvent(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr := seedRequestedTransfer(t, repo, "tr-repeat", "AP-5")
	// 既に AWAITING_APPROVAL に進めておく。
	if err := tr.Transition(domain.StatusAwaitingApproval, now); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if err := repo.Update(context.Background(), fakeTx{}, tr); err != nil {
		t.Fatalf("update: %v", err)
	}

	stub := &stubSunabarClient{
		statusFn: func(_ context.Context, _ string) (*sunabar.TransferStatusResult, error) {
			return &sunabar.TransferStatusResult{Status: "AwaitingApproval"}, nil
		},
	}
	h := application.NewCheckStatusHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	err := h.Handle(context.Background(), makeStatusCheckEvent(t, "tr-repeat", "AP-5"))
	if !errors.Is(err, application.ErrStillInFlight) {
		t.Errorf("err = %v, want ErrStillInFlight", err)
	}
	if events := pub.Events(); len(events) != 0 {
		t.Errorf("events = %d, want 0 ( 同じ未確定の再観測ではイベント発行しない ) ", len(events))
	}
}
