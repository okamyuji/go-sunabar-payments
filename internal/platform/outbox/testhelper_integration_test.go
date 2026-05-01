//go:build integration

package outbox_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

// setupMySQL 統合テスト用に MySQL を準備する。
// INTEGRATION_DB_DSN が設定されていればそれを利用する ( CI / make compose-up 経由 ) 。
// 未設定なら testcontainers-go で MySQL 8.0 を起動する。
// 戻り値の cleanup はテスト終了時に呼ぶ ( t.Cleanup で登録済み ) 。
func setupMySQL(t *testing.T) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	var (
		dsn     string
		cleanup func()
	)

	if env := os.Getenv("INTEGRATION_DB_DSN"); env != "" {
		dsn = env
		cleanup = func() {}
	} else {
		container, err := tcmysql.Run(ctx,
			"mysql:8.0",
			tcmysql.WithDatabase("payments"),
			tcmysql.WithUsername("app"),
			tcmysql.WithPassword("app"),
		)
		if err != nil {
			t.Skipf("testcontainers mysql 起動に失敗 ( Docker 未稼働の可能性 ) : %v", err)
		}
		dsn, err = container.ConnectionString(ctx, "parseTime=true", "multiStatements=true", "loc=UTC")
		if err != nil {
			_ = container.Terminate(context.Background())
			t.Fatalf("connection string: %v", err)
		}
		cleanup = func() { _ = container.Terminate(context.Background()) }
	}
	t.Cleanup(cleanup)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 接続確立まで最大 60 秒待つ ( 立ち上げ直後を考慮 ) 。
	deadline := time.Now().Add(60 * time.Second)
	for {
		if pingErr := db.PingContext(ctx); pingErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping deadline 超過: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	resetSchema(t, db)
	applyMigrations(t, db)
	return db
}

// migrationsDir リポジトリ直下の migrations ディレクトリの絶対パスを返す。
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller 失敗")
	}
	// internal/platform/outbox/testhelper_integration_test.go から 3 階層上がリポジトリルート。
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	return filepath.Join(root, "migrations")
}

// resetSchema 既存のテーブルとマイグレーション履歴を削除し、 まっさらな状態に戻す。
// 後続マイグレーションで追加されたテーブルも忘れずに DROP する ( 全テスト DROP 安全 ) 。
func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
		"DROP TABLE IF EXISTS incoming_transactions",
		"DROP TABLE IF EXISTS invoices",
		"DROP TABLE IF EXISTS virtual_accounts",
		"DROP TABLE IF EXISTS accounts",
		"DROP TABLE IF EXISTS transfers",
		"DROP TABLE IF EXISTS event_processed",
		"DROP TABLE IF EXISTS outbox_events",
		"DROP TABLE IF EXISTS schema_migrations",
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("reset schema %q: %v", s, err)
		}
	}
}

// applyMigrations migrations/*.up.sql を順に流し込む。
// 個別ステートメントは ; で分割せずまとめて流し込み multiStatements=true 前提。
func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var ups []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		ups = append(ups, name)
	}
	// 名前昇順 ( 000001, 000002, ... ) で実行。
	for i := 0; i < len(ups); i++ {
		for j := i + 1; j < len(ups); j++ {
			if ups[i] > ups[j] {
				ups[i], ups[j] = ups[j], ups[i]
			}
		}
	}
	for _, name := range ups {
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		body := strings.TrimSpace(string(raw))
		if body == "" {
			continue
		}
		if _, err := db.Exec(body); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
}
