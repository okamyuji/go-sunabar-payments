# go-sunabar-payments

Go と sunabar ( GMO あおぞらネット銀行の API サンドボックス ) で作る個人開発の送金システム。
モジュラーモノリス × Outbox で本番 BaaS 移行に耐える設計を目指す実装サンプル。

> 関連記事
> 「Go と sunabar で作る個人開発の送金システム — モジュラーモノリス × Outbox で本番移行に耐える設計」
> Zenn で公開予定 ( /Users/yujiokamoto/devs/zenn/articles に原稿を配置 ) 。

## 何を作っているか
- sunabar API を叩く最小実装 ( 残高照会、振込依頼、入出金明細、バーチャル口座 )
- 共通 Outbox パターンでビジネスデータ更新と外部 API 呼び出しの原子性を担保
- 4 モジュール構成 ( Account / Transfer / Reconciliation / Notification )
- 冪等性キーをアプリ層と API 層で二重持ち
- 振込の状態機械 ( PENDING -> REQUESTED -> AWAITING_APPROVAL -> APPROVED -> SETTLED / FAILED )

## 技術スタック
- Go 1.25 以上 ( できる限り標準ライブラリだけで構成 )
- MySQL 8.0 ( Aurora MySQL 8.0 互換を意識 )
- net/http ( ルーターも標準のみ )
- database/sql + go-sql-driver/mysql ( DB ドライバのみサードパーティ )
- log/slog ( 構造化ログ )
- testcontainers-go ( 統合テストでの実 DB 起動 )

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
```

## 開発コマンド
| コマンド | 内容 |
| --- | --- |
| `make fmt` | go fmt ./... |
| `make vet` | go vet ./... |
| `make staticcheck` | staticcheck 単独実行 |
| `make lint` | fmt / vet / staticcheck / golangci-lint をまとめて実行 |
| `make test` | -short -race -shuffle=on -count=1 のユニットテスト |
| `make test-integration` | Docker 必須の統合テスト |
| `make build` | 3 バイナリ ( api / relay / reconciler ) をビルド |
| `make run-api` / `make run-relay` / `make run-reconciler` | 各プロセスの起動 |
| `make migrate-up` / `make migrate-down` | golang-migrate の操作 |
| `make compose-up` / `make compose-down` | ローカル MySQL 起動 / 停止 |
| `make install-hooks` | core.hooksPath を .githooks に向ける |

## ディレクトリ構造
```
go-sunabar-payments/
├── cmd/
│   ├── api/          # HTTP API サーバ
│   ├── relay/        # Outbox リレーワーカー
│   └── reconciler/   # 消込ワーカー
├── internal/
│   ├── modules/
│   │   ├── account/          # 口座モジュール
│   │   ├── transfer/         # 振込モジュール
│   │   ├── reconciliation/   # 消込モジュール
│   │   └── notification/     # 通知モジュール
│   ├── platform/
│   │   ├── outbox/         # 共通 Outbox
│   │   ├── transaction/    # Unit of Work
│   │   ├── sunabar/        # sunabar API クライアント
│   │   ├── idempotency/    # 冪等性キー周り
│   │   ├── clock/          # 時刻抽象
│   │   └── observability/  # ロガー / メトリクス
│   └── scenario/      # 複数モジュールを束ねる薄いオーケストレーション層
├── migrations/        # SQL マイグレーション
├── docs/
│   ├── adr/           # Architecture Decision Record
│   └── diagrams/      # Mermaid 図
├── .github/workflows/ # GitHub Actions
└── .githooks/         # pre-commit
```

## 設計判断 ( ADR )
- [ADR-001 モジュラーモノリスを採用する](docs/adr/0001-adopt-modular-monolith.md)
- [ADR-002 共通 Outbox パターンを採用する](docs/adr/0002-shared-outbox.md)
- [ADR-003 冪等キーをアプリ層と API 層で二重持ちする](docs/adr/0003-double-idempotency-key.md)

## ライセンス
MIT
