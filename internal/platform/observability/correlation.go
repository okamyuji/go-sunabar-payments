// Package observability ログ、 相関ID、 メトリクス、 機微情報マスクなど横断的な観測性ヘルパを提供する。
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

// correlationKey context に相関 ID を載せるための非公開キー型。
type correlationKey struct{}

// CorrelationIDHeader HTTP ヘッダ名。 X-Request-ID 互換。
const CorrelationIDHeader = "X-Request-ID"

// WithCorrelationID 相関 ID を context に載せて返す。 空文字列なら新規生成する。
func WithCorrelationID(ctx context.Context, id string) context.Context {
	if id == "" {
		id = NewCorrelationID()
	}
	return context.WithValue(ctx, correlationKey{}, id)
}

// CorrelationIDFromContext context から相関 ID を取り出す。 未設定なら空文字列を返す。
func CorrelationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(correlationKey{}).(string)
	return v
}

// NewCorrelationID 新しい相関 ID ( 16 バイト hex ) を生成する。
// crypto/rand で乱数を取り、 失敗時は固定文字列にフォールバックする ( ID 不在による null 参照を避ける ) 。
func NewCorrelationID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "fallback-id"
	}
	return hex.EncodeToString(buf[:])
}
