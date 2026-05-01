# go-sunabar-payments

Go と sunabar ( GMO あおぞらネット銀行の API サンドボックス ) で作る個人開発の送金システム。
モジュラーモノリス × Outbox で本番 BaaS 移行に耐える設計を目指す実装サンプル。

> 関連記事
> 「Go と sunabar で作る個人開発の送金システム — モジュラーモノリス × Outbox で本番移行に耐える設計」
> Zenn 第 3 章ドラフト ( published: false ) を `/Users/yujiokamoto/devs/zenn/articles/go-sunabar-outbox-chapter-03-draft.md` に配置しています。 リポジトリ側の控えは `docs/article/draft-chapter-03.md` です。

## 何を作っているか
- sunabar API を叩く最小実装 ( 残高照会、 振込依頼、 入出金明細、 バーチャル口座 )
- 共通 Outbox パターンでビジネスデータ更新と外部 API 呼び出しの原子性を担保
- 4 モジュール構成 ( Account / Transfer / Reconciliation / Notification )
- 冪等性キーをアプリ層と API 層で二重持ち
- 振込の状態機械 ( PENDING -> REQUESTED -> AWAITING_APPROVAL -> APPROVED -> SETTLED / FAILED )
- 消込ワーカーで請求と入金の突合 ( OPEN / PARTIAL / CLEARED / EXCESS )
- 観測性 ( 相関 ID 伝搬、 構造化ログ、 メトリクス、 機微情報マスク )

## 技術スタック
- Go 1.25 以上 ( できる限り標準ライブラリだけで構成 )
- MySQL 8.0 ( Aurora MySQL 8.0 互換を意識 )
- net/http ( ルーターも標準のみ )
- database/sql + go-sql-driver/mysql ( DB ドライバのみサードパーティ )
- log/slog ( 構造化ログ )
- testcontainers-go ( 統合テストでの実 DB 起動 )
- google/uuid ( UUID v7 / v4 )

## セットアップ

### 必要なもの
- Go 1.25 以上
- Docker / Docker Compose
- golang-migrate ( `brew install golang-migrate` )
- golangci-lint v2 系 ( `brew install golangci-lint` )
- staticcheck ( `go install honnef.co/go/tools/cmd/staticcheck@latest` )

### 手順
```bash
git clone <リポジトリURL>
cd go-sunabar-payments
cp .env.example .env

# 既存の 3306 と衝突する場合は MYSQL_HOST_PORT と DB_PORT を 3307 などに変更
make compose-up
make migrate-up
make install-hooks
make lint
make test
make test-integration
```

## 開発コマンド
| コマンド | 内容 |
| --- | --- |
| `make fmt` | go fmt ./... |
| `make vet` | go vet ./... |
| `make staticcheck` | staticcheck 単独実行 |
| `make lint` | fmt / vet / staticcheck / golangci-lint をまとめて実行 |
| `make test` | -short -race -shuffle=on -count=1 のユニットテスト |
| `make test-integration` | Docker 必須の統合テスト ( -p 1 で逐次 ) |
| `make build` | 3 バイナリ ( api / relay / reconciler ) をビルド |
| `make run-api` / `make run-relay` / `make run-reconciler` | 各プロセスの起動 |
| `make migrate-up` / `make migrate-down` | golang-migrate の操作 |
| `make compose-up` / `make compose-down` | ローカル MySQL 起動 / 停止 |
| `make install-hooks` | core.hooksPath を .githooks に向ける |

## HTTP API エンドポイント

| メソッド | パス | 内容 |
| --- | --- | --- |
| GET | /healthz | DB 疎通付きヘルスチェック |
| GET | /metrics | Outbox / Transfer 状態の JSON メトリクス |
| POST | /transfers | 振込依頼 ( 冪等キー必須 ) |
| GET | /transfers/{id} | 振込状態の取得 |
| POST | /accounts/sync | sunabar から口座情報を同期 |
| GET | /accounts | アプリ DB の口座一覧 |
| GET | /accounts/{id}/balance | 残高取得 |
| POST | /virtual-accounts | バーチャル口座発行 |
| GET | /virtual-accounts | 発行済みバーチャル口座一覧 |

## ディレクトリ構造
```
go-sunabar-payments/
├── cmd/
│   ├── api/          # HTTP API サーバ
│   ├── relay/        # Outbox リレーワーカー
│   └── reconciler/   # 消込ワーカー
├── internal/
│   ├── modules/
│   │   ├── account/          # 口座モジュール ( 残高 / バーチャル口座 )
│   │   ├── transfer/         # 振込モジュール ( 状態機械 / 冪等キー )
│   │   ├── reconciliation/   # 消込モジュール ( 請求 / 入金突合 )
│   │   └── notification/     # 通知モジュール ( Outbox 受信 + stdout 送信 )
│   ├── platform/
│   │   ├── outbox/         # 共通 Outbox ( Publisher / Relay / MarkProcessed )
│   │   ├── transaction/    # Unit of Work
│   │   ├── sunabar/        # sunabar API クライアント / AuthSource ( Static / OAuth2 )
│   │   ├── database/       # MySQL DSN / プール
│   │   ├── idempotency/    # UUID 生成
│   │   └── observability/  # 相関 ID / ロガー / メトリクス / マスク / HTTP middleware
│   └── scenario/      # 複数モジュールを束ねる薄いオーケストレーション層 ( 将来用 )
├── migrations/        # SQL マイグレーション
├── docs/
│   ├── adr/           # Architecture Decision Record ( 0001-0007 )
│   ├── diagrams/      # Mermaid アーキテクチャ図
│   ├── runbook/       # 実機接続手順書
│   └── article/       # Zenn 記事ドラフトの控え
├── .github/workflows/ # GitHub Actions ( lint + test + integration )
└── .githooks/         # pre-commit ( fmt / vet / staticcheck / lint / build / test )
```

## 設計判断 ( ADR )
- [ADR-001 モジュラーモノリスを採用する](docs/adr/0001-adopt-modular-monolith.md)
- [ADR-002 共通 Outbox パターンを採用する](docs/adr/0002-shared-outbox.md)
- [ADR-003 冪等キーをアプリ層と API 層で二重持ちする](docs/adr/0003-double-idempotency-key.md)
- [ADR-004 状態機械に AWAITING_APPROVAL を含める](docs/adr/0004-include-awaiting-approval-state.md)
- [ADR-005 Notification モジュールは event_processed で受信側冪等性を担保する](docs/adr/0005-notification-consumer-idempotency.md)
- [ADR-006 Relay の分離レベルを READ COMMITTED に固定する](docs/adr/0006-relay-read-committed.md)
- [ADR-007 時刻は UTC で統一する](docs/adr/0007-store-times-as-utc.md)

## アーキテクチャ図 / シーケンス図
- [モジュール境界とデータフロー](docs/diagrams/architecture.md)
- [Transfer の状態機械](docs/diagrams/transfer-state-machine.md)
- [消込シーケンス](docs/diagrams/reconciliation-sequence.md)

## 実機接続
sunabar / 本番 BaaS への接続手順、 OAuth2 切替方法、 緊急停止手順は [docs/runbook/sunabar-live-connection.md](docs/runbook/sunabar-live-connection.md) を参照してください。 実取引が動く可能性があるため、 必ず人間レビューを挟みます。

## ライセンス
MIT
