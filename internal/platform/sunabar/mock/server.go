// Package mock sunabar API の httptest ベースのモックサーバを提供する。
// テストおよびローカル開発で使用する。 公開するレスポンス形は package sunabar のクライアントが
// パースできる範囲を必要十分にカバーする。
package mock

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Server httptest サーバのラッパー。
// 内部に保持した state で振込依頼に対する応答を組み立てる。
// API 呼び出しの記録 ( 主に再送検出 ) を持ち、 同じ IdempotencyKey の再依頼で同じ ApplyNo を返す。
type Server struct {
	*httptest.Server

	mu        sync.Mutex
	transfers map[string]*storedTransfer // applyNo -> stored
	keyIndex  map[string]string          // idempotencyKey -> applyNo
	now       func() time.Time
}

// storedTransfer モックが保持する振込状態。 状態遷移は GetTransferStatus 呼び出しのたびに進める。
type storedTransfer struct {
	ApplyNo        string
	IdempotencyKey string
	Amount         int64
	Status         string
	StatusDetail   string
	AcceptedAt     time.Time
	UpdatedAt      time.Time
	pollCount      int
}

// Option NewServer のオプション。
type Option func(*Server)

// WithClock テスト容易性のため now 関数を差し替える。
func WithClock(now func() time.Time) Option {
	return func(s *Server) {
		if now != nil {
			s.now = now
		}
	}
}

// NewServer モックサーバを起動する。 Close で停止する。
func NewServer(opts ...Option) *Server {
	s := &Server{
		transfers: make(map[string]*storedTransfer),
		keyIndex:  make(map[string]string),
		now:       func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(s)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/personal/v1/accounts", s.handleAccounts)
	mux.HandleFunc("/personal/v1/accounts/balance", s.handleBalance)
	mux.HandleFunc("/personal/v1/accounts/transactions", s.handleTransactions)
	mux.HandleFunc("/personal/v1/transfers", s.handleTransfer)
	mux.HandleFunc("/personal/v1/transfers/status", s.handleTransferStatus)
	mux.HandleFunc("/personal/v1/virtual-accounts", s.handleVirtualAccount)

	s.Server = httptest.NewServer(mux)
	return s
}

// writeJSON status と body を JSON レスポンスとして書き出す。
// Encode の失敗時は ResponseWriter にエラーを書く ( ヘッダ既送出のためエラー応答は限定的 ) 。
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}

// writeError エラーレスポンスを JSON で返す。
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// handleAccounts GET /personal/v1/accounts 。
func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": []map[string]any{
			{
				"accountId":          "ACC0001",
				"branchCode":         "001",
				"accountNumber":      "1234567",
				"accountTypeCode":    "1",
				"accountName":        "スナバ タロウ",
				"primaryAccountCode": "1",
			},
		},
	})
}

// handleBalance GET /personal/v1/accounts/balance?accountId=... 。
func (s *Server) handleBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.URL.Query().Get("accountId")
	if id == "" {
		writeError(w, http.StatusBadRequest, "accountId required")
		return
	}
	now := s.now()
	writeJSON(w, http.StatusOK, map[string]any{
		"accountId":    id,
		"currencyCode": "JPY",
		"balance":      1000000,
		"baseDate":     now.Format("2006-01-02"),
		"baseTime":     now.Format("15:04:05"),
	})
}

// handleTransactions GET /personal/v1/accounts/transactions?accountId=...&from=...&to=... 。
func (s *Server) handleTransactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	id := r.URL.Query().Get("accountId")
	if id == "" {
		writeError(w, http.StatusBadRequest, "accountId required")
		return
	}
	now := s.now()
	writeJSON(w, http.StatusOK, map[string]any{
		"accountId":    id,
		"currencyCode": "JPY",
		"baseDate":     now.Format("2006-01-02"),
		"baseTime":     now.Format("15:04:05"),
		"hasNext":      false,
		"count":        1,
		"transactions": []map[string]any{
			{
				"transactionDate": now.Format("2006-01-02"),
				"valueDate":       now.Format("2006-01-02"),
				"transactionType": "credit",
				"amount":          10000,
				"balance":         1010000,
				"remarks":         "TEST DEPOSIT",
				"itemKey":         "ITEM-0001",
			},
		},
	})
}

// handleTransfer POST /personal/v1/transfers 。
// x-idempotency-key ヘッダを必須とし、 同じキーの再送には同じ ApplyNo を返す。
func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idem := r.Header.Get("x-idempotency-key")
	if idem == "" {
		writeError(w, http.StatusBadRequest, "idempotency key required")
		return
	}
	var body struct {
		Amount int64 `json:"amount"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.keyIndex[idem]; ok {
		st := s.transfers[existing]
		writeJSON(w, http.StatusOK, map[string]any{
			"applyNo":  st.ApplyNo,
			"status":   st.Status,
			"acceptAt": st.AcceptedAt.UTC().Format(time.RFC3339),
		})
		return
	}

	now := s.now()
	applyNo := "AP" + idem
	if len(applyNo) > 16 {
		applyNo = applyNo[:16]
	}
	st := &storedTransfer{
		ApplyNo:        applyNo,
		IdempotencyKey: idem,
		Amount:         body.Amount,
		Status:         "AcceptedToBank",
		StatusDetail:   "受付完了",
		AcceptedAt:     now,
		UpdatedAt:      now,
	}
	s.transfers[applyNo] = st
	s.keyIndex[idem] = applyNo

	writeJSON(w, http.StatusOK, map[string]any{
		"applyNo":  st.ApplyNo,
		"status":   st.Status,
		"acceptAt": st.AcceptedAt.UTC().Format(time.RFC3339),
	})
}

// handleTransferStatus GET /personal/v1/transfers/status?applyNo=... 。
// 1 回ごとに状態を進めることで結果照会のポーリングテストに使えるようにする。
func (s *Server) handleTransferStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	applyNo := r.URL.Query().Get("applyNo")
	if applyNo == "" {
		writeError(w, http.StatusBadRequest, "applyNo required")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.transfers[applyNo]
	if !ok {
		writeError(w, http.StatusNotFound, "applyNo not found")
		return
	}
	st.pollCount++
	st.UpdatedAt = s.now()
	switch st.pollCount {
	case 1:
		st.Status = "AcceptedToBank"
		st.StatusDetail = "銀行受付済み"
	case 2:
		st.Status = "Approved"
		st.StatusDetail = "承認済み"
	default:
		st.Status = "Settled"
		st.StatusDetail = "送金完了"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"applyNo":      st.ApplyNo,
		"status":       st.Status,
		"statusDetail": st.StatusDetail,
		"updatedAt":    st.UpdatedAt.UTC().Format(time.RFC3339),
	})
}

// handleVirtualAccount POST /personal/v1/virtual-accounts 。
func (s *Server) handleVirtualAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	idem := r.Header.Get("x-idempotency-key")
	if idem == "" {
		writeError(w, http.StatusBadRequest, "idempotency key required")
		return
	}
	now := s.now()
	expires := now.AddDate(0, 6, 0)
	writeJSON(w, http.StatusOK, map[string]any{
		"virtualAccountId": "VA-" + idem,
		"branchCode":       "001",
		"accountNumber":    "9876543",
		"expiresOn":        expires.Format("2006-01-02"),
	})
}
