package application_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/transfer/application"
	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
)

func seedPendingTransfer(t *testing.T, repo *inMemoryRepo, transferID, appReq string) *domain.Transfer {
	t.Helper()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := domain.NewTransfer(transferID, appReq, "00000000-0000-4000-8000-000000000001", domain.TransferParams{
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
	return tr
}

func makeRequestedEvent(t *testing.T, transferID, idemKey string) outbox.Event {
	t.Helper()
	payload, err := json.Marshal(domain.TransferRequestedPayload{
		TransferID:        transferID,
		APIIdempotencyKey: idemKey,
		Amount:            1000,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return outbox.Event{
		ID:            "00000000-0000-7000-8000-000000000099",
		AggregateType: "transfer",
		AggregateID:   transferID,
		EventType:     domain.EventTransferRequested,
		Payload:       payload,
		OccurredAt:    time.Now().UTC(),
	}
}

func TestSendToSunabar_SuccessPath(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr := seedPendingTransfer(t, repo, "transfer-1", "app-1")
	stub := &stubSunabarClient{
		requestFn: func(_ context.Context, req sunabar.TransferRequest) (*sunabar.TransferResult, error) {
			if req.IdempotencyKey != tr.APIIdempotencyKey {
				t.Errorf("IdempotencyKey = %q, want %q", req.IdempotencyKey, tr.APIIdempotencyKey)
			}
			return &sunabar.TransferResult{ApplyNo: "AP-OK-1", Status: "AcceptedToBank", AcceptAt: now}, nil
		},
	}

	h := application.NewSendToSunabarHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	if err := h.Handle(context.Background(), makeRequestedEvent(t, "transfer-1", tr.APIIdempotencyKey)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got, _ := repo.FindByID(context.Background(), "transfer-1")
	if got.Status != domain.StatusRequested {
		t.Errorf("status = %s, want REQUESTED", got.Status)
	}
	if got.ApplyNo == nil || *got.ApplyNo != "AP-OK-1" {
		t.Errorf("ApplyNo = %v, want AP-OK-1", got.ApplyNo)
	}

	events := pub.Events()
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 ( accepted + status check ) ", len(events))
	}
	gotTypes := []string{events[0].EventType, events[1].EventType}
	wantTypes := []string{domain.EventTransferAcceptedToBank, domain.EventTransferStatusCheck}
	for i := range wantTypes {
		if gotTypes[i] != wantTypes[i] {
			t.Errorf("events[%d] = %s, want %s", i, gotTypes[i], wantTypes[i])
		}
	}
}

func TestSendToSunabar_FailureMarksFailed(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr := seedPendingTransfer(t, repo, "transfer-2", "app-2")
	// 4xx は ADR-008 により即 MarkFailed 。 5xx ならリトライさせる ( 別テスト ) 。
	stub := &stubSunabarClient{
		requestFn: func(_ context.Context, _ sunabar.TransferRequest) (*sunabar.TransferResult, error) {
			return nil, &sunabar.APIError{StatusCode: 400, RawBody: `{"error":"over limit"}`}
		},
	}

	h := application.NewSendToSunabarHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	if err := h.Handle(context.Background(), makeRequestedEvent(t, "transfer-2", tr.APIIdempotencyKey)); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got, _ := repo.FindByID(context.Background(), "transfer-2")
	if got.Status != domain.StatusFailed {
		t.Errorf("status = %s, want FAILED", got.Status)
	}

	events := pub.Events()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 ( failed のみ ) ", len(events))
	}
	if events[0].EventType != domain.EventTransferFailed {
		t.Errorf("event = %s, want TransferFailed", events[0].EventType)
	}
}

func TestSendToSunabar_ServerErrorRetries(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr := seedPendingTransfer(t, repo, "transfer-5xx", "app-5xx")
	stub := &stubSunabarClient{
		requestFn: func(_ context.Context, _ sunabar.TransferRequest) (*sunabar.TransferResult, error) {
			return nil, &sunabar.APIError{StatusCode: 503, RawBody: `{"error":"upstream busy"}`}
		},
	}

	h := application.NewSendToSunabarHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	err := h.Handle(context.Background(), makeRequestedEvent(t, "transfer-5xx", tr.APIIdempotencyKey))
	if err == nil {
		t.Fatalf("err = nil, want non-nil ( 5xx はリトライさせる ) ")
	}
	got, _ := repo.FindByID(context.Background(), "transfer-5xx")
	if got.Status != domain.StatusPending {
		t.Errorf("status = %s, want PENDING ( 5xx は MarkFailed しない ) ", got.Status)
	}
	if len(pub.Events()) != 0 {
		t.Errorf("events = %d, want 0 ( 5xx はイベント発行なし ) ", len(pub.Events()))
	}
}

func TestSendToSunabar_NotPendingIsNoOp(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tr := seedPendingTransfer(t, repo, "transfer-3", "app-3")
	// Status を REQUESTED に変えておく ( 既に処理済み相当 ) 。
	tr.Status = domain.StatusRequested
	if err := repo.Update(context.Background(), fakeTx{}, tr); err != nil {
		t.Fatalf("seed update: %v", err)
	}

	stub := &stubSunabarClient{}
	h := application.NewSendToSunabarHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })
	if err := h.Handle(context.Background(), makeRequestedEvent(t, "transfer-3", tr.APIIdempotencyKey)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if stub.requestN.Load() != 0 {
		t.Errorf("RequestTransfer 呼び出し = %d, want 0", stub.requestN.Load())
	}
	if len(pub.Events()) != 0 {
		t.Errorf("events = %d, want 0", len(pub.Events()))
	}
}

func TestSendToSunabar_PayloadCorrupt(t *testing.T) {
	t.Parallel()
	repo := newInMemoryRepo()
	pub := newInMemoryPublisher()
	idGen := &fakeIDGen{}
	now := time.Now().UTC()

	stub := &stubSunabarClient{}
	h := application.NewSendToSunabarHandler(fakeTxManager{}, repo, pub, stub, idGen, func() time.Time { return now })

	bad := outbox.Event{ID: "x", EventType: domain.EventTransferRequested, Payload: []byte("not json")}
	if err := h.Handle(context.Background(), bad); err == nil {
		t.Errorf("err nil, want non-nil for bad payload")
	}
}
