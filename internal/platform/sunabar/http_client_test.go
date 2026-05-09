package sunabar_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-sunabar-payments/internal/platform/sunabar"
	mocksvr "go-sunabar-payments/internal/platform/sunabar/mock"
)

// errorAuthSource Token 呼び出し時に常にエラーを返す AuthSource 。
type errorAuthSource struct {
	err error
}

func (e errorAuthSource) Token(_ context.Context) (string, error) {
	return "", e.err
}

func newClient(t *testing.T, baseURL string, auth sunabar.AuthSource) *sunabar.HTTPClient {
	t.Helper()
	c, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{
		BaseURL: baseURL,
		Auth:    auth,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	return c
}

func staticAuth(t *testing.T) sunabar.AuthSource {
	t.Helper()
	a, err := sunabar.NewStaticTokenSource("test-token")
	if err != nil {
		t.Fatalf("NewStaticTokenSource: %v", err)
	}
	return a
}

func TestNewHTTPClient_Validations(t *testing.T) {
	t.Parallel()
	if _, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{Auth: staticAuth(t)}); !errors.Is(err, sunabar.ErrEmptyBaseURL) {
		t.Errorf("err = %v, want ErrEmptyBaseURL", err)
	}
	if _, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{BaseURL: "http://x"}); !errors.Is(err, sunabar.ErrNilAuthSource) {
		t.Errorf("err = %v, want ErrNilAuthSource", err)
	}
}

func TestRequestTransfer_ReturnsApplyNo(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t))
	res, err := c.RequestTransfer(context.Background(), sunabar.TransferRequest{
		IdempotencyKey:  "abc12345",
		SourceAccountID: "ACC0001",
		Amount:          1000,
		DestBankCode:    "0033",
		DestBranchCode:  "001",
		DestAccountType: "1",
		DestAccountNum:  "1234567",
		DestAccountName: "ヤマダ タロウ",
		TransferDate:    "2026-05-01",
	})
	if err != nil {
		t.Fatalf("RequestTransfer: %v", err)
	}
	if res.ApplyNo == "" {
		t.Errorf("ApplyNo が空")
	}
	if res.Status != "AcceptedToBank" {
		t.Errorf("Status = %q, want AcceptedToBank", res.Status)
	}
	if res.AcceptAt.IsZero() {
		t.Errorf("AcceptAt がゼロ値")
	}
}

func TestRequestTransfer_SameIdempotencyKeyReturnsSameApplyNo(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t))
	req := sunabar.TransferRequest{
		IdempotencyKey:  "fixed-idem-key-001",
		SourceAccountID: "ACC0001",
		Amount:          5000,
	}
	first, err := c.RequestTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := c.RequestTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if first.ApplyNo != second.ApplyNo {
		t.Errorf("first.ApplyNo = %q, second.ApplyNo = %q", first.ApplyNo, second.ApplyNo)
	}
}

func TestRequestTransfer_AuthSourceErrorPropagated(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	wantErr := errors.New("auth boom")
	c := newClient(t, srv.URL, errorAuthSource{err: wantErr})
	// 入力バリデーションを通過させてから AuthSource 呼び出しでエラーになることを確認する。
	_, err := c.RequestTransfer(context.Background(), sunabar.TransferRequest{
		IdempotencyKey:  "k",
		SourceAccountID: "ACC0001",
		Amount:          1,
		DestBankCode:    "0033",
		DestBranchCode:  "001",
		DestAccountType: "1",
		DestAccountNum:  "1234567",
		DestAccountName: "ﾔﾏﾀﾞ ﾀﾛｳ",
		TransferDate:    "2026-05-01",
	})
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want wraps %v", err, wantErr)
	}
}

func TestDo_HTTPErrorReturnsAPIError(t *testing.T) {
	t.Parallel()

	// 4xx を必ず返す自前 httptest 。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad"}`))
	}))
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t))
	_, err := c.GetAccounts(context.Background())
	if err == nil {
		t.Fatalf("err = nil, want APIError")
	}
	var apiErr *sunabar.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusBadRequest)
	}
	if !strings.Contains(apiErr.RawBody, "bad") {
		t.Errorf("RawBody = %q", apiErr.RawBody)
	}
}

func TestGetAccounts_ParsesPrimaryFlag(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t))
	accounts, err := c.GetAccounts(context.Background())
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("len = %d, want 1", len(accounts))
	}
	if !accounts[0].PrimaryAccountFlag {
		t.Errorf("PrimaryAccountFlag = false, want true ( primaryAccountCode='1' をフラグ化 ) ")
	}
}

func TestGetTransferStatus_AdvancesOnEachPoll(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t))
	// まず振込依頼を作る。
	res, err := c.RequestTransfer(context.Background(), sunabar.TransferRequest{
		IdempotencyKey:  "poll-test-001",
		SourceAccountID: "ACC0001",
		Amount:          1,
	})
	if err != nil {
		t.Fatalf("RequestTransfer: %v", err)
	}

	// 1 回目の照会 → AcceptedToBank
	st1, err := c.GetTransferStatus(context.Background(), res.ApplyNo)
	if err != nil {
		t.Fatalf("status1: %v", err)
	}
	if st1.Status != "AcceptedToBank" {
		t.Errorf("status1 = %q, want AcceptedToBank", st1.Status)
	}
	// 2 回目 → Approved
	st2, err := c.GetTransferStatus(context.Background(), res.ApplyNo)
	if err != nil {
		t.Fatalf("status2: %v", err)
	}
	if st2.Status != "Approved" {
		t.Errorf("status2 = %q, want Approved", st2.Status)
	}
	// 3 回目以降 → Settled
	st3, err := c.GetTransferStatus(context.Background(), res.ApplyNo)
	if err != nil {
		t.Fatalf("status3: %v", err)
	}
	if st3.Status != "Settled" {
		t.Errorf("status3 = %q, want Settled", st3.Status)
	}
}

func TestIssueVirtualAccount_ParsesIssuedVaList(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	corpAuth, err := sunabar.NewStaticTokenSource("corp-token")
	if err != nil {
		t.Fatalf("NewStaticTokenSource: %v", err)
	}
	c, err := sunabar.NewHTTPClient(sunabar.HTTPClientConfig{
		BaseURL:       srv.URL,
		Auth:          staticAuth(t),
		CorporateAuth: corpAuth,
	})
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	va, err := c.IssueVirtualAccount(context.Background(), sunabar.VirtualAccountRequest{
		IdempotencyKey:   "va-key-001",
		Memo:             "memo",
		RaID:             "RA-TEST-001",
		VaHolderNameKana: "ﾍﾟｲﾒﾝﾄﾗﾎﾞ",
		ExpiresOn:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("IssueVirtualAccount: %v", err)
	}
	if va.VirtualAccountID == "" {
		t.Errorf("VirtualAccountID 空")
	}
	if va.ExpiresOn.IsZero() {
		t.Errorf("ExpiresOn ゼロ値")
	}
}

func TestIssueVirtualAccount_RequiresCorporateAuth(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t)) // CorporateAuth 未設定
	_, err := c.IssueVirtualAccount(context.Background(), sunabar.VirtualAccountRequest{
		IdempotencyKey:   "va-no-corp",
		RaID:             "RA",
		VaHolderNameKana: "ﾍﾟｲﾒﾝﾄﾗﾎﾞ",
	})
	if !errors.Is(err, sunabar.ErrNoCorporateAuth) {
		t.Errorf("err = %v, want ErrNoCorporateAuth", err)
	}
}

// newListTransactionsServer count フィールドを差し替え可能な /personal/v1/accounts/transactions モック。
func newListTransactionsServer(t *testing.T, count string) *httptest.Server {
	t.Helper()
	body := fmt.Sprintf(`{"accountId":"ACC0001","currencyCode":"JPY","baseDate":"2026-05-01","baseTime":"00:00:00","hasNext":"false","count":%q,"transactions":[]}`, count)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/personal/v1/accounts/transactions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListTransactions_ParsesCount(t *testing.T) {
	t.Parallel()
	srv := newListTransactionsServer(t, "3")
	c := newClient(t, srv.URL, staticAuth(t))
	got, err := c.ListTransactions(context.Background(), "ACC0001", sunabar.ListTransactionsParams{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if got.Count != 3 {
		t.Errorf("Count = %d, want 3", got.Count)
	}
}

// TestListTransactions_AcceptsLargeCount Count は int64 で持つため
// 32bit 上限を越える値も narrowing せずそのまま保持できる ( CWE-190 回避 ) 。
func TestListTransactions_AcceptsLargeCount(t *testing.T) {
	t.Parallel()
	huge := fmt.Sprintf("%d", int64(math.MaxInt32)+1)
	srv := newListTransactionsServer(t, huge)
	c := newClient(t, srv.URL, staticAuth(t))
	got, err := c.ListTransactions(context.Background(), "ACC0001", sunabar.ListTransactionsParams{})
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if got.Count != int64(math.MaxInt32)+1 {
		t.Errorf("Count = %d, want %d", got.Count, int64(math.MaxInt32)+1)
	}
}

// TestListTransactions_RejectsNegativeCount 件数は仕様上 0 以上のはず。 負値は API 異常として弾く。
func TestListTransactions_RejectsNegativeCount(t *testing.T) {
	t.Parallel()
	srv := newListTransactionsServer(t, "-1")
	c := newClient(t, srv.URL, staticAuth(t))
	if _, err := c.ListTransactions(context.Background(), "ACC0001", sunabar.ListTransactionsParams{}); err == nil {
		t.Errorf("負値で err = nil")
	}
}
