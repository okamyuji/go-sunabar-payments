// Package idempotency 冪等性キー / 内部 ID の生成抽象を提供する。
// アプリ層が UUID 等のライブラリを直接 import しないようにし、 テスト時は固定値を返すモックを使う。
package idempotency

import (
	"github.com/google/uuid"
)

// UUIDGenerator UUID v7 / v4 を返す本番用の IDGenerator 。
type UUIDGenerator struct{}

// NewUUIDGenerator UUIDGenerator を生成する。
func NewUUIDGenerator() *UUIDGenerator { return &UUIDGenerator{} }

// NewTransferID UUID v7 を返す。 v7 は時系列順に並ぶため Outbox や DB インデックスに有利。
func (g *UUIDGenerator) NewTransferID() string {
	v, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 の失敗は乱数源異常などの異常時のみ。 v4 にフォールバックする。
		return uuid.NewString()
	}
	return v.String()
}

// NewIdempotencyKey UUID v4 を返す。 sunabar / 本番 BaaS の冪等キー要件 ( CHAR(36) ) に合致する。
func (g *UUIDGenerator) NewIdempotencyKey() string {
	return uuid.NewString()
}
