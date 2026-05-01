// Package domain Account 集約のドメイン定義を提供する。
package domain

import (
	"errors"
	"time"
)

// Account 口座メタ情報。 sunabar 側の口座をアプリ DB にキャッシュした表現。
type Account struct {
	ID              string
	AccountID       string
	BranchCode      string
	AccountNumber   string
	AccountTypeCode string
	AccountName     string
	PrimaryFlag     bool
	Label           *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NewAccount 新規 Account を生成する。
func NewAccount(id, accountID, branchCode, accountNumber, accountTypeCode, accountName string, primary bool, now time.Time) *Account {
	return &Account{
		ID:              id,
		AccountID:       accountID,
		BranchCode:      branchCode,
		AccountNumber:   accountNumber,
		AccountTypeCode: accountTypeCode,
		AccountName:     accountName,
		PrimaryFlag:     primary,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// Balance 残高情報を表す値オブジェクト。 円単位。
type Balance struct {
	AccountID    string
	CurrencyCode string
	Amount       int64
	BaseDate     string
	BaseTime     string
}

// VirtualAccount バーチャル口座情報。
type VirtualAccount struct {
	ID               string
	VirtualAccountID string
	BranchCode       string
	AccountNumber    string
	Memo             *string
	ExpiresOn        *time.Time
	InvoiceID        *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// NewVirtualAccount バーチャル口座メタ情報を生成する。
func NewVirtualAccount(id, vaID, branchCode, accountNumber string, memo *string, expiresOn *time.Time, now time.Time) *VirtualAccount {
	return &VirtualAccount{
		ID:               id,
		VirtualAccountID: vaID,
		BranchCode:       branchCode,
		AccountNumber:    accountNumber,
		Memo:             memo,
		ExpiresOn:        expiresOn,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// ドメインエラー。
var (
	// ErrNotFound 指定 ID の Account / VirtualAccount が見つからない。
	ErrNotFound = errors.New("account not found")
	// ErrAlreadyExists 重複登録を試みた。
	ErrAlreadyExists = errors.New("account already exists")
)
