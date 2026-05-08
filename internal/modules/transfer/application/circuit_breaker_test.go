package application_test

import (
	"errors"
	"sync"
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

	for range 3 {
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

// TestCircuitBreaker_StateTransitionsClosedOpenHalfOpenClosed CLOSED -> OPEN -> HALF_OPEN -> CLOSED の
// 3 状態遷移を State() で観測できることを検証する。
func TestCircuitBreaker_StateTransitionsClosedOpenHalfOpenClosed(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 2,
		ResetTimeout:     1 * time.Second,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	if got := cb.State(base); got != application.StateClosed {
		t.Errorf("initial state = %v, want CLOSED", got)
	}

	cb.RecordFailure(base)
	cb.RecordFailure(base)
	if got := cb.State(base); got != application.StateOpen {
		t.Errorf("after threshold state = %v, want OPEN", got)
	}

	if err := cb.Allow(base.Add(500 * time.Millisecond)); !errors.Is(err, application.ErrCircuitOpen) {
		t.Errorf("Allow during OPEN window = %v, want ErrCircuitOpen", err)
	}

	probeAt := base.Add(2 * time.Second)
	if err := cb.Allow(probeAt); err != nil {
		t.Errorf("Allow after timeout ( probe ) = %v, want nil", err)
	}
	if got := cb.State(probeAt); got != application.StateHalfOpen {
		t.Errorf("state after probe-allow = %v, want HALF_OPEN", got)
	}

	cb.RecordSuccess(probeAt)
	if got := cb.State(probeAt); got != application.StateClosed {
		t.Errorf("state after probe success = %v, want CLOSED", got)
	}
}

// TestCircuitBreaker_HalfOpenLimitsConcurrentProbes HALF_OPEN 中は HalfOpenMaxCalls 件しか
// 並列に通さない ( デフォルト 1 件 ) ことを検証する。
func TestCircuitBreaker_HalfOpenLimitsConcurrentProbes(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1,
		ResetTimeout:     1 * time.Second,
		HalfOpenMaxCalls: 1,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb.RecordFailure(base)

	probeAt := base.Add(2 * time.Second)
	if err := cb.Allow(probeAt); err != nil {
		t.Fatalf("first probe Allow = %v, want nil", err)
	}
	if err := cb.Allow(probeAt); !errors.Is(err, application.ErrCircuitOpen) {
		t.Errorf("concurrent second probe Allow = %v, want ErrCircuitOpen", err)
	}
}

// TestCircuitBreaker_HalfOpenFailureReopensWithNewWindow HALF_OPEN 中の失敗で再 OPEN し、
// 新しい reset window が開始することを検証する。
func TestCircuitBreaker_HalfOpenFailureReopensWithNewWindow(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1,
		ResetTimeout:     1 * time.Second,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb.RecordFailure(base)

	probeAt := base.Add(2 * time.Second)
	if err := cb.Allow(probeAt); err != nil {
		t.Fatalf("probe Allow: %v", err)
	}
	cb.RecordFailure(probeAt)

	if got := cb.State(probeAt); got != application.StateOpen {
		t.Errorf("state after probe failure = %v, want OPEN", got)
	}
	if err := cb.Allow(probeAt.Add(500 * time.Millisecond)); !errors.Is(err, application.ErrCircuitOpen) {
		t.Errorf("Allow inside new OPEN window = %v, want ErrCircuitOpen", err)
	}
}

// TestCircuitBreaker_HalfOpen_ConcurrentSafe goroutine 並列で複数 Allow を呼んでも
// 通過する probe は HalfOpenMaxCalls 件以内であることを race 検出と合わせて検証する。
func TestCircuitBreaker_HalfOpen_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1,
		ResetTimeout:     1 * time.Second,
		HalfOpenMaxCalls: 1,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb.RecordFailure(base)

	probeAt := base.Add(2 * time.Second)
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	allowed := 0
	var mu sync.Mutex
	for range n {
		go func() {
			defer wg.Done()
			if err := cb.Allow(probeAt); err == nil {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 1 {
		t.Errorf("HALF_OPEN concurrent allowed = %d, want 1", allowed)
	}
}

// TestCircuitBreaker_RollingWindowOpensByFailureRate WindowSize と FailureRate / MinRequests を
// 組み合わせると、 連続失敗ではなく失敗率で OPEN できることを検証する。
func TestCircuitBreaker_RollingWindowOpensByFailureRate(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1000, // 連続失敗トリガを実質無効化
		FailureRate:      0.5,
		MinRequests:      4,
		WindowSize:       30 * time.Second,
		ResetTimeout:     1 * time.Second,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cb.RecordSuccess(base)
	cb.RecordSuccess(base.Add(1 * time.Second))
	cb.RecordFailure(base.Add(2 * time.Second))
	cb.RecordFailure(base.Add(3 * time.Second))

	if got := cb.State(base.Add(3 * time.Second)); got != application.StateOpen {
		t.Errorf("state = %v, want OPEN ( 50%% failure rate ) ", got)
	}
}

// TestCircuitBreaker_RollingWindowDropsOldSamples 窓外のサンプルが評価から除外されることを検証する。
// MinRequests=3 で開始時の 2 failures は失敗率トリガに乗らない。 その後 window 経過で失敗が evict され、
// 新しい 2 successes だけが残るため OPEN しない。 evict されない場合は [F,F,S,S] = rate 0.5 で OPEN するはず。
func TestCircuitBreaker_RollingWindowDropsOldSamples(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1000,
		FailureRate:      0.5,
		MinRequests:      3,
		WindowSize:       10 * time.Second,
		ResetTimeout:     30 * time.Second,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cb.RecordFailure(base)
	cb.RecordFailure(base.Add(1 * time.Second))

	later := base.Add(20 * time.Second)
	cb.RecordSuccess(later)
	cb.RecordSuccess(later.Add(1 * time.Second))

	if got := cb.State(later.Add(1 * time.Second)); got != application.StateClosed {
		t.Errorf("state = %v, want CLOSED ( old failures should be dropped from window ) ", got)
	}
}

// TestCircuitBreaker_RollingWindowRespectsMinRequests サンプル数が MinRequests に達するまでは
// 失敗率閾値を超えても OPEN しないことを検証する。
func TestCircuitBreaker_RollingWindowRespectsMinRequests(t *testing.T) {
	t.Parallel()
	cb := application.NewCircuitBreaker(application.CircuitBreakerConfig{
		FailureThreshold: 1000,
		FailureRate:      0.5,
		MinRequests:      5,
		WindowSize:       30 * time.Second,
		ResetTimeout:     1 * time.Second,
	})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cb.RecordFailure(base)
	if got := cb.State(base); got != application.StateClosed {
		t.Errorf("state with too few samples = %v, want CLOSED", got)
	}
}
