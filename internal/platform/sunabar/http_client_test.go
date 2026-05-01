package sunabar_test

import (
	"context"
	"errors"
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
	_, err := c.RequestTransfer(context.Background(), sunabar.TransferRequest{IdempotencyKey: "k"})
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

func TestIssueVirtualAccount_ParsesExpiresOn(t *testing.T) {
	t.Parallel()
	srv := mocksvr.NewServer()
	t.Cleanup(srv.Close)

	c := newClient(t, srv.URL, staticAuth(t))
	va, err := c.IssueVirtualAccount(context.Background(), sunabar.VirtualAccountRequest{
		IdempotencyKey: "va-key-001",
		Memo:           "memo",
		ExpiresOn:      time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
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
