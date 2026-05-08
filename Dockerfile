# syntax=docker/dockerfile:1.7

# ===========================================================================
# go-sunabar-payments 用 マルチステージ Dockerfile
# - Linux/amd64 ( ECS Fargate / App Runner ) で動く静的バイナリを生成
# - cmd/api / cmd/relay / cmd/reconciler / cmd/sunabar-probe を 1 イメージに同梱
# - ENTRYPOINT は ENV BIN で切替 ( 例: BIN=api / BIN=sunabar-probe )
# ===========================================================================

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src

# 依存解決
COPY go.mod go.sum ./
RUN go mod download

# ソース全体
COPY . .

# 静的バイナリ ( CGO 無効 ) を bin/ に出力
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/api            ./cmd/api && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/relay          ./cmd/relay && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/reconciler     ./cmd/reconciler && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/sunabar-probe  ./cmd/sunabar-probe

# 実行ステージ ( distroless: 最小、 CA 証明書同梱 )
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=builder /out/api            /app/api
COPY --from=builder /out/relay          /app/relay
COPY --from=builder /out/reconciler     /app/reconciler
COPY --from=builder /out/sunabar-probe  /app/sunabar-probe

# 既定は API サーバ。 ECS タスク定義側で command を上書きして他のバイナリも実行できる。
USER nonroot:nonroot
EXPOSE 8080
ENV API_ADDR=:8080
ENTRYPOINT ["/app/api"]
