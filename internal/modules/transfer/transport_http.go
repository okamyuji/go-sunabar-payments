package transfer

import (
	"encoding/json"
	"errors"
	"net/http"

	"go-sunabar-payments/internal/modules/transfer/application"
	"go-sunabar-payments/internal/modules/transfer/domain"
)

// HTTPHandler 標準 net/http で実装した Transfer モジュールの HTTP ハンドラ。
// chi 等のルーターは使わず、 ServeMux に登録できる形で公開する。
type HTTPHandler struct {
	svc Service
}

// NewHTTPHandler HTTPHandler を生成する。
func NewHTTPHandler(svc Service) *HTTPHandler {
	return &HTTPHandler{svc: svc}
}

// Register ServeMux にエンドポイントを登録する。
//
//	POST /transfers           -> 振込依頼受付
//	GET  /transfers/{id}      -> 状態取得 ( 末尾 ID は path で受ける )
func (h *HTTPHandler) Register(mux *http.ServeMux) {
	mux.Handle("POST /transfers", http.HandlerFunc(h.create))
	mux.Handle("GET /transfers/{id}", http.HandlerFunc(h.get))
}

// requestTransferBody POST /transfers の入力 JSON 。
type requestTransferBody struct {
	AppRequestID    string `json:"appRequestId"`
	Amount          int64  `json:"amount"`
	SourceAccountID string `json:"sourceAccountId"`
	DestBankCode    string `json:"destBankCode"`
	DestBranchCode  string `json:"destBranchCode"`
	DestAccountType string `json:"destAccountType"`
	DestAccountNum  string `json:"destAccountNum"`
	DestAccountName string `json:"destAccountName"`
}

// transferResponse 振込状態のレスポンス JSON 。 ApplyNo / LastError は値が無ければ省略する。
type transferResponse struct {
	ID           string `json:"id"`
	AppRequestID string `json:"appRequestId"`
	Status       string `json:"status"`
	Amount       int64  `json:"amount"`
	ApplyNo      string `json:"applyNo,omitempty"`
	LastError    string `json:"lastError,omitempty"`
	Version      int    `json:"version"`
}

func toResponse(t *domain.Transfer) transferResponse {
	r := transferResponse{
		ID:           t.ID,
		AppRequestID: t.AppRequestID,
		Status:       string(t.Status),
		Amount:       t.Amount,
		Version:      t.Version,
	}
	if t.ApplyNo != nil {
		r.ApplyNo = *t.ApplyNo
	}
	if t.LastError != nil {
		r.LastError = *t.LastError
	}
	return r
}

// create POST /transfers ハンドラ。
func (h *HTTPHandler) create(w http.ResponseWriter, r *http.Request) {
	var body requestTransferBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	if body.AppRequestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "appRequestId required"})
		return
	}
	if body.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "amount must be positive"})
		return
	}

	t, err := h.svc.RequestTransfer(r.Context(), application.RequestTransferCommand(body))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, toResponse(t))
}

// get GET /transfers/{id} ハンドラ。
func (h *HTTPHandler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	t, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, toResponse(t))
}

// writeJSON status と body を JSON で返す共通ヘルパ。
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// ヘッダ既送出のためエラー応答は限定的。 ログ出力は呼び出し側ミドルウェアに任せる。
		_ = err
	}
}
