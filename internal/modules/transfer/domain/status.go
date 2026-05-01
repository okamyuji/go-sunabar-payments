// Package domain Transfer 集約のドメインロジックを提供する。
// エンティティ、 値オブジェクト、 状態機械、 ドメインイベント、 ドメインエラーを含む。
package domain

// Status 振込の状態を表す。
type Status string

const (
	// StatusPending アプリで受付済み、 まだ sunabar に未送信の状態。
	StatusPending Status = "PENDING"
	// StatusRequested sunabar に振込依頼 API 送信済み、 メールトークン承認待ちまたは結果照会前。
	StatusRequested Status = "REQUESTED"
	// StatusAwaitingApproval sunabar 側で取引内容承認待ち ( 人間操作 ) 。
	StatusAwaitingApproval Status = "AWAITING_APPROVAL"
	// StatusApproved sunabar 側で承認済み、 銀行への送金処理進行中。
	StatusApproved Status = "APPROVED"
	// StatusSettled 終端 振込完了。
	StatusSettled Status = "SETTLED"
	// StatusFailed 終端 失敗 ( 拒否、 限度額超過、 タイムアウトなど ) 。
	StatusFailed Status = "FAILED"
)

// validTransitions 状態遷移グラフ。 キーから値の状態へのみ遷移を許可する。
// REQUESTED から複数経路 ( AWAITING_APPROVAL / APPROVED / SETTLED ) を許容することで、
// 本番 BaaS でメール承認が不要な契約形態でも破綻しない設計にしている ( ADR-004 参照 ) 。
var validTransitions = map[Status][]Status{
	StatusPending:          {StatusRequested, StatusFailed},
	StatusRequested:        {StatusAwaitingApproval, StatusApproved, StatusSettled, StatusFailed},
	StatusAwaitingApproval: {StatusApproved, StatusSettled, StatusFailed},
	StatusApproved:         {StatusSettled, StatusFailed},
	StatusSettled:          {},
	StatusFailed:           {},
}

// IsTerminal 終端状態かどうかを返す。
func (s Status) IsTerminal() bool {
	return s == StatusSettled || s == StatusFailed
}

// canTransition from -> to が validTransitions に含まれるかを判定する。
// 同じ状態への遷移 ( 冪等な再観測 ) も true を返す。
func canTransition(from, to Status) bool {
	if from == to {
		return true
	}
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
