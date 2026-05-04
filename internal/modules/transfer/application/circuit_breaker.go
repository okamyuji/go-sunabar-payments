package application

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen は外部ゲートウェイ保護のため呼び出しを拒否したことを示す。
var ErrCircuitOpen = errors.New("circuit breaker open")

// CircuitBreakerConfig CircuitBreaker の動作設定。
type CircuitBreakerConfig struct {
	// FailureThreshold 連続失敗がこの回数に到達すると OPEN にする。
	FailureThreshold int
	// ResetTimeout OPEN 後、この時間を過ぎたら次の呼び出しを許可する。
	ResetTimeout time.Duration
}

// DefaultCircuitBreakerConfig 画像の設計に合わせたデフォルト値。
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     30 * time.Second,
	}
}

// CircuitBreaker 外部ゲートウェイ呼び出しの可否と結果記録を担う。
type CircuitBreaker interface {
	Allow(now time.Time) error
	RecordSuccess(now time.Time)
	RecordFailure(now time.Time)
}

// InMemoryCircuitBreaker プロセス内で完結する軽量な circuit breaker。
type InMemoryCircuitBreaker struct {
	mu               sync.Mutex
	failureThreshold int
	resetTimeout     time.Duration
	failureCount     int
	openedAt         time.Time
	open             bool
}

// NewCircuitBreaker InMemoryCircuitBreaker を生成する。
func NewCircuitBreaker(cfg CircuitBreakerConfig) *InMemoryCircuitBreaker {
	defaults := DefaultCircuitBreakerConfig()
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaults.FailureThreshold
	}
	if cfg.ResetTimeout <= 0 {
		cfg.ResetTimeout = defaults.ResetTimeout
	}
	return &InMemoryCircuitBreaker{
		failureThreshold: cfg.FailureThreshold,
		resetTimeout:     cfg.ResetTimeout,
	}
}

// Allow CLOSED なら nil、OPEN かつ reset timeout 前なら ErrCircuitOpen を返す。
func (b *InMemoryCircuitBreaker) Allow(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.open {
		return nil
	}
	if now.Sub(b.openedAt) < b.resetTimeout {
		return ErrCircuitOpen
	}
	b.open = false
	b.failureCount = 0
	b.openedAt = time.Time{}
	return nil
}

// RecordSuccess 成功時に失敗回数と OPEN 状態をリセットする。
func (b *InMemoryCircuitBreaker) RecordSuccess(_ time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.open = false
	b.failureCount = 0
	b.openedAt = time.Time{}
}

// RecordFailure retryable 失敗を記録し、閾値到達で OPEN にする。
func (b *InMemoryCircuitBreaker) RecordFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.open {
		return
	}
	b.failureCount++
	if b.failureCount >= b.failureThreshold {
		b.open = true
		b.openedAt = now
	}
}
