package application

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen は外部ゲートウェイ保護のため呼び出しを拒否したことを示す。
var ErrCircuitOpen = errors.New("circuit breaker open")

// State circuit breaker の 3 状態。
type State int

const (
	// StateClosed 通常状態。 すべての呼び出しを通過させ、 失敗を集計する。
	StateClosed State = iota
	// StateOpen 障害判定中。 ResetTimeout が経過するまで全コールを fail-fast で拒否する。
	StateOpen
	// StateHalfOpen 試験的復帰中。 HalfOpenMaxCalls 件だけ通し、 結果を見て CLOSED か OPEN に戻す。
	StateHalfOpen
)

// String slog 出力用の人間可読表現。
func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreakerConfig CircuitBreaker の動作設定。
//
// 連続失敗トリガ ( FailureThreshold ) と失敗率トリガ ( FailureRate / WindowSize / MinRequests )
// を OR で評価する。 どちらかの閾値を超えれば OPEN にする。
//   - FailureRate = 0 のときは失敗率トリガを無効化し、 連続失敗のみで判断する。
//   - WindowSize = 0 のときも失敗率トリガを無効化する。
//
// HalfOpenMaxCalls は ResetTimeout 経過後の HALF_OPEN 中に並列で許す probe 数。
// 0 なら 1 件 ( 単一 probe ) として扱う。
type CircuitBreakerConfig struct {
	FailureThreshold int           // 連続失敗がこの回数に到達すると OPEN
	ResetTimeout     time.Duration // OPEN 後 HALF_OPEN に遷移するまでの時間
	FailureRate      float64       // 0-1; window 内の失敗率がこれを超えると OPEN ( 0 なら無効 )
	MinRequests      int           // window 内サンプル数がこれ未満なら failure rate を評価しない
	WindowSize       time.Duration // rolling window 長さ ( 0 なら window モード無効 )
	HalfOpenMaxCalls int           // HALF_OPEN 中に並列で許す probe 数 ( デフォルト 1 )
}

// DefaultCircuitBreakerConfig 標準的な保護的デフォルトを返す。
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     30 * time.Second,
		FailureRate:      0.5,
		MinRequests:      5,
		WindowSize:       30 * time.Second,
		HalfOpenMaxCalls: 1,
	}
}

// CircuitBreaker 外部ゲートウェイ呼び出しの可否と結果記録を担う。
type CircuitBreaker interface {
	Allow(now time.Time) error
	RecordSuccess(now time.Time)
	RecordFailure(now time.Time)
	State(now time.Time) State
}

type sample struct {
	at      time.Time
	success bool
}

// InMemoryCircuitBreaker プロセス内で完結する 3 状態 circuit breaker。
type InMemoryCircuitBreaker struct {
	mu               sync.Mutex
	failureThreshold int
	resetTimeout     time.Duration
	failureRate      float64
	minRequests      int
	windowSize       time.Duration
	halfOpenMaxCalls int

	state            State
	failureCount     int       // 連続失敗カウンタ ( 成功でリセット )
	openedAt         time.Time // OPEN になった時刻
	halfOpenInflight int       // HALF_OPEN 中の進行中 probe 数
	samples          []sample  // rolling window 用の最近のサンプル
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
	if cfg.HalfOpenMaxCalls <= 0 {
		cfg.HalfOpenMaxCalls = 1
	}
	if cfg.MinRequests < 0 {
		cfg.MinRequests = 0
	}
	if cfg.FailureRate < 0 {
		cfg.FailureRate = 0
	}
	if cfg.FailureRate > 1 {
		cfg.FailureRate = 1
	}
	return &InMemoryCircuitBreaker{
		failureThreshold: cfg.FailureThreshold,
		resetTimeout:     cfg.ResetTimeout,
		failureRate:      cfg.FailureRate,
		minRequests:      cfg.MinRequests,
		windowSize:       cfg.WindowSize,
		halfOpenMaxCalls: cfg.HalfOpenMaxCalls,
		state:            StateClosed,
	}
}

// Allow 呼び出し許可を判定する。
//   - CLOSED: 常に nil。
//   - OPEN: ResetTimeout 経過なら HALF_OPEN に遷移し probe を 1 件許す。 未経過なら ErrCircuitOpen。
//   - HALF_OPEN: 進行中 probe が HalfOpenMaxCalls 未満なら通す。 超えたら ErrCircuitOpen。
func (b *InMemoryCircuitBreaker) Allow(now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.maybeTransitionToHalfOpenLocked(now)

	switch b.state {
	case StateClosed:
		return nil
	case StateHalfOpen:
		if b.halfOpenInflight >= b.halfOpenMaxCalls {
			return ErrCircuitOpen
		}
		b.halfOpenInflight++
		return nil
	default:
		return ErrCircuitOpen
	}
}

// RecordSuccess 成功時にカウンタをリセットし、 HALF_OPEN なら CLOSED に復帰する。
func (b *InMemoryCircuitBreaker) RecordSuccess(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.appendSampleLocked(now, true)

	switch b.state {
	case StateHalfOpen:
		b.toClosedLocked()
	case StateClosed:
		b.failureCount = 0
	}
}

// RecordFailure retryable 失敗を記録し、 閾値到達で OPEN にする。 HALF_OPEN 中の失敗は即 OPEN に戻す。
func (b *InMemoryCircuitBreaker) RecordFailure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.appendSampleLocked(now, false)

	switch b.state {
	case StateHalfOpen:
		b.toOpenLocked(now)
		return
	case StateOpen:
		// 既に OPEN なら何もしない ( probe で来ているはずだが念のため )。
		return
	}

	b.failureCount++
	if b.failureCount >= b.failureThreshold || b.failureRateExceededLocked(now) {
		b.toOpenLocked(now)
	}
}

// State 現在の状態を返す。 OPEN かつ ResetTimeout 経過なら HALF_OPEN に遷移したうえで返す。
func (b *InMemoryCircuitBreaker) State(now time.Time) State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.maybeTransitionToHalfOpenLocked(now)
	return b.state
}

// maybeTransitionToHalfOpenLocked OPEN かつ ResetTimeout 経過時に HALF_OPEN へ遷移する。
func (b *InMemoryCircuitBreaker) maybeTransitionToHalfOpenLocked(now time.Time) {
	if b.state != StateOpen {
		return
	}
	if now.Sub(b.openedAt) < b.resetTimeout {
		return
	}
	b.state = StateHalfOpen
	b.halfOpenInflight = 0
}

// toOpenLocked OPEN 状態に遷移する。 reset window を再計算するため openedAt を更新する。
func (b *InMemoryCircuitBreaker) toOpenLocked(now time.Time) {
	b.state = StateOpen
	b.openedAt = now
	b.halfOpenInflight = 0
}

// toClosedLocked CLOSED 状態に戻す。 連続失敗カウンタと probe inflight をリセットする。
func (b *InMemoryCircuitBreaker) toClosedLocked() {
	b.state = StateClosed
	b.failureCount = 0
	b.openedAt = time.Time{}
	b.halfOpenInflight = 0
}

// appendSampleLocked rolling window 用にサンプルを追加し、 古いサンプルを破棄する。
func (b *InMemoryCircuitBreaker) appendSampleLocked(now time.Time, success bool) {
	if b.windowSize <= 0 {
		return
	}
	b.samples = append(b.samples, sample{at: now, success: success})
	b.pruneSamplesLocked(now)
}

// pruneSamplesLocked window から外れた古いサンプルを切り捨てる。
func (b *InMemoryCircuitBreaker) pruneSamplesLocked(now time.Time) {
	if b.windowSize <= 0 {
		b.samples = b.samples[:0]
		return
	}
	cutoff := now.Add(-b.windowSize)
	idx := 0
	for ; idx < len(b.samples); idx++ {
		if !b.samples[idx].at.Before(cutoff) {
			break
		}
	}
	if idx > 0 {
		// スライスを縮める ( underlying array は GC に任せず再利用する )。
		b.samples = b.samples[idx:]
	}
}

// failureRateExceededLocked 失敗率が閾値を超えているか判定する。
func (b *InMemoryCircuitBreaker) failureRateExceededLocked(now time.Time) bool {
	if b.failureRate <= 0 || b.windowSize <= 0 {
		return false
	}
	b.pruneSamplesLocked(now)
	total := len(b.samples)
	if total < b.minRequests || total == 0 {
		return false
	}
	failed := 0
	for _, s := range b.samples {
		if !s.success {
			failed++
		}
	}
	rate := float64(failed) / float64(total)
	return rate >= b.failureRate
}
