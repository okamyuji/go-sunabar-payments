package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// sqlTx database/sql の *sql.Tx を保持する Tx 実装。
type sqlTx struct {
	tx *sql.Tx
}

// SQL 内部の *sql.Tx を返す。
func (s *sqlTx) SQL() *sql.Tx { return s.tx }

// SQLManager database/sql で実装した Manager 。
type SQLManager struct {
	db *sql.DB
}

// NewSQLManager 与えられた *sql.DB から SQLManager を生成する。
func NewSQLManager(db *sql.DB) *SQLManager {
	return &SQLManager{db: db}
}

// Do 新しいトランザクションを開始し fn を実行する。
// fn がエラーを返した場合と panic 発生時は Rollback する。 panic は再送出する。
func (m *SQLManager) Do(ctx context.Context, fn func(ctx context.Context, tx Tx) error) (retErr error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("transaction begin: %w", err)
	}

	defer func() {
		if r := recover(); r != nil {
			// panic 発生時は Rollback してから再送出する。 ロールバック失敗は無視する ( panic 情報を優先 ) 。
			_ = tx.Rollback()
			panic(r)
		}
		if retErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				retErr = fmt.Errorf("%w ( rollback failed: %v ) ", retErr, rbErr)
			}
		}
	}()

	if err := fn(ctx, &sqlTx{tx: tx}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("transaction commit: %w", err)
	}
	return nil
}
