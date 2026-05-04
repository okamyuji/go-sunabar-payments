package application_test

import (
	"errors"
	"testing"
	"time"

	"go-sunabar-payments/internal/modules/transfer/application"
)

func TestCircuitBreaker_OpensAfterFailureThreshold(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     30 * time.Second,
	})

	for i := 0; i < 3; i++ {
		if err := cb.Allow(now); err != nil {
			t.Fatalf("Allow before threshold: %v", err)
		}
		cb.RecordFailure(now)
	}

	if err := cb.Allow(now); !errors.Is(err, application.ErrCircuitOpen) {
		t.Fatalf("Allow after threshold = %v, want ErrCircuitOpen", err)
	}
}

func TestCircuitBreaker_AllowsAfterResetTimeout(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1,
		ResetTimeout:     30 * time.Second,
	})

	cb.RecordFailure(now)
	if err := cb.Allow(now.Add(29 * time.Second)); !errors.Is(err, application.ErrCircuitOpen) {
		t.Fatalf("Allow before reset timeout = %v, want ErrCircuitOpen", err)
	}
	if err := cb.Allow(now.Add(30 * time.Second)); err != nil {
		t.Fatalf("Allow after reset timeout: %v", err)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 2,
		ResetTimeout:     30 * time.Second,
	})

	cb.RecordFailure(now)
	cb.RecordSuccess(now)
	cb.RecordFailure(now)

	if err := cb.Allow(now); err != nil {
		t.Fatalf("Allow after success reset: %v", err)
	}
}
