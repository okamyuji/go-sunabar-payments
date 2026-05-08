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
- 3 状態 CircuitBreaker ( CLOSED / OPEN / HALF_OPEN ) + rolling window 失敗率による外部ゲートウェイ保護。 OPEN 由来の拒否は `outbox.ErrSkipAttempt` でラップし Outbox の attempt_count を消費しない

## 技術スタック
- Go 1.25 以上 ( できる限り標準ライブラリだけで構成 )
- MySQL 8.0 ( Aurora MySQL 8.0 互換を意識 )
- net/http ( ルーターも標準のみ )
- database/sql + go-sql-driver/mysql ( DB ドライバのみサードパーティ )
- log/slog ( 構造化ログ )
- testcontainers-go ( 統合テストでの実 DB 起動 )
- google/uuid ( UUID v7 / v4 )
- Docker ( マルチステージビルドで distroless ベースの本番イメージを生成 )
- Terraform ( ECR / ECS Fargate / IAM / CloudWatch Logs を IaC 管理 )
- gitleaks ( pre-commit と GitHub Actions でシークレットスキャンを二段防衛 )

## セットアップ

### 必要なもの
- Go 1.25 以上
- Docker / Docker Compose
- golang-migrate ( `brew install golang-migrate` )
- golangci-lint v2 系 ( `brew install golangci-lint` )
- staticcheck ( `go install honnef.co/go/tools/cmd/staticcheck@latest` )
- gitleaks ( `brew install gitleaks` ) — pre-commit と CI で必須
- ( 任意 ) Terraform 1.7 以上 — `terraform/envs/dev` で AWS 検証環境を立てる場合

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

`make install-hooks` 後の pre-commit は CI と同じ Go 1.25 toolchain で lint / build / test / integration test を実行します。 初回は Go toolchain の取得で時間がかかる場合があります。

## 開発コマンド
| コマンド | 内容 |
| --- | --- |
| `make fmt` | go fmt ./... |
| `make fmt-check` | CI と同じ gofmt 差分チェック |
| `make vet` | go vet ./... |
| `make staticcheck` | staticcheck 単独実行 |
| `make lint` | fmt / vet / staticcheck / golangci-lint をまとめて実行 |
| `make test` | -short -race -shuffle=on -count=1 のユニットテスト |
| `make test-integration` | Docker 必須の統合テスト ( -p 1 で逐次 ) |
| `make build` | 3 バイナリ ( api / relay / reconciler ) をビルド |
| `make run-api` / `make run-relay` / `make run-reconciler` | 各プロセスの起動 |
| `make migrate-up` / `make migrate-down` | golang-migrate の操作 |
| `make compose-up` / `make compose-down` | ローカル MySQL 起動 / 停止 |
| `make install-hooks` | core.hooksPath を .githooks に向け、 commit 前に CI と同じチェックを実行する |
| `make smoke` | ローカル / 実機相手のスモークテスト ( scripts/sunabar-smoke.sh ) |
| `make status` | 振込 / Outbox の状態スナップショット ( 運用補助 ) |
| `make emergency-stop` | PENDING を一斉 FAILED に倒す緊急停止 ( 確認プロンプト付き ) |
| `make seed` | scripts/seed.sql を投入 ( 開発用ダミーデータ ) |

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
| POST | /virtual-accounts | バーチャル口座発行 ( 法人 API のため `SUNABAR_ACCESS_TOKEN_CORPORATE` が必要 ) |
| GET | /virtual-accounts | 発行済みバーチャル口座一覧 |

### sunabar 認可トークン
sunabar の API は個人系 ( `/personal/v1/*` ) と法人系 ( `/corporation/v1/*` ) で別トークンを要求します。 `cmd/api` も両方を環境変数で受け取ります。

| 環境変数 | 用途 | 必須 |
| --- | --- | --- |
| `SUNABAR_BASE_URL` | sunabar の Base URL | yes |
| `SUNABAR_ACCESS_TOKEN` | 個人 API ( 残高 / 振込 / 入出金明細 ) のアクセストークン | yes |
| `SUNABAR_ACCESS_TOKEN_CORPORATE` | 法人 API ( バーチャル口座発行など ) のアクセストークン | VA を使う場合のみ |

## ディレクトリ構造
```
go-sunabar-payments/
├── cmd/
│   ├── api/             # HTTP API サーバ
│   ├── relay/           # Outbox リレーワーカー
│   ├── reconciler/      # 消込ワーカー
│   └── sunabar-probe/   # sunabar 実 API への到達性検証 CLI ( Fargate / ローカル両対応 )
├── internal/
│   ├── modules/
│   │   ├── account/          # 口座モジュール ( 残高 / バーチャル口座 )
│   │   ├── transfer/         # 振込モジュール ( 状態機械 / 冪等キー )
│   │   ├── reconciliation/   # 消込モジュール ( 請求 / 入金突合 )
│   │   └── notification/     # 通知モジュール ( Outbox 受信 + stdout 送信 )
│   ├── platform/
│   │   ├── outbox/         # 共通 Outbox ( Publisher / Relay / MarkProcessed )
│   │   ├── transaction/    # Unit of Work
│   │   ├── sunabar/        # sunabar API クライアント / AuthSource ( Static / OAuth2 ) — 個人 / 法人を分離
│   │   ├── database/       # MySQL DSN / プール
│   │   ├── idempotency/    # UUID 生成
│   │   └── observability/  # 相関 ID / ロガー / メトリクス / マスク / HTTP middleware
│   └── scenario/      # 複数モジュールを束ねる薄いオーケストレーション層 ( 将来用 )
├── migrations/        # SQL マイグレーション
├── docs/
│   ├── adr/           # Architecture Decision Record ( 0001-0008 )
│   ├── diagrams/      # Mermaid アーキテクチャ図
│   ├── runbook/       # 実機接続手順書
│   └── article/       # Zenn 記事ドラフトの控え
├── terraform/
│   ├── envs/dev/      # AWS dev 環境 ( ECR / ECS Fargate Task / IAM / Logs )
│   └── modules/       # ecr / ecs-fargate-task / iam / logs モジュール
├── Dockerfile         # マルチステージビルド ( distroless ベースの本番イメージ )
├── .dockerignore
├── .gitleaks.toml     # sunabar 固有ルール + allowlist
├── .github/workflows/ # GitHub Actions ( lint + test + integration + gitleaks 日次スキャン )
└── .githooks/         # pre-commit ( gitleaks / fmt / vet / staticcheck / lint / build / test )
```

## 設計判断 ( ADR )
- [ADR-001 モジュラーモノリスを採用する](docs/adr/0001-adopt-modular-monolith.md)
- [ADR-002 共通 Outbox パターンを採用する](docs/adr/0002-shared-outbox.md)
- [ADR-003 冪等キーをアプリ層と API 層で二重持ちする](docs/adr/0003-double-idempotency-key.md)
- [ADR-004 状態機械に AWAITING_APPROVAL を含める](docs/adr/0004-include-awaiting-approval-state.md)
- [ADR-005 Notification モジュールは event_processed で受信側冪等性を担保する](docs/adr/0005-notification-consumer-idempotency.md)
- [ADR-006 Relay の分離レベルを READ COMMITTED に固定する](docs/adr/0006-relay-read-committed.md)
- [ADR-007 時刻は UTC で統一する](docs/adr/0007-store-times-as-utc.md)
- [ADR-008 sunabar の 5xx と接続エラーはリトライ、 4xx は即 MarkFailed](docs/adr/0008-retry-on-5xx-fail-on-4xx.md)

## アーキテクチャ図 / シーケンス図
- [モジュール境界とデータフロー](docs/diagrams/architecture.md)
- [Transfer の状態機械](docs/diagrams/transfer-state-machine.md)
- [消込シーケンス](docs/diagrams/reconciliation-sequence.md)

## 実機接続
sunabar / 本番 BaaS への接続手順、 OAuth2 切替方法、 緊急停止手順は [docs/runbook/sunabar-live-connection.md](docs/runbook/sunabar-live-connection.md) を参照してください。 実取引が動く可能性があるため、 必ず人間レビューを挟みます。

## 実機到達性検証 ( sunabar-probe )
`cmd/sunabar-probe` は sunabar 実 API への到達性をワンショットで検証する CLI です。 ローカルからも、 ECS Fargate のタスクとしても同じバイナリで動かせます。 認証情報はソースに埋め込まず、 すべて環境変数 ( `SUNABAR_BASE_URL` / `SUNABAR_ACCESS_TOKEN` / `SUNABAR_ACCESS_TOKEN_CORPORATE` 等 ) から読みます。

```bash
SUNABAR_BASE_URL=https://api.sunabar.gmo-aozora.com \
SUNABAR_ACCESS_TOKEN=xxxxx \
go run ./cmd/sunabar-probe
```

read 系 ( GetAccounts / GetBalance / ListTransactions ) を必ず実行し、 `SUNABAR_PROBE_WRITE=transfer` を設定すると 1 円振込で書き込み系の疎通も検証します ( 残高不足の 400 でも「 通信できた」 証拠としては十分 ) 。 出力は構造化ログ ( slog JSON ) で stdout に流れるため、 CloudWatch Logs から到達ログを追跡できます。

## コンテナ / IaC ( AWS デプロイ )
- `Dockerfile` はマルチステージビルドで `cmd/api` を distroless イメージに同梱します。 ローカルからは `docker build -t go-sunabar-payments .` で生成。
- `terraform/envs/dev` は ECR リポジトリ、 ECS Fargate タスク定義、 IAM ロール、 CloudWatch Logs グループを管理します。 state やプロビジョニング後に生成される `.terraform/`, `*.tfstate*`, `.terraform.lock.hcl` は `terraform/envs/dev/.gitignore` で追跡対象外です。
- 検証後に AWS 側のリソースを残さない運用を想定しているため、 利用後は `terraform destroy` を実行してください。

## シークレットスキャン
- pre-commit ( `.githooks/pre-commit` ) と `.pre-commit-config.yaml` の双方で `gitleaks git --staged --config .gitleaks.toml` を実行します。
- GitHub Actions ( `.github/workflows/ci.yml` ) は PR ごとの差分スキャンに加え、 毎日 03:00 UTC ( JST 12:00 ) に履歴全体を再スキャンします ( 多層防衛 ) 。
- `.gitleaks.toml` は sunabar 固有ルールと allowlist を内包しています。 必要に応じて allowlist を更新してください。

## API クライアントコレクション
- [api/requests.http](api/requests.http) VS Code REST Client や JetBrains の HTTP Client 用
- [api/postman_collection.json](api/postman_collection.json) Postman / Insomnia 用 ( v2.1.0 )

## 障害ドリル
M11 で追加した「現場でハマる障害」を再現する統合テストです。
- 同一 appRequestId の連打 ( 二重 Transfer 抑止 )
- sunabar 5xx 一時障害 ( バックオフ後の自動復旧 )
- 緊急停止 ( PENDING -> FAILED 一斉切替後の dispatch 抑制と手動復旧 )
- アクセストークン失効 ( 4xx 即 MarkFailed )

`make test-integration` でドリルも含めて全件通ります。

## ライセンス
MIT
