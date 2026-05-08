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
	"strings"
	"time"
)

// HTTPClient sunabar / 本番 BaaS の HTTP API 実装。
// net/http のみで構成し、 認可は AuthSource に委譲する。
// 個人 API ( /personal/v1/* ) は personalAuth、 法人 API ( /corporation/v1/* ) は corporateAuth を使う。
// 法人系は VA 発行など一部のみで使うため、 corporateAuth は任意。
type HTTPClient struct {
	baseURL       *url.URL
	httpClient    *http.Client
	personalAuth  AuthSource
	corporateAuth AuthSource // optional; nil なら法人系メソッドはエラーを返す
}

// HTTPClientConfig HTTPClient の初期化設定。
type HTTPClientConfig struct {
	// BaseURL 必須。 末尾スラッシュは付けても付けなくてもよい。
	BaseURL string
	// HTTPClient nil 渡しならタイムアウト 10 秒のデフォルトを使う。
	HTTPClient *http.Client
	// Auth 必須。 個人 API ( /personal/v1/* ) のアクセストークン。
	Auth AuthSource
	// CorporateAuth 任意。 法人 API ( /corporation/v1/* ) のアクセストークン。
	// 設定しない場合、 IssueVirtualAccount はエラーになる。
	CorporateAuth AuthSource
}

// 静的エラー。
var (
	// ErrEmptyBaseURL BaseURL が空のときに返る。
	ErrEmptyBaseURL = errors.New("sunabar empty base url")
	// ErrNilAuthSource Auth が nil のときに返る。
	ErrNilAuthSource = errors.New("sunabar nil auth source")
	// ErrNoCorporateAuth 法人 API 用の AuthSource が未設定のときに返る。
	ErrNoCorporateAuth = errors.New("sunabar corporate auth not configured")
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
	return &HTTPClient{
		baseURL:       u,
		httpClient:    hc,
		personalAuth:  cfg.Auth,
		corporateAuth: cfg.CorporateAuth,
	}, nil
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
// path のプレフィックス ( "/personal/v1/" or "/corporation/v1/" ) を見て使うトークンを切り替える。
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

	auth := c.personalAuth
	if strings.HasPrefix(path, "/corporation/") {
		if c.corporateAuth == nil {
			return ErrNoCorporateAuth
		}
		auth = c.corporateAuth
	}
	token, err := auth.Token(ctx)
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

// balanceItem sunabar の残高照会レスポンス内の1要素。
// 実 API は数値も文字列で返すため、すべて string で受けてから変換する。
type balanceItem struct {
	AccountID    string `json:"accountId"`
	CurrencyCode string `json:"currencyCode"`
	Balance      string `json:"balance"`
	BaseDate     string `json:"baseDate"`
	BaseTime     string `json:"baseTime"`
}

// balancesResponse 実 sunabar API の残高照会レスポンス。 balances は配列。
type balancesResponse struct {
	Balances []balanceItem `json:"balances"`
}

// transactionResponse 入出金明細1件。 sunabar 実 API は金額・残高を文字列で返す。
type transactionResponse struct {
	TransactionDate string `json:"transactionDate"`
	ValueDate       string `json:"valueDate"`
	TransactionType string `json:"transactionType"`
	Amount          string `json:"amount"`
	Balance         string `json:"balance"`
	Remarks         string `json:"remarks"`
	ItemKey         string `json:"itemKey"`
}

// transactionListResponse 実 sunabar API は hasNext / count も文字列。
type transactionListResponse struct {
	AccountID    string                `json:"accountId"`
	CurrencyCode string                `json:"currencyCode"`
	BaseDate     string                `json:"baseDate"`
	BaseTime     string                `json:"baseTime"`
	HasNext      string                `json:"hasNext"`
	Count        string                `json:"count"`
	Transactions []transactionResponse `json:"transactions"`
}

// transferItem 振込依頼ボディ内の transfers 配列の要素。
// sunabar 実 API は数値を文字列で送る。
type transferItem struct {
	ItemID                string `json:"itemId"`
	TransferAmount        string `json:"transferAmount"`
	BeneficiaryBankCode   string `json:"beneficiaryBankCode"`
	BeneficiaryBranchCode string `json:"beneficiaryBranchCode"`
	AccountTypeCode       string `json:"accountTypeCode"`
	AccountNumber         string `json:"accountNumber"`
	BeneficiaryName       string `json:"beneficiaryName"`
}

// transferRequestPayload POST /personal/v1/transfer/request の bulk-shape ボディ。
// 1 件の振込でも transfers 配列に 1 要素入れて送る。
type transferRequestPayload struct {
	AccountID               string         `json:"accountId"`
	TransferDesignatedDate  string         `json:"transferDesignatedDate"` // YYYY-MM-DD
	TransferDateHolidayCode string         `json:"transferDateHolidayCode"`
	TotalCount              string         `json:"totalCount"`
	TotalAmount             string         `json:"totalAmount"`
	Transfers               []transferItem `json:"transfers"`
}

// transferResponse 振込依頼レスポンス ( 配列内 / トップレベル両対応 ) 。
// sunabar の実 API は ApplyNo を文字列で返し、 一部の項目はネストする。
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

// virtualAccountRequestPayload POST /corporation/v1/va/issue のボディ。
type virtualAccountRequestPayload struct {
	VaTypeCode        string `json:"vaTypeCode"`
	IssueRequestCount string `json:"issueRequestCount"`
	RaID              string `json:"raId"`
	VaContractAuthKey string `json:"vaContractAuthKey"`
	VaHolderNameKana  string `json:"vaHolderNameKana"`
	VaHolderNamePos   string `json:"vaHolderNamePos"`
}

// virtualAccountIssuedItem /corporation/v1/va/issue のレスポンスに含まれる発行済み VA 1件。
// 実 sunabar API は vaBranchCode / vaAccountNumber というフィールド名で返す。
type virtualAccountIssuedItem struct {
	VaID            string `json:"vaId"`
	VaBranchCode    string `json:"vaBranchCode"`
	VaAccountNumber string `json:"vaAccountNumber"`
}

// virtualAccountResponse /corporation/v1/va/issue のレスポンス。
// 配列キーは vaList ( issuedVaList ではない ) 。 vaList 以外にも vaTypeCode 等が併走するが今は使わない。
type virtualAccountResponse struct {
	VaTypeCode       string                     `json:"vaTypeCode"`
	VaTypeName       string                     `json:"vaTypeName"`
	VaHolderNameKana string                     `json:"vaHolderNameKana"`
	VaList           []virtualAccountIssuedItem `json:"vaList"`
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

// GetBalance 残高を取得する。 sunabar 実 API は /personal/v1/accounts/balances ( 複数形 ) で
// balances 配列に1要素以上を含むレスポンスを返す。 数値は全て文字列で返されるため変換する。
func (c *HTTPClient) GetBalance(ctx context.Context, accountID string) (*Balance, error) {
	q := url.Values{"accountId": []string{accountID}}
	var resp balancesResponse
	if err := c.do(ctx, http.MethodGet, "/personal/v1/accounts/balances", q, nil, &resp, ""); err != nil {
		return nil, err
	}
	if len(resp.Balances) == 0 {
		return nil, fmt.Errorf("sunabar empty balances for accountId=%s", accountID)
	}
	b := resp.Balances[0]
	amount, err := parseSunabarInt(b.Balance)
	if err != nil {
		return nil, fmt.Errorf("parse balance: %w", err)
	}
	return &Balance{
		AccountID:    b.AccountID,
		CurrencyCode: b.CurrencyCode,
		Balance:      amount,
		BaseDate:     b.BaseDate,
		BaseTime:     b.BaseTime,
	}, nil
}

// ListTransactions 入出金明細を取得する。 sunabar 実 API のクエリは dateFrom / dateTo 。
// レスポンスの hasNext / count / amount / balance はすべて文字列で返るため変換する。
func (c *HTTPClient) ListTransactions(ctx context.Context, accountID string, params ListTransactionsParams) (*TransactionList, error) {
	q := url.Values{
		"accountId": []string{accountID},
	}
	if !params.From.IsZero() {
		q.Set("dateFrom", params.From.UTC().Format("2006-01-02"))
	}
	if !params.To.IsZero() {
		q.Set("dateTo", params.To.UTC().Format("2006-01-02"))
	}
	var resp transactionListResponse
	if err := c.do(ctx, http.MethodGet, "/personal/v1/accounts/transactions", q, nil, &resp, ""); err != nil {
		return nil, err
	}
	count, err := parseSunabarInt(resp.Count)
	if err != nil {
		return nil, fmt.Errorf("parse count: %w", err)
	}
	out := &TransactionList{
		AccountID:    resp.AccountID,
		CurrencyCode: resp.CurrencyCode,
		BaseDate:     resp.BaseDate,
		BaseTime:     resp.BaseTime,
		HasNext:      strings.EqualFold(resp.HasNext, "true"),
		Count:        int(count),
	}
	out.Transactions = make([]Transaction, 0, len(resp.Transactions))
	for _, t := range resp.Transactions {
		amount, err := parseSunabarInt(t.Amount)
		if err != nil {
			return nil, fmt.Errorf("parse transaction amount: %w", err)
		}
		bal, err := parseSunabarInt(t.Balance)
		if err != nil {
			return nil, fmt.Errorf("parse transaction balance: %w", err)
		}
		out.Transactions = append(out.Transactions, Transaction{
			TransactionDate: t.TransactionDate,
			ValueDate:       t.ValueDate,
			TransactionType: t.TransactionType,
			Amount:          amount,
			Balance:         bal,
			Remarks:         t.Remarks,
			ItemKey:         t.ItemKey,
		})
	}
	return out, nil
}

// RequestTransfer 振込を依頼する。
// sunabar 実 API は POST /personal/v1/transfer/request で受け付け、 1 件でも transfers 配列に 1 要素入れて送る。
// 受取人名 ( BeneficiaryName ) は半角カナでなければバリデーションエラー (errorDetailsCode "220001") になる。
// IdempotencyKey はアプリ層 ( ヘッダ "Idempotency-Key" ) として送る。
func (c *HTTPClient) RequestTransfer(ctx context.Context, req TransferRequest) (*TransferResult, error) {
	if req.SourceAccountID == "" {
		return nil, errors.New("sunabar: SourceAccountID required")
	}
	if req.Amount <= 0 {
		return nil, errors.New("sunabar: Amount must be positive")
	}
	amountStr := strconv.FormatInt(req.Amount, 10)
	date := req.TransferDate
	if date == "" {
		date = time.Now().In(jstLocation()).Format("2006-01-02")
	}
	body := transferRequestPayload{
		AccountID:               req.SourceAccountID,
		TransferDesignatedDate:  date,
		TransferDateHolidayCode: "1",
		TotalCount:              "1",
		TotalAmount:             amountStr,
		Transfers: []transferItem{{
			ItemID:                "1",
			TransferAmount:        amountStr,
			BeneficiaryBankCode:   req.DestBankCode,
			BeneficiaryBranchCode: req.DestBranchCode,
			AccountTypeCode:       req.DestAccountType,
			AccountNumber:         req.DestAccountNum,
			BeneficiaryName:       req.DestAccountName,
		}},
	}
	var resp transferResponse
	if err := c.do(ctx, http.MethodPost, "/personal/v1/transfer/request", nil, body, &resp, req.IdempotencyKey); err != nil {
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

// GetTransferStatus 振込状況を照会する。 sunabar 実 API は
// GET /personal/v1/transfer/status?accountId=&queryKeyClass=&applyNo= 。
// queryKeyClass=2 = applyNo 指定で 1 件問い合わせ ( sunabar 環境変数の既定値に倣う ) 。
// accountID は依頼元の口座 ID 。 mock サーバとの後方互換のため accountID は省略可能。
func (c *HTTPClient) GetTransferStatus(ctx context.Context, applyNo string) (*TransferStatusResult, error) {
	q := url.Values{
		"applyNo":       []string{applyNo},
		"queryKeyClass": []string{"2"},
	}
	var resp transferStatusResponse
	if err := c.do(ctx, http.MethodGet, "/personal/v1/transfer/status", q, nil, &resp, ""); err != nil {
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

// IssueVirtualAccount バーチャル口座を発行する。 sunabar 実 API では
// POST /corporation/v1/va/issue ( 法人 API 専用 ) のため、 corporateAuth が必要。
// raId / vaTypeCode / vaHolderNameKana 等は呼び出し側から指定する。
func (c *HTTPClient) IssueVirtualAccount(ctx context.Context, req VirtualAccountRequest) (*VirtualAccount, error) {
	vaType := req.VaTypeCode
	if vaType == "" {
		vaType = "2"
	}
	count := req.IssueRequestCount
	if count == "" {
		count = "1"
	}
	pos := req.VaHolderNamePos
	if pos == "" {
		pos = "1"
	}
	payload := virtualAccountRequestPayload{
		VaTypeCode:        vaType,
		IssueRequestCount: count,
		RaID:              req.RaID,
		VaContractAuthKey: req.VaContractAuthKey,
		VaHolderNameKana:  req.VaHolderNameKana,
		VaHolderNamePos:   pos,
	}
	var resp virtualAccountResponse
	if err := c.do(ctx, http.MethodPost, "/corporation/v1/va/issue", nil, payload, &resp, req.IdempotencyKey); err != nil {
		return nil, err
	}
	if len(resp.VaList) == 0 {
		return nil, errors.New("sunabar: empty vaList in VA response")
	}
	first := resp.VaList[0]
	return &VirtualAccount{
		VirtualAccountID: first.VaID,
		BranchCode:       first.VaBranchCode,
		AccountNumber:    first.VaAccountNumber,
		ExpiresOn:        req.ExpiresOn, // sunabar VA は無期限のためアプリ側保存値をそのまま透過
	}, nil
}

// jstLocation Asia/Tokyo の time.Location をキャッシュなしで返す ( ロード失敗時は固定オフセット ) 。
func jstLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tokyo"); err == nil {
		return loc
	}
	return time.FixedZone("JST", 9*3600)
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

// parseSunabarInt sunabar 実 API は数値項目を文字列で返すため、空文字列は 0 に丸めつつ
// 通常は strconv.ParseInt で数値化する。 末尾に余計な空白が混じるケースに備えて TrimSpace する。
func parseSunabarInt(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse int %q: %w", s, err)
	}
	return n, nil
}
