# sunabar 実機接続テスト 再開手順 ( 口座開設後 )

口座開設待ちで作業を中断した状態から再開するための手順書です。 開設完了後、 上から順に実施してください。 詳細は [sunabar-live-checklist.md](sunabar-live-checklist.md) と [sunabar-live-connection.md](sunabar-live-connection.md) を併用してください。

## 1. 環境確認

```bash
# プロジェクトルートに移動
cd "$(git rev-parse --show-toplevel)"

# 最新の状態を pull
git status
git pull --ff-only origin main

# テストすべて緑であることを再確認
make compose-up
make migrate-up
make lint
make test
make test-integration
```

statinte ぜんぶ緑なら次へ。

## 2. アクセストークンの取得

1. https://gmo-aozora.com/sunabar/ にログイン
2. 左メニューの「お知らせ」を開く
3. 「アクセストークン発行」を実行
4. 表示された値を `.env` の `SUNABAR_ACCESS_TOKEN=` に貼る ( クォートしない )

`.env` は `.gitignore` で track されないことを確認:

```bash
git check-ignore .env || echo "WARNING: .env が track されている可能性"
```

## 3. 副作用ゼロの疎通確認

別タームで:

```bash
# Term 1
source .env
SUNABAR_BASE_URL=https://api.sunabar.gmo-aozora.com \
SUNABAR_ACCESS_TOKEN=$SUNABAR_ACCESS_TOKEN \
DB_HOST=127.0.0.1 DB_PORT=3306 DB_USER=app DB_PASSWORD=app DB_NAME=payments \
make run-api

# Term 2
SUNABAR_BASE_URL=https://api.sunabar.gmo-aozora.com \
SUNABAR_ACCESS_TOKEN=$SUNABAR_ACCESS_TOKEN \
DB_HOST=127.0.0.1 DB_PORT=3306 DB_USER=app DB_PASSWORD=app DB_NAME=payments \
OUTBOX_POLL_INTERVAL=2s \
make run-relay

# Term 3 ( 確認 )
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/metrics | jq .
curl -sX POST http://localhost:8080/accounts/sync | jq .
# 出力された accountId をコピー
ACCOUNT_ID=<取得した accountId>
curl -s http://localhost:8080/accounts/${ACCOUNT_ID}/balance | jq .
```

ここまで通らないなら以降は進めない。 401 ならトークン無効、 接続エラーなら NW を疑う。

## 4. バーチャル口座発行 ( 軽い書き込み )

```bash
curl -sX POST http://localhost:8080/virtual-accounts \
  -H 'Content-Type: application/json' \
  -d '{"memo":"live-test-1","expiresOn":"2027-12-31"}' | jq .

# 一覧確認
curl -s http://localhost:8080/virtual-accounts | jq .
```

## 5. 振込テスト ( 1 円、 自分の予備口座宛 )

事前に予備口座の値を `.env.live` などに書いておく:

```bash
DEST_BANK_CODE=0033
DEST_BRANCH_CODE=001
DEST_ACCOUNT_TYPE=1
DEST_ACCOUNT_NUM=1234567   # ★ 自分の予備口座番号に置換
DEST_ACCOUNT_NAME="ヤマダ タロウ"  # ★ 自分の名前 ( 半角カナ )
```

実行:

```bash
source .env.live
APP_REQ_ID="live-$(date +%s)"
echo "APP_REQ_ID=${APP_REQ_ID}"

curl -sX POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d "$(jq -n \
    --arg appReqId "${APP_REQ_ID}" \
    --arg src "${ACCOUNT_ID}" \
    --arg bank "${DEST_BANK_CODE}" \
    --arg branch "${DEST_BRANCH_CODE}" \
    --arg accType "${DEST_ACCOUNT_TYPE}" \
    --arg accNum "${DEST_ACCOUNT_NUM}" \
    --arg accName "${DEST_ACCOUNT_NAME}" \
    '{
       appRequestId: $appReqId, amount: 1, sourceAccountId: $src,
       destBankCode: $bank, destBranchCode: $branch, destAccountType: $accType,
       destAccountNum: $accNum, destAccountName: $accName
     }')" | jq .

# 返ってきた id を保存
TRANSFER_ID=<上の id>
```

## 6. 状態を追う

```bash
# 数秒置きに観察
watch -n 2 "curl -s http://localhost:8080/transfers/${TRANSFER_ID} | jq ."

# DB 直接確認も可能
bash scripts/transfer-status.sh
```

PENDING -> REQUESTED の遷移を確認したら、 sunabar サービスサイトの「お知らせ」から「取引内容承認」ページを開き、 取引パスワード ( sunabar では任意値 ) を入力して承認。

承認後、 数秒〜数十秒で REQUESTED -> APPROVED -> SETTLED と進めば成功。

## 7. 二重実行抑止の確認

```bash
# 同じ APP_REQ_ID で再 POST -> 同じ id が返る ( 二重 Transfer は作られない )
curl -sX POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg appReqId "${APP_REQ_ID}" --arg src "${ACCOUNT_ID}" \
    --arg bank "${DEST_BANK_CODE}" --arg branch "${DEST_BRANCH_CODE}" \
    --arg accType "${DEST_ACCOUNT_TYPE}" --arg accNum "${DEST_ACCOUNT_NUM}" \
    --arg accName "${DEST_ACCOUNT_NAME}" \
    '{appRequestId:$appReqId, amount:1, sourceAccountId:$src,
      destBankCode:$bank, destBranchCode:$branch, destAccountType:$accType,
      destAccountNum:$accNum, destAccountName:$accName}')" | jq -r '.id'

# DB 確認
docker exec gsp-mysql mysql -uapp -papp payments -N -e \
  "SELECT COUNT(*) FROM transfers WHERE app_request_id='${APP_REQ_ID}'"
# -> 1 を返すこと
```

## 8. 終了

```bash
# Term 1 / Term 2 で Ctrl+C で停止
# 機微情報の漏れ確認
git status
# .env / .env.live が untracked のまま、 もしくは .gitignore で無視されていることを確認
```

## 9. 異常時の緊急停止

```bash
bash scripts/emergency-stop.sh
# yes と入力で確定
```

復旧時はトークン更新後に手動で個別 PENDING に戻す ( ADR-003 の方針 ) 。

## 10. 完了後にやること ( 任意 )

- [ ] 結果を `docs/runbook/live-test-result-YYYY-MM-DD.md` に追記 ( 観察した状態遷移時間 / mock との差分 / sunabar 側の正確な status 文字列 )
- [ ] `mapSunabarStatus` の値を実機で観測した文字列に更新 ( `internal/modules/transfer/application/handler_check_status.go` )
- [ ] 実機テストで気付いた挙動差を ADR か Zenn 記事第4章にフィードバック

## ファイル参照

- [sunabar-live-checklist.md](sunabar-live-checklist.md) チェックボックス形式の実行ログ
- [sunabar-live-connection.md](sunabar-live-connection.md) 詳細な背景と OAuth2 切替手順
- `scripts/sunabar-smoke.sh` 自動スモークスクリプト ( ローカル mock でも実機でも `API_BASE` だけ変えれば動く )
- `scripts/transfer-status.sh` 状態スナップショット
- `scripts/emergency-stop.sh` 緊急停止
