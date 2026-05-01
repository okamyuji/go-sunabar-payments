package domain_test

import (
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/reconciliation/domain"
)

func TestInvoice_Apply_StatusTransitions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name   string
		amount int64
		paid   []int64 // 順に Apply する
		want   domain.InvoiceStatus
	}{
		{"unpaid stays open", 1000, nil, domain.InvoiceOpen},
		{"partial single", 1000, []int64{500}, domain.InvoicePartial},
		{"cleared single", 1000, []int64{1000}, domain.InvoiceCleared},
		{"cleared multiple", 1000, []int64{300, 700}, domain.InvoiceCleared},
		{"excess", 1000, []int64{1500}, domain.InvoiceExcess},
		{"refund back to open", 1000, []int64{1000, -1000}, domain.InvoiceOpen},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			inv := domain.NewInvoice("inv-1", c.amount, "VA-1", nil, now)
			for _, p := range c.paid {
				inv.Apply(p, now)
			}
			if inv.Status != c.want {
				t.Errorf("status = %s, want %s ( paid=%v )", inv.Status, c.want, c.paid)
			}
		})
	}
}

func TestInvoice_NewInvoice_Defaults(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	inv := domain.NewInvoice("inv-1", 1000, "VA-1", nil, now)
	if inv.Status != domain.InvoiceOpen {
		t.Errorf("status = %s, want OPEN", inv.Status)
	}
	if inv.PaidAmount != 0 {
		t.Errorf("PaidAmount = %d, want 0", inv.PaidAmount)
	}
	if !inv.CreatedAt.Equal(now) || !inv.UpdatedAt.Equal(now) {
		t.Errorf("timestamp 不整合")
	}
}
