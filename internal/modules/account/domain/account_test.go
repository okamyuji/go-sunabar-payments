package domain_test

import (
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/account/domain"
)

func TestNewAccount_DefaultsAreSane(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := domain.NewAccount("id-1", "ACC0001", "001", "1234567", "1", "ヤマダ タロウ", true, now)
	if a.PrimaryFlag != true {
		t.Errorf("PrimaryFlag = false, want true")
	}
	if !a.CreatedAt.Equal(now) || !a.UpdatedAt.Equal(now) {
		t.Errorf("timestamp 不整合 created=%v updated=%v", a.CreatedAt, a.UpdatedAt)
	}
	if a.Label != nil {
		t.Errorf("Label = %v, want nil", a.Label)
	}
}

func TestNewVirtualAccount_NilOptionals(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	va := domain.NewVirtualAccount("id-2", "VA-001", "001", "9999999", nil, nil, now)
	if va.Memo != nil {
		t.Errorf("Memo = %v, want nil", va.Memo)
	}
	if va.ExpiresOn != nil {
		t.Errorf("ExpiresOn = %v, want nil", va.ExpiresOn)
	}
}

func TestNewVirtualAccount_WithMemoAndExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	memo := "memo-1"
	exp := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	va := domain.NewVirtualAccount("id-3", "VA-002", "001", "9999998", &memo, &exp, now)
	if va.Memo == nil || *va.Memo != "memo-1" {
		t.Errorf("Memo = %v, want memo-1", va.Memo)
	}
	if va.ExpiresOn == nil || !va.ExpiresOn.Equal(exp) {
		t.Errorf("ExpiresOn = %v, want %v", va.ExpiresOn, exp)
	}
}
