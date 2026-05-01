#!/usr/bin/env bash
# scripts/transfer-status.sh
# 進行中の振込状態を一覧表示する運用補助スクリプト。
# 実機接続時に「メールトークン承認待ちが何件あるか」「FAILED が増えていないか」を確認する用途。

set -euo pipefail

CONTAINER="${CONTAINER:-gsp-mysql}"
DB_USER="${DB_USER:-app}"
DB_PASS="${DB_PASS:-app}"
DB_NAME="${DB_NAME:-payments}"

echo "=== transfers status counts ==="
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -N -e \
  "SELECT status, COUNT(*) FROM transfers GROUP BY status ORDER BY status" 2>/dev/null

echo
echo "=== outbox status counts ==="
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -N -e \
  "SELECT status, COUNT(*) FROM outbox_events GROUP BY status ORDER BY status" 2>/dev/null

echo
echo "=== awaiting approval rows ==="
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -e \
  "SELECT id, app_request_id, apply_no, updated_at FROM transfers WHERE status='AWAITING_APPROVAL' ORDER BY updated_at" 2>/dev/null

echo
echo "=== failed transfers ( latest 10 ) ==="
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -e \
  "SELECT id, status, last_error, updated_at FROM transfers WHERE status='FAILED' ORDER BY updated_at DESC LIMIT 10" 2>/dev/null

echo
echo "=== failed outbox events ( latest 10 ) ==="
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -e \
  "SELECT id, event_type, attempt_count, last_error FROM outbox_events WHERE status='FAILED' ORDER BY id DESC LIMIT 10" 2>/dev/null
