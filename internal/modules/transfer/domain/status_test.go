package domain_test

import (
	"errors"
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/transfer/domain"
)

func TestStatus_IsTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		s    domain.Status
		want bool
	}{
		{domain.StatusPending, false},
		{domain.StatusRequested, false},
		{domain.StatusAwaitingApproval, false},
		{domain.StatusApproved, false},
		{domain.StatusSettled, true},
		{domain.StatusFailed, true},
	}
	for _, c := range cases {
		if got := c.s.IsTerminal(); got != c.want {
			t.Errorf("%s.IsTerminal() = %v, want %v", c.s, got, c.want)
		}
	}
}

func TestTransfer_Transition(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		from    domain.Status
		to      domain.Status
		wantErr error
	}{
		{"pending to requested", domain.StatusPending, domain.StatusRequested, nil},
		{"pending to failed", domain.StatusPending, domain.StatusFailed, nil},
		{"pending to settled invalid", domain.StatusPending, domain.StatusSettled, domain.ErrInvalidTransition},
		{"requested to awaiting", domain.StatusRequested, domain.StatusAwaitingApproval, nil},
		{"requested to approved direct", domain.StatusRequested, domain.StatusApproved, nil},
		{"requested to settled direct", domain.StatusRequested, domain.StatusSettled, nil},
		{"awaiting to approved", domain.StatusAwaitingApproval, domain.StatusApproved, nil},
		{"approved to settled", domain.StatusApproved, domain.StatusSettled, nil},
		{"settled is terminal", domain.StatusSettled, domain.StatusRequested, domain.ErrInvalidTransition},
		{"failed is terminal", domain.StatusFailed, domain.StatusSettled, domain.ErrInvalidTransition},
		{"same state idempotent", domain.StatusPending, domain.StatusPending, nil},
		{"settled to settled idempotent", domain.StatusSettled, domain.StatusSettled, nil},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			tr := &domain.Transfer{Status: c.from}
			err := tr.Transition(c.to, now)
			if c.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if tr.Status != c.to {
					t.Errorf("status = %s, want %s", tr.Status, c.to)
				}
				if !tr.UpdatedAt.Equal(now) {
					t.Errorf("UpdatedAt = %v, want %v", tr.UpdatedAt, now)
				}
			} else if !errors.Is(err, c.wantErr) {
				t.Errorf("err = %v, want %v", err, c.wantErr)
			}
		})
	}
}
