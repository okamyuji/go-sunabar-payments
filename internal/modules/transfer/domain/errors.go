package domain

import "errors"

// ドメインエラー。 アプリ層・インフラ層はこれらをラップして返す。
var (
	// ErrInvalidTransition 許可されない状態遷移を試みた。
	ErrInvalidTransition = errors.New("transfer invalid status transition")
	// ErrAlreadyExists app_request_id が既に存在する。
	ErrAlreadyExists = errors.New("transfer already exists with the same app request id")
	// ErrNotFound 指定 ID の Transfer が見つからない。
	ErrNotFound = errors.New("transfer not found")
	// ErrConcurrentUpdate 楽観ロック衝突。
	ErrConcurrentUpdate = errors.New("transfer concurrent update detected")
)
