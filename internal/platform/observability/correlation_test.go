package observability_test

import (
	"context"
	"strings"
	"testing"

	"go-sunabar-payments/internal/platform/observability"
)

func TestNewCorrelationID_Unique(t *testing.T) {
	t.Parallel()
	a := observability.NewCorrelationID()
	b := observability.NewCorrelationID()
	if a == b {
		t.Errorf("ID 重複 a=%s b=%s", a, b)
	}
	if len(a) != 32 {
		t.Errorf("len = %d, want 32 ( 16 byte hex ) ", len(a))
	}
}

func TestWithCorrelationID_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := observability.WithCorrelationID(context.Background(), "abc-123")
	if got := observability.CorrelationIDFromContext(ctx); got != "abc-123" {
		t.Errorf("got = %q, want abc-123", got)
	}
}

func TestWithCorrelationID_GeneratesWhenEmpty(t *testing.T) {
	t.Parallel()
	ctx := observability.WithCorrelationID(context.Background(), "")
	got := observability.CorrelationIDFromContext(ctx)
	if got == "" {
		t.Errorf("ID 空 want generated")
	}
	if strings.Contains(got, " ") {
		t.Errorf("ID にスペース混入: %q", got)
	}
}
