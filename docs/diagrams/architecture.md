# アーキテクチャ図

go-sunabar-payments は 4 モジュールのモジュラーモノリスです。 各モジュールは port.go で定義した Service interface だけを公開し、 内部実装には他モジュールから直接依存しません。 横断的関心事 ( Outbox / トランザクション / sunabar クライアント / 観測性 ) は `internal/platform/*` に集約しています。

## モジュール境界

```mermaid
flowchart TB
    subgraph cmd
        api[cmd/api - HTTP API]
        relay[cmd/relay - Outbox Relay]
        recon[cmd/reconciler - Reconcile Worker]
    end

    subgraph modules
        Account
        Transfer
        Reconciliation
        Notification
    end

    subgraph platform
        Outbox[platform/outbox - Publisher / Relay]
        Tx[platform/transaction - Unit of Work]
        Sunabar[platform/sunabar - Client / AuthSource]
        Obs[platform/observability - Logger / Metrics / Correlation]
        DB[platform/database - MySQL DSN / pool]
    end

    api -->|DI| Transfer
    api -->|DI| Account
    relay -->|Register handlers| Transfer
    relay -->|Register handlers| Notification
    recon -->|Periodic| Reconciliation

    Transfer --> Tx
    Transfer --> Outbox
    Transfer --> Sunabar
    Account --> Tx
    Account --> Sunabar
    Reconciliation --> Tx
    Reconciliation --> Outbox
    Reconciliation --> Sunabar
    Notification --> Outbox

    api --> Obs
    relay --> Obs
    recon --> Obs
    api --> DB
    relay --> DB
    recon --> DB
```

## データフロー ( 振込依頼 -> 承認 -> 確定 )

```mermaid
sequenceDiagram
    participant Client
    participant API as cmd/api
    participant Tx as transfer module
    participant DB as MySQL
    participant Relay as cmd/relay
    participant Sunabar as sunabar API
    participant Notif as notification module

    Client->>API: POST /transfers
    API->>Tx: RequestTransfer
    Tx->>DB: INSERT transfers + Outbox(TransferRequested)
    Tx-->>API: PENDING
    API-->>Client: 202 Accepted

    Relay->>DB: SELECT FOR UPDATE SKIP LOCKED
    DB-->>Relay: TransferRequested
    Relay->>Tx: SendToSunabarHandler
    Tx->>Sunabar: RequestTransfer (with idempotency-key)
    Sunabar-->>Tx: applyNo
    Tx->>DB: UPDATE transfers (REQUESTED) + Outbox(AcceptedToBank, StatusCheckScheduled)

    Relay->>DB: 別ループで TransferStatusCheckScheduled
    Relay->>Tx: CheckStatusHandler
    Tx->>Sunabar: GetTransferStatus
    Sunabar-->>Tx: AwaitingApproval
    Tx->>DB: UPDATE transfers (AWAITING_APPROVAL) + Outbox(AwaitingApproval)
    Tx-->>Relay: ErrStillInFlight (再投入)

    Note over Relay,Sunabar: メールトークン承認後、 結果照会で Settled
    Relay->>Tx: CheckStatusHandler
    Tx->>Sunabar: GetTransferStatus
    Sunabar-->>Tx: Settled
    Tx->>DB: UPDATE transfers (SETTLED) + Outbox(Settled)

    Relay->>Notif: TransferAcceptedToBank / Awaiting / Settled
    Notif->>DB: MarkProcessed (event_processed)
    Notif->>Notif: Send notice
```
