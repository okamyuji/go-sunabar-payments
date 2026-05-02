.PHONY: help fmt fmt-check vet lint staticcheck test test-unit test-integration build \
        run-api run-relay run-reconciler \
        migrate-up migrate-down compose-up compose-down install-hooks ci-check \
        smoke status emergency-stop seed

GO          := go
GOFLAGS     := -mod=readonly
export GOTOOLCHAIN ?= go1.25.0
DB_HOST     ?= 127.0.0.1
DB_PORT     ?= 3306
DB_USER     ?= app
DB_PASS     ?= app
DB_NAME     ?= payments
# golang-migrate URI 形式 (mysql:// 形式) で書く。?multiStatements=true 必須でない場合も付与しておく。
MIGRATE_DSN ?= mysql://$(DB_USER):$(DB_PASS)@tcp($(DB_HOST):$(DB_PORT))/$(DB_NAME)?parseTime=true&loc=Asia%2FTokyo&multiStatements=true

help: ## ヘルプ表示
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

fmt: ## go fmt 全実行
	$(GO) fmt ./...

fmt-check: ## CI と同じ gofmt 差分チェック
	@test -z "$$(gofmt -l .)" || (echo "gofmt diff:"; gofmt -l .; exit 1)

vet: ## go vet 全実行
	$(GO) vet ./...

staticcheck: ## staticcheck 単独実行
	staticcheck ./...

lint: fmt vet staticcheck ## fmt と vet と staticcheck と golangci-lint を直列実行
	golangci-lint run ./...

test: test-unit ## デフォルトはユニットテスト

test-unit: ## ユニットテスト実行 ( -race -shuffle=on -count=1 )
	$(GO) test $(GOFLAGS) -short -race -shuffle=on -count=1 ./...

test-integration: ## CI と同じ統合テスト実行 ( Docker / MySQL 必須 )
	$(GO) test $(GOFLAGS) -race -shuffle=on -count=1 -tags=integration -timeout=10m ./...

build: ## 全バイナリをビルド
	$(GO) build -o bin/api ./cmd/api
	$(GO) build -o bin/relay ./cmd/relay
	$(GO) build -o bin/reconciler ./cmd/reconciler

run-api: ## API サーバを起動
	$(GO) run ./cmd/api

run-relay: ## Outbox リレーワーカーを起動
	$(GO) run ./cmd/relay

run-reconciler: ## 消込ワーカーを起動
	$(GO) run ./cmd/reconciler

migrate-up: ## マイグレーション適用
	migrate -path ./migrations -database "$(MIGRATE_DSN)" up

migrate-down: ## マイグレーション 1 ステップ戻し
	migrate -path ./migrations -database "$(MIGRATE_DSN)" down 1

compose-up: ## Docker Compose でローカル依存を起動 ( ヘルスチェック完了まで待機 )
	docker compose up -d --wait

compose-down: ## Docker Compose を停止 ( ボリューム保持 )
	docker compose down

compose-down-clean: ## Docker Compose を停止しボリュームも削除
	docker compose down -v

install-hooks: ## .githooks を git のフックパスに登録
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-commit

ci-check: fmt-check vet staticcheck ## CI と同等のフルチェック
	golangci-lint run ./...
	$(GO) build ./...
	$(GO) test $(GOFLAGS) -short -race -shuffle=on -count=1 ./...
	$(GO) test $(GOFLAGS) -race -shuffle=on -count=1 -tags=integration -timeout=10m ./...

smoke: ## モック / 実機相手のスモークテスト ( API_BASE で切替 )
	bash scripts/sunabar-smoke.sh

status: ## 振込 / Outbox の状態を一覧表示 ( 運用補助 )
	bash scripts/transfer-status.sh

emergency-stop: ## 全 PENDING を FAILED に倒して送信を止める ( 確認プロンプト付き )
	bash scripts/emergency-stop.sh

seed: ## ローカル開発用の seed データを投入する
	docker exec -i $(shell docker ps -qf "name=gsp-mysql") mysql -u$(DB_USER) -p$(DB_PASS) $(DB_NAME) < scripts/seed.sql
