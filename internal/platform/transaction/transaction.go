// Package transaction トランザクション境界を抽象化する Unit of Work を提供する。
// アプリケーション層は Manager を介してトランザクションを開始し、インフラ層のリポジトリは
// Tx を受け取って同一トランザクション内で動作する。
package transaction

import (
	"context"
	"database/sql"
)

// Tx トランザクション内で操作を実行するためのハンドル。
// 内部に *sql.Tx を保持し、リポジトリ実装はここから取り出してクエリ発行する。
type Tx interface {
	// SQL 内部の *sql.Tx を返す。インフラ層のリポジトリ実装でのみ使用する。
	SQL() *sql.Tx
}

// Manager トランザクションのライフサイクルを管理する。
// Do コールバック内のすべての操作を 1 つのトランザクションで実行し、戻り値の error が
// nil なら Commit、そうでなければ Rollback する。
type Manager interface {
	Do(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
}
