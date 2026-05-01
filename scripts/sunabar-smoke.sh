#!/usr/bin/env bash
# scripts/sunabar-smoke.sh
# 実機接続前のスモークテスト。 ローカル DB と API サーバが立ち上がっている前提で、
# モック sunabar 相手の主要 API を順番に叩いて応答を確認する。
# 実機接続テストの前段としても使える ( SUNABAR_BASE_URL を実機エンドポイントに差し替える ) 。
#
# 使い方:
#   make compose-up
#   make migrate-up
#   make run-api       # 別タームで起動済みであること
#   make run-relay     # 別タームで起動済みであること
#   bash scripts/sunabar-smoke.sh
#
# 任意の環境変数:
#   API_BASE   ( 既定 http://localhost:8080 )
#   APP_REQ_ID ( 既定 smoke-<unix-timestamp> )

set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
APP_REQ_ID="${APP_REQ_ID:-smoke-$(date +%s)}"

echo "[smoke] API_BASE=${API_BASE}"
echo "[smoke] APP_REQ_ID=${APP_REQ_ID}"

# 1. ヘルスチェック
echo
echo "[1/6] GET /healthz"
curl -fsS "${API_BASE}/healthz"
echo

# 2. メトリクス ( Outbox の depth など )
echo
echo "[2/6] GET /metrics"
curl -fsS "${API_BASE}/metrics" | jq .

# 3. 口座同期 ( sunabar から取得して DB に保存 )
echo
echo "[3/6] POST /accounts/sync"
ACCOUNTS=$(curl -fsS -X POST "${API_BASE}/accounts/sync")
echo "${ACCOUNTS}" | jq .
ACCOUNT_ID=$(echo "${ACCOUNTS}" | jq -r '.[0].accountId')
if [[ -z "${ACCOUNT_ID}" || "${ACCOUNT_ID}" == "null" ]]; then
  echo "[smoke] FAILED: accountId が取れない"
  exit 1
fi
echo "[smoke] ACCOUNT_ID=${ACCOUNT_ID}"

# 4. 残高照会
echo
echo "[4/6] GET /accounts/${ACCOUNT_ID}/balance"
curl -fsS "${API_BASE}/accounts/${ACCOUNT_ID}/balance" | jq .

# 5. バーチャル口座発行
echo
echo "[5/6] POST /virtual-accounts"
curl -fsS -X POST "${API_BASE}/virtual-accounts" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg memo "smoke-${APP_REQ_ID}" '{memo: $memo}')" | jq .

# 6. 振込依頼 ( 1 円のみ。 実機接続時は実取引が動くため要注意 )
echo
echo "[6/6] POST /transfers ( amount=1 )"
TRANSFER=$(curl -fsS -X POST "${API_BASE}/transfers" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
    --arg appReqId "${APP_REQ_ID}" \
    --arg src "${ACCOUNT_ID}" \
    '{
      appRequestId: $appReqId,
      amount: 1,
      sourceAccountId: $src,
      destBankCode: "0033",
      destBranchCode: "001",
      destAccountType: "1",
      destAccountNum: "1234567",
      destAccountName: "ヤマダ タロウ"
     }')")
echo "${TRANSFER}" | jq .
TRANSFER_ID=$(echo "${TRANSFER}" | jq -r '.id')
if [[ -z "${TRANSFER_ID}" || "${TRANSFER_ID}" == "null" ]]; then
  echo "[smoke] FAILED: 振込 id が取れない"
  exit 1
fi
echo "[smoke] TRANSFER_ID=${TRANSFER_ID}"

# 7. Relay の処理を待ってから状態確認
echo
echo "[follow] GET /transfers/${TRANSFER_ID} ( Relay の処理を待機 )"
for i in 1 2 3 4 5 6 7 8 9 10; do
  sleep 2
  RES=$(curl -fsS "${API_BASE}/transfers/${TRANSFER_ID}")
  STATUS=$(echo "${RES}" | jq -r '.status')
  echo "[follow] try=${i} status=${STATUS}"
  if [[ "${STATUS}" == "SETTLED" || "${STATUS}" == "FAILED" ]]; then
    echo "${RES}" | jq .
    echo "[smoke] DONE"
    exit 0
  fi
done

echo "[smoke] WARN: 終端に到達しないままタイムアウト ( メールトークン承認待ちの可能性 ) "
curl -fsS "${API_BASE}/transfers/${TRANSFER_ID}" | jq .
exit 0
