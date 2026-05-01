.PHONY: help fmt vet lint staticcheck test test-unit test-integration build \
        run-api run-relay run-reconciler \
        migrate-up migrate-down compose-up compose-down install-hooks ci-check

GO          := go
GOFLAGS     := -mod=readonly
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

vet: ## go vet 全実行
	$(GO) vet ./...

staticcheck: ## staticcheck 単独実行
	staticcheck ./...

lint: fmt vet staticcheck ## fmt と vet と staticcheck と golangci-lint を直列実行
	golangci-lint run ./...

test: test-unit ## デフォルトはユニットテスト

test-unit: ## ユニットテスト実行 ( -race -shuffle=on -count=1 )
	$(GO) test $(GOFLAGS) -short -race -shuffle=on -count=1 ./...

test-integration: ## 統合テスト実行 ( Docker 必須、 共有 DB のため -p 1 で逐次 )
	$(GO) test $(GOFLAGS) -race -shuffle=on -count=1 -p 1 -tags=integration -timeout=10m ./...

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

ci-check: fmt vet staticcheck ## CI と同等のチェック ( lint + build + test )
	golangci-lint run ./...
	$(GO) build ./...
	$(GO) test $(GOFLAGS) -short -race -shuffle=on -count=1 ./...
