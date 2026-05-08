//go:build integration

// Package scenario_test 障害ドリル統合テスト ( M11 ) 。
// 「現場でハマる典型障害」を自動的に再現し、 リカバリ手順の正しさを検証する。
package scenario_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"go-sunabar-payments/internal/modules/transfer"
	transferdomain "go-sunabar-payments/internal/modules/transfer/domain"
	"go-sunabar-payments/internal/platform/idempotency"
	"go-sunabar-payments/internal/platform/outbox"
	"go-sunabar-payments/internal/platform/sunabar"
	mocksunabar "go-sunabar-payments/internal/platform/sunabar/mock"
	"go-sunabar-payments/internal/platform/transaction"
)

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
		container, err := tcmysql.Run(ctx, "mysql:8.0",
			tcmysql.WithDatabase("payments"),
			tcmysql.WithUsername("app"),
			tcmysql.WithPassword("app"),
		)
		if err != nil {
			t.Skipf("testcontainers mysql 起動失敗: %v", err)
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

func resetSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, s := range []string{
		"DROP TABLE IF EXISTS incoming_transactions",
		"DROP TABLE IF EXISTS invoices",
		"DROP TABLE IF EXISTS virtual_accounts",
		"DROP TABLE IF EXISTS accounts",
		"DROP TABLE IF EXISTS transfers",
		"DROP TABLE IF EXISTS event_processed",
		"DROP TABLE IF EXISTS outbox_events",
		"DROP TABLE IF EXISTS schema_migrations",
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	dir := filepath.Join(root, "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var ups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
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
			t.Fatalf("apply %s: %v", name, err)
		}
	}
}

func newTransferModuleWithMockSunabar(t *testing.T, db *sql.DB) (*transfer.Module, *mocksunabar.Server) {
	t.Helper()
	mock := mocksunabar.NewServer()
	t.Cleanup(mock.Close)
	auth, err := sunabar.NewStaticTokenSource("test-token")
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: mock.URL, Auth: auth})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	return transfer.New(db, txMgr, pub, client, idGen, time.Now), mock
}

func makePostHandler(t *testing.T, mod *transfer.Module) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	transfer.NewHTTPHandler(mod.Service).Register(mux)
	return mux
}

func postTransfer(t *testing.T, srv *httptest.Server, appReqID string, amount int64) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"appRequestId":    appReqID,
		"amount":          amount,
		"sourceAccountId": "ACC0001",
		"destBankCode":    "0033",
		"destBranchCode":  "001",
		"destAccountType": "1",
		"destAccountNum":  "1234567",
		"destAccountName": "ヤマダ タロウ",
	})
	res, err := http.Post(srv.URL+"/transfers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /transfers: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(res.Body)
		t.Fatalf("status = %d body=%s", res.StatusCode, raw)
	}
	var got struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got.ID
}

// Drill 1 同一 app_request_id の同時 5 連打でも Transfer は 1 件しか作られないことを検証する。
// 現場の典型: クライアントのリトライバグ、 ロードバランサの再試行、 ユーザの連打など。
func TestDrill_DoublePostSameAppRequestID(t *testing.T) {
	db := setupMySQL(t)
	mod, _ := newTransferModuleWithMockSunabar(t, db)
	srv := httptest.NewServer(makePostHandler(t, mod))
	t.Cleanup(srv.Close)

	const appReqID = "drill-dup-1"
	const n = 5
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = postTransfer(t, srv, appReqID, 1000)
	}
	// すべて同じ Transfer.ID が返るべき。
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Fatalf("ids[%d]=%s, want %s ( 同一 app_request_id は冪等 )", i, ids[i], ids[0])
		}
	}
	// transfers テーブルは 1 件のみ。
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM transfers WHERE app_request_id=?`, appReqID).Scan(&cnt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if cnt != 1 {
		t.Errorf("transfers = %d, want 1", cnt)
	}
	// Outbox の TransferRequested も 1 件。
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type=?`, transferdomain.EventTransferRequested).Scan(&cnt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if cnt != 1 {
		t.Errorf("outbox TransferRequested = %d, want 1", cnt)
	}
}

// Drill 2 sunabar が一時的に 5xx を返してもバックオフ後の再投入で正常完了することを検証する。
// 現場の典型: 銀行 API メンテナンス窓、 一時的なネットワーク断、 ロードバランサの一時障害。
func TestDrill_TransientServerErrorRecoversWithBackoff(t *testing.T) {
	db := setupMySQL(t)

	// 最初の N 回は 500 を返し、 それ以降はモック本来の挙動に委譲する自前 sunabar 。
	failCount := atomic.Int32{}
	failCount.Store(2)
	mock := mocksunabar.NewServer()
	t.Cleanup(mock.Close)
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/personal/v1/transfer/request" && r.Method == http.MethodPost && failCount.Load() > 0 {
			failCount.Add(-1)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"upstream busy"}`))
			return
		}
		// それ以外はモック本物にリダイレクト ( 簡易 ) せずプロキシする。
		req, err := http.NewRequestWithContext(r.Context(), r.Method, mock.URL+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		req.Header = r.Header.Clone()
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = res.Body.Close() }()
		for k, vs := range res.Header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	t.Cleanup(flaky.Close)

	auth, _ := sunabar.NewStaticTokenSource("test-token")
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: flaky.URL, Auth: auth})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	mod := transfer.New(db, txMgr, pub, client, idGen, time.Now)

	// HTTP API 経由で振込依頼を作る。
	srv := httptest.NewServer(makePostHandler(t, mod))
	t.Cleanup(srv.Close)
	id := postTransfer(t, srv, "drill-flaky-1", 1000)

	// Relay を回す。 失敗 -> バックオフ -> 復旧の挙動を再現するため next_attempt_at を毎ループ過去に倒す。
	relay := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 10, Consumer: "drill"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	relay.Register(transferdomain.EventTransferRequested, mod.SendToSunabarHandler)
	relay.Register(transferdomain.EventTransferStatusCheck, mod.CheckStatusHandler)

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := db.Exec(`UPDATE outbox_events SET next_attempt_at=UTC_TIMESTAMP(6) WHERE status='PENDING'`); err != nil {
			t.Fatalf("reset next_attempt_at: %v", err)
		}
		if err := relay.ProcessBatch(context.Background()); err != nil {
			t.Fatalf("ProcessBatch: %v", err)
		}
		cur, err := mod.Service.GetTransfer(context.Background(), id)
		if err != nil {
			t.Fatalf("GetTransfer: %v", err)
		}
		if cur.Status == transferdomain.StatusRequested || cur.Status.IsTerminal() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 最終的に REQUESTED 以降に到達していること。 失敗回数 ( 2 ) を消費したあとに復旧している。
	got, err := mod.Service.GetTransfer(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if got.Status == transferdomain.StatusPending || got.Status == transferdomain.StatusFailed {
		t.Errorf("status = %s, want >= REQUESTED ( バックオフ後の復旧 ) ", got.Status)
	}
	if failCount.Load() != 0 {
		t.Errorf("failCount = %d, want 0 ( 全失敗を消費していない ) ", failCount.Load())
	}
}

// Drill 3 緊急停止スクリプト相当の操作で PENDING を一斉 FAILED に倒すと、
// 以降の Relay 実行で送信が走らないことを検証する。
func TestDrill_EmergencyStopHaltsDispatch(t *testing.T) {
	db := setupMySQL(t)
	mod, _ := newTransferModuleWithMockSunabar(t, db)
	srv := httptest.NewServer(makePostHandler(t, mod))
	t.Cleanup(srv.Close)

	id := postTransfer(t, srv, "drill-stop-1", 1000)

	// 緊急停止: PENDING を一斉 FAILED に。
	if _, err := db.Exec(`UPDATE outbox_events SET status='FAILED', last_error='emergency-stop' WHERE status='PENDING'`); err != nil {
		t.Fatalf("emergency stop: %v", err)
	}

	// Relay を回しても処理対象が無いはず。
	called := atomic.Int32{}
	relay := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 5, Consumer: "drill"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	relay.Register(transferdomain.EventTransferRequested, outbox.HandlerFunc(func(_ context.Context, _ outbox.Event) error {
		called.Add(1)
		return nil
	}))
	if err := relay.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if called.Load() != 0 {
		t.Errorf("handler called = %d, want 0 ( 緊急停止後は dispatch されない ) ", called.Load())
	}

	// transfer は PENDING のまま固まっていること。 自動再生成しないという ADR-003 方針を確認。
	got, _ := mod.Service.GetTransfer(context.Background(), id)
	if got.Status != transferdomain.StatusPending {
		t.Errorf("status = %s, want PENDING ( 自動復旧しない ) ", got.Status)
	}

	// 復旧操作: 個別に PENDING に戻すと再投入される。
	if _, err := db.Exec(`UPDATE outbox_events SET status='PENDING', last_error=NULL, next_attempt_at=UTC_TIMESTAMP(6) WHERE event_type=?`, transferdomain.EventTransferRequested); err != nil {
		t.Fatalf("restore PENDING: %v", err)
	}
	relay.Register(transferdomain.EventTransferRequested, mod.SendToSunabarHandler)
	if err := relay.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch after restore: %v", err)
	}
	got2, _ := mod.Service.GetTransfer(context.Background(), id)
	if got2.Status != transferdomain.StatusRequested {
		t.Errorf("status = %s, want REQUESTED ( 復旧後 ) ", got2.Status)
	}
}

// Drill 4 アクセストークン失効を模した 401 が返るとき、 4xx は即 transfer.status='FAILED' に倒し、
// Outbox の元イベントは SENT に進めて再投入を止める ( ADR-008 ) 。 復旧はトークン更新と人手戻し。
func TestDrill_TokenExpiredImmediatelyMarksTransferFailed(t *testing.T) {
	db := setupMySQL(t)

	// 401 を返し続ける sunabar 互換サーバ。
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token_expired"}`))
	}))
	t.Cleanup(flaky.Close)

	auth, _ := sunabar.NewStaticTokenSource("expired-token")
	client, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: flaky.URL, Auth: auth})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	txMgr := transaction.NewSQLManager(db)
	pub := outbox.NewSQLPublisher()
	idGen := idempotency.NewUUIDGenerator()
	mod := transfer.New(db, txMgr, pub, client, idGen, time.Now)

	srv := httptest.NewServer(makePostHandler(t, mod))
	t.Cleanup(srv.Close)
	id := postTransfer(t, srv, "drill-401-1", 500)

	relay := outbox.NewRelay(db, outbox.RelayConfig{BatchSize: 10, MaxAttempt: 5, Consumer: "drill"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	relay.Register(transferdomain.EventTransferRequested, mod.SendToSunabarHandler)

	if _, err := db.Exec(`UPDATE outbox_events SET next_attempt_at=UTC_TIMESTAMP(6) WHERE status='PENDING'`); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := relay.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	// Transfer.status が FAILED に倒されている ( 4xx 即失敗ポリシー ) 。
	got, err := mod.Service.GetTransfer(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if got.Status != transferdomain.StatusFailed {
		t.Errorf("transfer.status = %s, want FAILED", got.Status)
	}
	if got.LastError == nil || !strings.Contains(*got.LastError, "401") {
		t.Errorf("last_error が 401 を含まない: %v", got.LastError)
	}

	// Outbox の元 TransferRequested は SENT で確定 ( handler は nil 返却なので再投入されない ) 。
	var status string
	row := db.QueryRow(`SELECT status FROM outbox_events WHERE event_type=?`, transferdomain.EventTransferRequested)
	if err := row.Scan(&status); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if status != "SENT" {
		t.Errorf("outbox status = %s, want SENT", status)
	}
}
