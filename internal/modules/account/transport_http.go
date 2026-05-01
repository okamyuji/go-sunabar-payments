package account

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"go-sunabar-payments/internal/modules/account/application"
	"go-sunabar-payments/internal/modules/account/domain"
)

// accountResponse Account の HTTP レスポンス。
type accountResponse struct {
	ID            string `json:"id"`
	AccountID     string `json:"accountId"`
	BranchCode    string `json:"branchCode"`
	AccountNumber string `json:"accountNumber"`
	AccountName   string `json:"accountName"`
	PrimaryFlag   bool   `json:"primaryFlag"`
}

func toAccountResponse(a *domain.Account) accountResponse {
	return accountResponse{
		ID:            a.ID,
		AccountID:     a.AccountID,
		BranchCode:    a.BranchCode,
		AccountNumber: a.AccountNumber,
		AccountName:   a.AccountName,
		PrimaryFlag:   a.PrimaryFlag,
	}
}

// virtualAccountResponse VirtualAccount の HTTP レスポンス。
type virtualAccountResponse struct {
	ID               string `json:"id"`
	VirtualAccountID string `json:"virtualAccountId"`
	BranchCode       string `json:"branchCode"`
	AccountNumber    string `json:"accountNumber"`
	Memo             string `json:"memo,omitempty"`
	ExpiresOn        string `json:"expiresOn,omitempty"`
}

func toVAResponse(va *domain.VirtualAccount) virtualAccountResponse {
	r := virtualAccountResponse{
		ID:               va.ID,
		VirtualAccountID: va.VirtualAccountID,
		BranchCode:       va.BranchCode,
		AccountNumber:    va.AccountNumber,
	}
	if va.Memo != nil {
		r.Memo = *va.Memo
	}
	if va.ExpiresOn != nil {
		r.ExpiresOn = va.ExpiresOn.Format("2006-01-02")
	}
	return r
}

// sync POST /accounts/sync
func (h *HTTPHandler) sync(w http.ResponseWriter, r *http.Request) {
	accs, err := h.svc.SyncAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]accountResponse, 0, len(accs))
	for _, a := range accs {
		out = append(out, toAccountResponse(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// list GET /accounts
func (h *HTTPHandler) list(w http.ResponseWriter, r *http.Request) {
	accs, err := h.svc.ListAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]accountResponse, 0, len(accs))
	for _, a := range accs {
		out = append(out, toAccountResponse(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// balance GET /accounts/{id}/balance
func (h *HTTPHandler) balance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id required"})
		return
	}
	b, err := h.svc.GetBalance(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accountId":    b.AccountID,
		"currencyCode": b.CurrencyCode,
		"amount":       b.Amount,
		"baseDate":     b.BaseDate,
		"baseTime":     b.BaseTime,
	})
}

// issueVA POST /virtual-accounts
func (h *HTTPHandler) issueVA(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Memo      string `json:"memo"`
		ExpiresOn string `json:"expiresOn,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json: " + err.Error()})
		return
	}
	cmd := application.IssueVirtualAccountCommand{Memo: body.Memo}
	if body.ExpiresOn != "" {
		t, err := time.Parse("2006-01-02", body.ExpiresOn)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expiresOn: must be YYYY-MM-DD"})
			return
		}
		cmd.ExpiresOn = &t
	}
	va, err := h.svc.IssueVirtualAccount(r.Context(), cmd)
	if err != nil {
		if errors.Is(err, domain.ErrAlreadyExists) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, toVAResponse(va))
}

// listVA GET /virtual-accounts
func (h *HTTPHandler) listVA(w http.ResponseWriter, r *http.Request) {
	vas, err := h.svc.ListVirtualAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := make([]virtualAccountResponse, 0, len(vas))
	for _, va := range vas {
		out = append(out, toVAResponse(va))
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
