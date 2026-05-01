# Transfer の状態機械

`internal/modules/transfer/domain/status.go` の `validTransitions` をそのまま図にしたものです。 詳細は ADR-004 を参照してください。

```mermaid
stateDiagram-v2
    [*] --> PENDING : 振込依頼受付
    PENDING --> REQUESTED : sunabar.RequestTransfer 成功
    PENDING --> FAILED : sunabar.RequestTransfer 失敗
    REQUESTED --> AWAITING_APPROVAL : 承認待ちを検知
    REQUESTED --> APPROVED : 承認不要契約
    REQUESTED --> SETTLED : 即時着金
    REQUESTED --> FAILED : 銀行側拒否
    AWAITING_APPROVAL --> APPROVED : 取引パスワード承認完了
    AWAITING_APPROVAL --> FAILED : 承認タイムアウト
    APPROVED --> SETTLED : 着金確認
    APPROVED --> FAILED : 着金失敗
    SETTLED --> [*]
    FAILED --> [*]
```

## 各状態の意味

| Status | 意味 | 通知イベント |
| --- | --- | --- |
| PENDING | アプリで受付済み、 sunabar 未送信 | なし |
| REQUESTED | sunabar 受付済み、 結果未確定 | TransferAcceptedToBank |
| AWAITING_APPROVAL | メールトークン承認待ち | TransferAwaitingApproval |
| APPROVED | sunabar 側で承認済み、 着金処理中 | なし |
| SETTLED | 着金完了 ( 終端 ) | TransferSettled |
| FAILED | 失敗 ( 終端 ) | TransferFailed |
