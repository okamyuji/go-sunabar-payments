package application_test

import (
	"context"
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/transfer/application"
	"go-sunabar-payments/internal/modules/transfer/domain"
)

func newRequestCmd(appReq string) application.RequestTransferCommand {
	return application.RequestTransferCommand{
		AppRequestID:    appReq,
		Amount:          1000,
		SourceAccountID: "ACC0001",
		DestBankCode:    "0033",
		DestBranchCode:  "001",
		DestAccountType: "1",
		DestAccountNum:  "1234567",
		DestAccountName: "ヤマダ タロウ",
	}
}

func TestRequestTransfer_NewCreatesTransferAndOutboxEvent(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	uc := application.NewRequestTransferUseCase(fakeTxManager{}, repo, pub, idGen, func() time.Time { return now })

	got, err := uc.Execute(context.Background(), newRequestCmd("app-1"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Status != domain.StatusPending {
		t.Errorf("status = %s, want PENDING", got.Status)
	}
	if got.APIIdempotencyKey == "" {
		t.Errorf("APIIdempotencyKey 空")
	}
	if events := pub.Events(); len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	} else if events[0].EventType != domain.EventTransferRequested {
		t.Errorf("event type = %s, want %s", events[0].EventType, domain.EventTransferRequested)
	}
}

func TestRequestTransfer_DuplicateAppRequestIDReturnsExisting(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	uc := application.NewRequestTransferUseCase(fakeTxManager{}, repo, pub, idGen, func() time.Time { return now })

	first, err := uc.Execute(context.Background(), newRequestCmd("app-dup"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := uc.Execute(context.Background(), newRequestCmd("app-dup"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("first.ID = %s, second.ID = %s ( 同じであるべき ) ", first.ID, second.ID)
	}
	if events := pub.Events(); len(events) != 1 {
		t.Errorf("Outbox events = %d, want 1 ( 重複時は発行しない ) ", len(events))
	}
}

func TestRequestTransfer_RepoInsertErrorRollsBack(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	repo.insertErr = domain.ErrConcurrentUpdate // 任意のエラーを偽装
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	uc := application.NewRequestTransferUseCase(fakeTxManager{}, repo, pub, idGen, func() time.Time { return now })

	if _, err := uc.Execute(context.Background(), newRequestCmd("app-err")); err == nil {
		t.Fatalf("err nil, want non-nil")
	}
	if events := pub.Events(); len(events) != 0 {
		t.Errorf("Outbox events = %d, want 0", len(events))
	}
}
