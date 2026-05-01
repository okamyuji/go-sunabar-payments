//go:build integration

package infrastructure_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"database/sql"
	_ "github.com/go-sql-driver/mysql"

	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/modules/transfer/infrastructure"
	"go-sunabar-payments/internal/platform/transaction"
)

// setupMySQL Outbox テストと同方針で、 INTEGRATION_DB_DSN または testcontainers で MySQL を起動し、
// マイグレーションを再適用する。
func setupMySQL(t *testing.T) *sql.DB {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	var dsn string
	var cleanup func()

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

	deadline := time.Now().Add(60 * time.Second)
	for {
		if pingErr := db.PingContext(ctx); pingErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping deadline 超過")
		}
		time.Sleep(500 * time.Millisecond)
	}

	resetSchema(t, db)
	applyMigrations(t, db)
	return db
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller 失敗")
	}
	// thisFile は internal/modules/transfer/infrastructure/<file>.go 。
	// infrastructure -> transfer -> modules -> internal -> root の 4 階層上がリポジトリルート。
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))
	return filepath.Join(root, "migrations")
}

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	stmts := []string{
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

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	for i := 0; i < len(ups); i++ {
		for j := i + 1; j < len(ups); j++ {
			if ups[i] > ups[j] {
				ups[i], ups[j] = ups[j], ups[i]
			}
		}
	}
	for _, name := range ups {
		raw, err := os.ReadFile(filepath.Join(dir, name))
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

// newTransfer テスト用 Transfer ファクトリ。 idempotency_key は CHAR(36) UUID 形式相当を作る。
func newTransfer(id, appReq string) *domain.Transfer {
	now := time.Now().UTC()
	idemKey := id // テスト用に id ( 36 文字 UUID ) をそのまま冪等キーとして再利用する。
	return domain.NewTransfer(id, appReq, idemKey, domain.TransferParams{
		Amount:          1000,
		SourceAccountID: "ACC0001",
		DestBankCode:    "0033",
		DestBranchCode:  "001",
		DestAccountType: "1",
		DestAccountNum:  "1234567",
		DestAccountName: "ヤマダ タロウ",
	}, now)
}

func TestSQLRepository_InsertFindUpdate(t *testing.T) {
	db := setupMySQL(t)
	repo := infrastructure.NewSQLRepository(db)
	mgr := transaction.NewSQLManager(db)
	ctx := context.Background()

	t1 := newTransfer("11111111-aaaa-7000-8000-000000000001", "app-1")
	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Insert(ctx, tx, t1)
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := repo.FindByID(ctx, t1.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.AppRequestID != t1.AppRequestID || got.Amount != t1.Amount {
		t.Errorf("got = %+v, want %+v", got, t1)
	}
	if got.Status != domain.StatusPending {
		t.Errorf("status = %s, want PENDING", got.Status)
	}

	got2, err := repo.FindByAppRequestID(ctx, t1.AppRequestID)
	if err != nil {
		t.Fatalf("FindByAppRequestID: %v", err)
	}
	if got2.ID != t1.ID {
		t.Errorf("ID = %s, want %s", got2.ID, t1.ID)
	}

	// MarkRequested 後の Update 検証。
	if err := got.MarkRequested("AP00000001", time.Now().UTC()); err != nil {
		t.Fatalf("MarkRequested: %v", err)
	}
	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Update(ctx, tx, got)
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got.Version != 2 {
		t.Errorf("Version = %d, want 2 ( Update で +1 ) ", got.Version)
	}

	got3, err := repo.FindByID(ctx, t1.ID)
	if err != nil {
		t.Fatalf("FindByID after update: %v", err)
	}
	if got3.Status != domain.StatusRequested {
		t.Errorf("status = %s, want REQUESTED", got3.Status)
	}
	if got3.ApplyNo == nil || *got3.ApplyNo != "AP00000001" {
		t.Errorf("ApplyNo = %v, want AP00000001", got3.ApplyNo)
	}
}

func TestSQLRepository_DuplicateAppRequestID(t *testing.T) {
	db := setupMySQL(t)
	repo := infrastructure.NewSQLRepository(db)
	mgr := transaction.NewSQLManager(db)
	ctx := context.Background()

	t1 := newTransfer("21111111-aaaa-7000-8000-000000000001", "app-dup")
	t2 := newTransfer("21111111-aaaa-7000-8000-000000000002", "app-dup") // 同一 app_request_id

	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Insert(ctx, tx, t1)
	}); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Insert(ctx, tx, t2)
	})
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("err = %v, want ErrAlreadyExists", err)
	}
}

func TestSQLRepository_FindByIDNotFound(t *testing.T) {
	db := setupMySQL(t)
	repo := infrastructure.NewSQLRepository(db)
	_, err := repo.FindByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSQLRepository_OptimisticLockMismatch(t *testing.T) {
	db := setupMySQL(t)
	repo := infrastructure.NewSQLRepository(db)
	mgr := transaction.NewSQLManager(db)
	ctx := context.Background()

	t1 := newTransfer("31111111-aaaa-7000-8000-000000000001", "app-ol")
	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Insert(ctx, tx, t1)
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// 同じ Transfer を 2 経路で読み出す。
	a, err := repo.FindByID(ctx, t1.ID)
	if err != nil {
		t.Fatalf("find a: %v", err)
	}
	b, err := repo.FindByID(ctx, t1.ID)
	if err != nil {
		t.Fatalf("find b: %v", err)
	}

	// a 経路で先に Update する。
	if err := a.MarkRequested("AP-A", time.Now().UTC()); err != nil {
		t.Fatalf("mark a: %v", err)
	}
	if err := mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Update(ctx, tx, a)
	}); err != nil {
		t.Fatalf("update a: %v", err)
	}

	// b 経路 ( 古い version ) で Update を試みると ErrConcurrentUpdate 。
	if err := b.MarkRequested("AP-B", time.Now().UTC()); err != nil {
		t.Fatalf("mark b: %v", err)
	}
	err = mgr.Do(ctx, func(ctx context.Context, tx transaction.Tx) error {
		return repo.Update(ctx, tx, b)
	})
	if !errors.Is(err, domain.ErrConcurrentUpdate) {
		t.Errorf("err = %v, want ErrConcurrentUpdate", err)
	}
}
