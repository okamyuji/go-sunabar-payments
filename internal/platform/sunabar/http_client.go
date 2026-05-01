package sunabar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// HTTPClient sunabar / 本番 BaaS の HTTP API 実装。
// net/http のみで構成し、 認可は AuthSource に委譲する。
type HTTPClient struct {
	baseURL    *url.URL
	httpClient *http.Client
	auth       AuthSource
}

// HTTPClientConfig HTTPClient の初期化設定。
type HTTPClientConfig struct {
	// BaseURL 必須。 末尾スラッシュは付けても付けなくてもよい。
	BaseURL string
	// HTTPClient nil 渡しならタイムアウト 10 秒のデフォルトを使う。
	HTTPClient *http.Client
	// Auth 必須。 アクセストークン取得の方式を切り替えるための AuthSource 。
	Auth AuthSource
}

// 静的エラー。
var (
	// ErrEmptyBaseURL BaseURL が空のときに返る。
	ErrEmptyBaseURL = errors.New("sunabar empty base url")
	// ErrNilAuthSource Auth が nil のときに返る。
	ErrNilAuthSource = errors.New("sunabar nil auth source")
)

// NewHTTPClient HTTPClient を生成する。
func NewHTTPClient(cfg HTTPClientConfig) (*HTTPClient, error) {
	if cfg.BaseURL == "" {
		return nil, ErrEmptyBaseURL
	}
	if cfg.Auth == nil {
		return nil, ErrNilAuthSource
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("sunabar parse base url: %w", err)
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &HTTPClient{baseURL: u, httpClient: hc, auth: cfg.Auth}, nil
}

// APIError sunabar / BaaS が返したエラーレスポンスを表す。
type APIError struct {
	StatusCode int
	RawBody    string
}

// Error エラー文字列を返す。
func (e *APIError) Error() string {
	return fmt.Sprintf("sunabar api error: status=%d body=%s", e.StatusCode, e.RawBody)
}

// do HTTP リクエストの共通実装。 アクセストークン付与と JSON エンコード / デコードを担う。
func (c *HTTPClient) do(ctx context.Context, method, path string, query url.Values, body any, out any, idempotencyKey string) error {
	u := *c.baseURL
	u.Path = path
	if query != nil {
		u.RawQuery = query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reqBody)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	token, err := c.auth.Token(ctx)
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	req.Header.Set("Accept", "application/json;charset=UTF-8")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	}
	// sunabar は x-access-token ヘッダ。 本番 BaaS は Authorization Bearer 。
	// 環境別の差し替えは後続マイルストーンで AuthSource 経由に組み込む。
	req.Header.Set("x-access-token", token)
	if idempotencyKey != "" {
		req.Header.Set("x-idempotency-key", idempotencyKey)
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer func() {
		// レスポンスボディは必ずクローズする ( 接続再利用のため ) 。
		_ = res.Body.Close()
	}()

	if res.StatusCode >= 400 {
		respBody, readErr := io.ReadAll(res.Body)
		if readErr != nil {
			return &APIError{StatusCode: res.StatusCode, RawBody: ""}
		}
		return &APIError{StatusCode: res.StatusCode, RawBody: string(respBody)}
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// 各エンドポイントのレスポンス表現を package 内 type で持つ。 公開しない。

type accountResponse struct {
	AccountID          string `json:"accountId"`
	BranchCode         string `json:"branchCode"`
	AccountNumber      string `json:"accountNumber"`
	AccountTypeCode    string `json:"accountTypeCode"`
	AccountName        string `json:"accountName"`
	PrimaryAccountCode string `json:"primaryAccountCode"`
}

type accountsResponseEnvelope struct {
	Accounts []accountResponse `json:"accounts"`
}

type balanceResponse struct {
	AccountID    string `json:"accountId"`
	CurrencyCode string `json:"currencyCode"`
	Balance      int64  `json:"balance"`
	BaseDate     string `json:"baseDate"`
	BaseTime     string `json:"baseTime"`
}

type transactionResponse struct {
	TransactionDate string `json:"transactionDate"`
	ValueDate       string `json:"valueDate"`
	TransactionType string `json:"transactionType"`
	Amount          int64  `json:"amount"`
	Balance         int64  `json:"balance"`
	Remarks         string `json:"remarks"`
	ItemKey         string `json:"itemKey"`
}

type transactionListResponse struct {
	AccountID    string                `json:"accountId"`
	CurrencyCode string                `json:"currencyCode"`
	BaseDate     string                `json:"baseDate"`
	BaseTime     string                `json:"baseTime"`
	HasNext      bool                  `json:"hasNext"`
	Count        int                   `json:"count"`
	Transactions []transactionResponse `json:"transactions"`
}

type transferRequestPayload struct {
	SourceAccountID string `json:"sourceAccountId"`
	Amount          int64  `json:"amount"`
	DestBankCode    string `json:"destBankCode"`
	DestBranchCode  string `json:"destBranchCode"`
	DestAccountType string `json:"destAccountType"`
	DestAccountNum  string `json:"destAccountNumber"`
	DestAccountName string `json:"destAccountName"`
	TransferDate    string `json:"transferDate"`
	Remarks         string `json:"remarks,omitempty"`
}

type transferResponse struct {
	ApplyNo  string `json:"applyNo"`
	Status   string `json:"status"`
	AcceptAt string `json:"acceptAt"`
}

type transferStatusResponse struct {
	ApplyNo      string `json:"applyNo"`
	Status       string `json:"status"`
	StatusDetail string `json:"statusDetail"`
	UpdatedAt    string `json:"updatedAt"`
}

type virtualAccountRequestPayload struct {
	Memo      string `json:"memo,omitempty"`
	ExpiresOn string `json:"expiresOn,omitempty"`
}

type virtualAccountResponse struct {
	VirtualAccountID string `json:"virtualAccountId"`
	BranchCode       string `json:"branchCode"`
	AccountNumber    string `json:"accountNumber"`
	ExpiresOn        string `json:"expiresOn"`
}

// GetAccounts 口座一覧を取得する。
func (c *HTTPClient) GetAccounts(ctx context.Context) ([]Account, error) {
	var env accountsResponseEnvelope
	if err := c.do(ctx, http.MethodGet, "/personal/v1/accounts", nil, nil, &env, ""); err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(env.Accounts))
	for _, a := range env.Accounts {
		out = append(out, Account{
			AccountID:          a.AccountID,
			BranchCode:         a.BranchCode,
			AccountNumber:      a.AccountNumber,
			AccountTypeCode:    a.AccountTypeCode,
			AccountName:        a.AccountName,
			PrimaryAccountFlag: a.PrimaryAccountCode == "1",
		})
	}
	return out, nil
}

// GetBalance 残高を取得する。
func (c *HTTPClient) GetBalance(ctx context.Context, accountID string) (*Balance, error) {
	q := url.Values{"accountId": []string{accountID}}
	var resp balanceResponse
	if err := c.do(ctx, http.MethodGet, "/personal/v1/accounts/balance", q, nil, &resp, ""); err != nil {
		return nil, err
	}
	return &Balance{
		AccountID:    resp.AccountID,
		CurrencyCode: resp.CurrencyCode,
		Balance:      resp.Balance,
		BaseDate:     resp.BaseDate,
		BaseTime:     resp.BaseTime,
	}, nil
}

// ListTransactions 入出金明細を取得する。
func (c *HTTPClient) ListTransactions(ctx context.Context, accountID string, params ListTransactionsParams) (*TransactionList, error) {
	q := url.Values{
		"accountId": []string{accountID},
	}
	if !params.From.IsZero() {
		q.Set("from", params.From.UTC().Format("2006-01-02"))
	}
	if !params.To.IsZero() {
		q.Set("to", params.To.UTC().Format("2006-01-02"))
	}
	var resp transactionListResponse
	if err := c.do(ctx, http.MethodGet, "/personal/v1/accounts/transactions", q, nil, &resp, ""); err != nil {
		return nil, err
	}
	out := &TransactionList{
		AccountID:    resp.AccountID,
		CurrencyCode: resp.CurrencyCode,
		BaseDate:     resp.BaseDate,
		BaseTime:     resp.BaseTime,
		HasNext:      resp.HasNext,
		Count:        resp.Count,
	}
	out.Transactions = make([]Transaction, 0, len(resp.Transactions))
	for _, t := range resp.Transactions {
		// transactionResponse と Transaction はフィールド構成が完全一致するので直接変換する。
		out.Transactions = append(out.Transactions, Transaction(t))
	}
	return out, nil
}

// RequestTransfer 振込を依頼する。
// IdempotencyKey は必須。 sunabar / BaaS 側はこのキーで重複抑止する。
func (c *HTTPClient) RequestTransfer(ctx context.Context, req TransferRequest) (*TransferResult, error) {
	body := transferRequestPayload{
		SourceAccountID: req.SourceAccountID,
		Amount:          req.Amount,
		DestBankCode:    req.DestBankCode,
		DestBranchCode:  req.DestBranchCode,
		DestAccountType: req.DestAccountType,
		DestAccountNum:  req.DestAccountNum,
		DestAccountName: req.DestAccountName,
		TransferDate:    req.TransferDate,
		Remarks:         req.Remarks,
	}
	var resp transferResponse
	if err := c.do(ctx, http.MethodPost, "/personal/v1/transfers", nil, body, &resp, req.IdempotencyKey); err != nil {
		return nil, err
	}
	at, err := parseSunabarTime(resp.AcceptAt)
	if err != nil {
		return nil, fmt.Errorf("parse acceptAt: %w", err)
	}
	return &TransferResult{
		ApplyNo:  resp.ApplyNo,
		Status:   resp.Status,
		AcceptAt: at,
	}, nil
}

// GetTransferStatus 振込結果を照会する。
func (c *HTTPClient) GetTransferStatus(ctx context.Context, applyNo string) (*TransferStatusResult, error) {
	q := url.Values{"applyNo": []string{applyNo}}
	var resp transferStatusResponse
	if err := c.do(ctx, http.MethodGet, "/personal/v1/transfers/status", q, nil, &resp, ""); err != nil {
		return nil, err
	}
	updated, err := parseSunabarTime(resp.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updatedAt: %w", err)
	}
	return &TransferStatusResult{
		ApplyNo:      resp.ApplyNo,
		Status:       resp.Status,
		StatusDetail: resp.StatusDetail,
		UpdatedAt:    updated,
	}, nil
}

// IssueVirtualAccount バーチャル口座を発行する。
func (c *HTTPClient) IssueVirtualAccount(ctx context.Context, req VirtualAccountRequest) (*VirtualAccount, error) {
	payload := virtualAccountRequestPayload{Memo: req.Memo}
	if !req.ExpiresOn.IsZero() {
		payload.ExpiresOn = req.ExpiresOn.UTC().Format("2006-01-02")
	}
	var resp virtualAccountResponse
	if err := c.do(ctx, http.MethodPost, "/personal/v1/virtual-accounts", nil, payload, &resp, req.IdempotencyKey); err != nil {
		return nil, err
	}
	exp, err := parseSunabarDate(resp.ExpiresOn)
	if err != nil {
		return nil, fmt.Errorf("parse expiresOn: %w", err)
	}
	return &VirtualAccount{
		VirtualAccountID: resp.VirtualAccountID,
		BranchCode:       resp.BranchCode,
		AccountNumber:    resp.AccountNumber,
		ExpiresOn:        exp,
	}, nil
}

// parseSunabarTime sunabar が返す日時文字列 ( RFC 3339 と Unix 秒の両対応 ) を time.Time に変換する。
// 空文字列はゼロ値を返す。
func parseSunabarTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	// Unix 秒のフォールバック ( 数値文字列 ) 。
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(n, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unsupported time format: %q", s)
}

// parseSunabarDate sunabar が返す YYYY-MM-DD 形式の日付を time.Time ( UTC ) に変換する。
func parseSunabarDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("unsupported date format: %q", s)
	}
	return t, nil
}
