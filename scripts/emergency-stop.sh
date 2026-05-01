#!/usr/bin/env bash
# scripts/emergency-stop.sh
# 実機接続時の緊急停止スクリプト。 PENDING の Outbox イベントを FAILED に倒し、
# Relay が新規送信を進めないようにする。 ADR-003 の方針に従い、 自動再開はしない。
# 復旧時は status を手動で確認してから個別に PENDING に戻す。

set -euo pipefail

CONTAINER="${CONTAINER:-gsp-mysql}"
DB_USER="${DB_USER:-app}"
DB_PASS="${DB_PASS:-app}"
DB_NAME="${DB_NAME:-payments}"

read -r -p "本当に PENDING を全件 FAILED に倒しますか? ( yes / no ): " confirm
if [[ "${confirm}" != "yes" ]]; then
  echo "中止しました。"
  exit 1
fi

echo "[stop] 1) API / Relay プロセスへ SIGTERM"
pkill -TERM -f "bin/api" 2>/dev/null || true
pkill -TERM -f "bin/relay" 2>/dev/null || true
pkill -TERM -f "bin/reconciler" 2>/dev/null || true

echo "[stop] 2) PENDING を FAILED に変更"
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -e \
  "UPDATE outbox_events SET status='FAILED', last_error='emergency-stop'
   WHERE status='PENDING'" 2>/dev/null

echo "[stop] 3) 結果"
docker exec "${CONTAINER}" mysql -u"${DB_USER}" -p"${DB_PASS}" "${DB_NAME}" -N -e \
  "SELECT status, COUNT(*) FROM outbox_events GROUP BY status" 2>/dev/null

echo
echo "復旧時は次のコマンドで状況を確認してください。"
echo "  bash scripts/transfer-status.sh"
echo
echo "個別 PENDING 戻し ( 慎重に ) :"
echo "  docker exec ${CONTAINER} mysql -u${DB_USER} -p${DB_PASS} ${DB_NAME} -e \\"
echo "    \"UPDATE outbox_events SET status='PENDING', last_error=NULL WHERE id='<event_id>'\""
