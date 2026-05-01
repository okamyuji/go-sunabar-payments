package domain_test

import (
	"errors"
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/transfer/domain"
)

func newPendingTransfer(id string, now time.Time) *domain.Transfer {
	return domain.NewTransfer(id, "app-req-1", "api-idem-1", domain.TransferParams{
		Amount:          1000,
		SourceAccountID: "ACC0001",
		DestBankCode:    "0033",
		DestBranchCode:  "001",
		DestAccountType: "1",
		DestAccountNum:  "1234567",
		DestAccountName: "ヤマダ タロウ",
	}, now)
}

func TestNewTransfer_Defaults(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newPendingTransfer("tr-1", now)

	if tr.Status != domain.StatusPending {
		t.Errorf("Status = %s, want PENDING", tr.Status)
	}
	if tr.Version != 1 {
		t.Errorf("Version = %d, want 1", tr.Version)
	}
	if !tr.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v", tr.CreatedAt, now)
	}
	if !tr.UpdatedAt.Equal(now) {
		t.Errorf("UpdatedAt = %v, want %v", tr.UpdatedAt, now)
	}
	if tr.ApplyNo != nil {
		t.Errorf("ApplyNo = %v, want nil", tr.ApplyNo)
	}
}

func TestTransfer_MarkRequested(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newPendingTransfer("tr-1", now)

	later := now.Add(time.Minute)
	if err := tr.MarkRequested("AP123", later); err != nil {
		t.Fatalf("MarkRequested: %v", err)
	}
	if tr.Status != domain.StatusRequested {
		t.Errorf("Status = %s, want REQUESTED", tr.Status)
	}
	if tr.ApplyNo == nil || *tr.ApplyNo != "AP123" {
		t.Errorf("ApplyNo = %v, want AP123", tr.ApplyNo)
	}
	if !tr.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", tr.UpdatedAt, later)
	}
}

func TestTransfer_MarkRequested_FromTerminalReturnsError(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newPendingTransfer("tr-1", now)
	tr.Status = domain.StatusSettled

	err := tr.MarkRequested("AP999", now.Add(time.Minute))
	if !errors.Is(err, domain.ErrInvalidTransition) {
		t.Errorf("err = %v, want ErrInvalidTransition", err)
	}
}

func TestTransfer_MarkFailed(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := newPendingTransfer("tr-1", now)

	if err := tr.MarkFailed("over limit", now); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if tr.Status != domain.StatusFailed {
		t.Errorf("Status = %s, want FAILED", tr.Status)
	}
	if tr.LastError == nil || *tr.LastError != "over limit" {
		t.Errorf("LastError = %v, want over limit", tr.LastError)
	}
}
