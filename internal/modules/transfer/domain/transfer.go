package domain

import "time"

// Transfer 振込集約のルートエンティティ。
// アプリ層では Repository / UseCase 越しに操作し、 状態遷移は MarkRequested / MarkFailed / Transition で行う。
type Transfer struct {
	ID                string
	AppRequestID      string // クライアント生成の冪等キー
	APIIdempotencyKey string // sunabar API に渡す冪等キー
	Status            Status
	Amount            int64
	SourceAccountID   string
	DestBankCode      string
	DestBranchCode    string
	DestAccountType   string
	DestAccountNum    string
	DestAccountName   string
	ApplyNo           *string // sunabar 側の applyNo 。 REQUESTED 以降にセット
	LastError         *string // 直近の失敗理由 ( 概要 ) 。 詳細はログと Outbox
	Version           int     // 楽観ロック
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// TransferParams 新規 Transfer 作成時のビジネスパラメータ。
type TransferParams struct {
	Amount          int64
	SourceAccountID string
	DestBankCode    string
	DestBranchCode  string
	DestAccountType string
	DestAccountNum  string
	DestAccountName string
}

// NewTransfer 新規 Transfer 集約を作る。 status は PENDING 、 version は 1 で初期化する。
func NewTransfer(id, appRequestID, apiIdempotencyKey string, params TransferParams, now time.Time) *Transfer {
	return &Transfer{
		ID:                id,
		AppRequestID:      appRequestID,
		APIIdempotencyKey: apiIdempotencyKey,
		Status:            StatusPending,
		Amount:            params.Amount,
		SourceAccountID:   params.SourceAccountID,
		DestBankCode:      params.DestBankCode,
		DestBranchCode:    params.DestBranchCode,
		DestAccountType:   params.DestAccountType,
		DestAccountNum:    params.DestAccountNum,
		DestAccountName:   params.DestAccountName,
		Version:           1,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// Transition 状態遷移を実行する。 許可されない遷移は ErrInvalidTransition を返す。
// 同じ状態への遷移は許容 ( 冪等な再観測 ) 。
func (t *Transfer) Transition(to Status, now time.Time) error {
	if !canTransition(t.Status, to) {
		return ErrInvalidTransition
	}
	t.Status = to
	t.UpdatedAt = now
	return nil
}

// MarkRequested status=REQUESTED に遷移し、 applyNo を保存する。
func (t *Transfer) MarkRequested(applyNo string, now time.Time) error {
	if err := t.Transition(StatusRequested, now); err != nil {
		return err
	}
	t.ApplyNo = &applyNo
	return nil
}

// MarkFailed status=FAILED に遷移し、 エラー理由を保存する。
func (t *Transfer) MarkFailed(reason string, now time.Time) error {
	if err := t.Transition(StatusFailed, now); err != nil {
		return err
	}
	t.LastError = &reason
	return nil
}
