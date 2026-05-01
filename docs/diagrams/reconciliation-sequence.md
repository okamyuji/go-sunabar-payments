# 消込シーケンス

Reconciliation モジュールは バーチャル口座への入金を sunabar の入出金明細 API から取得し、 invoices テーブルと突合して status を進めます。 cmd/reconciler が定期的に呼び出します。

```mermaid
sequenceDiagram
    participant Cron as cmd/reconciler ( ticker )
    participant Acc as account module
    participant Rec as reconciliation module
    participant Sunabar as sunabar API
    participant DB as MySQL
    participant Outbox

    Cron->>Acc: ListVirtualAccounts
    Acc-->>Cron: [VA-1, VA-2, ...]
    loop 各バーチャル口座
        Cron->>Rec: Reconcile(VA-id)
        Rec->>Sunabar: ListTransactions(VA-id, from=now-24h, to=now)
        Sunabar-->>Rec: [credit 10000, ...]
        Rec->>DB: BEGIN
        Rec->>DB: SELECT invoices WHERE va_id=?
        DB-->>Rec: invoice ( amount=10000, paid=0, OPEN )
        Rec->>DB: INSERT IGNORE incoming_transactions ( item_key UNIQUE )
        Note right of Rec: 新規行のみ paid_amount を加算
        Rec->>Rec: invoice.Apply(+10000) -> CLEARED
        Rec->>DB: UPDATE invoices SET status=CLEARED, paid_amount=10000
        Rec->>Outbox: Publish ReconciliationCompleted
        Rec->>DB: COMMIT
        Rec-->>Cron: ReconcileResult { NewIncomings:1, status:CLEARED }
    end
```

## マッチングロジック

| 入金合計と請求額の関係 | status |
| --- | --- |
| 入金 = 0 | OPEN ( 既定 ) |
| 0 < 入金 < 請求 | PARTIAL |
| 入金 = 請求 | CLEARED |
| 入金 > 請求 | EXCESS ( 過入金、 手動確認 ) |

## 重複防止

- `incoming_transactions` テーブルに `UNIQUE KEY (virtual_account_id, item_key)` を張り、 同じ入金行を 2 回登録しない
- `INSERT IGNORE` で重複時は黙ってスキップする
- 重複だった場合は invoice への加算もスキップする ( 二重消込防止 )
