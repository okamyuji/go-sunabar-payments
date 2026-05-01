# sunabar 実機接続手順 ( 人間レビュー必須 )

このドキュメントは sunabar / GMO あおぞらネット銀行の実機 API に対して接続するための運用手順書です。 アプリ実装は `internal/platform/sunabar` の `Client` インターフェースを通じて呼ばれるので、 認可方式の切替と環境変数の設定だけで実機 / モックを行き来できます。 ただし実機接続は実取引が動く可能性があるため、 必ず人間が手順を確認しながら実行してください。

## 1. 接続方式の選択

| 環境 | 認可方式 | アクセストークン取得元 |
| --- | --- | --- |
| sunabar サンドボックス | StaticTokenSource | サービスサイトのお知らせ画面で発行されたトークンを手動コピー |
| 本番 BaaS ( OAuth2.0 クライアントクレデンシャル ) | OAuth2TokenSource | 事業者契約で払い出される ClientID / ClientSecret から動的に取得 |
| 本番 BaaS ( 認可コードフロー ) | 未実装 ( 今後 ) | ユーザの同意フローを経由する。 個人開発スコープ外 |

## 2. sunabar サンドボックス接続

### 2.1 必要な環境変数

```env
SUNABAR_BASE_URL=https://api.sunabar.gmo-aozora.com
SUNABAR_ACCESS_TOKEN=<サービスサイトのお知らせから取得した値>
```

### 2.2 動作確認

```bash
make compose-up
make migrate-up
make run-relay   # 別タームで起動
make run-api     # 別タームで起動

# 残高照会で疎通確認
curl -s http://localhost:8080/accounts/sync | jq .
curl -s http://localhost:8080/accounts/<accountId>/balance | jq .
```

### 2.3 振込依頼 -> 承認 -> 結果照会

```bash
# 1. 振込依頼を投げる
curl -sX POST http://localhost:8080/transfers -d '{
  "appRequestId": "live-test-001",
  "amount": 1,
  "sourceAccountId": "<accountId>",
  "destBankCode": "0033",
  "destBranchCode": "001",
  "destAccountType": "1",
  "destAccountNum": "1234567",
  "destAccountName": "ヤマダ タロウ"
}' | jq .

# 2. レスポンスの id をメモして、 sunabar サービスサイトの「お知らせ」を開き、 承認 URL を踏んで取引パスワードを入力
# 3. 結果照会は Relay の自動再投入で行われる。 数分後に状態を確認:
curl -s http://localhost:8080/transfers/<id> | jq .
```

### 2.4 トークン失効時の対応

- sunabar サービスサイトでアクセストークンが無効化された場合、 401 が返ります。
- `.env` の `SUNABAR_ACCESS_TOKEN` を新しい値に差し替えて Relay と API を再起動してください。
- 失効中の Outbox 行は status='PENDING' のまま attempt_count を消費するので、 復旧後にバックオフ完了で自動的に再送されます。

## 3. 本番 BaaS への切替

### 3.1 OAuth2 クライアントクレデンシャル

`internal/platform/sunabar/auth.go` の `OAuth2TokenSource` を使います。 配線は cmd 側で `NewStaticTokenSource` を `NewOAuth2TokenSource` に差し替えるだけです。

```go
auth, err := sunabar.NewOAuth2TokenSource(sunabar.OAuth2Config{
    TokenURL:     os.Getenv("SUNABAR_OAUTH_TOKEN_URL"),
    ClientID:     os.Getenv("SUNABAR_OAUTH_CLIENT_ID"),
    ClientSecret: os.Getenv("SUNABAR_OAUTH_CLIENT_SECRET"),
    Scope:        os.Getenv("SUNABAR_OAUTH_SCOPE"),
})
```

### 3.2 必要な環境変数

```env
SUNABAR_BASE_URL=https://api.gmo-aozora.com
SUNABAR_OAUTH_TOKEN_URL=https://api.gmo-aozora.com/oauth/token
SUNABAR_OAUTH_CLIENT_ID=<事業者契約で払い出される値>
SUNABAR_OAUTH_CLIENT_SECRET=<事業者契約で払い出される値>
SUNABAR_OAUTH_SCOPE=transfer balance
```

### 3.3 status 文字列の精緻化

`internal/modules/transfer/application/handler_check_status.go` の `mapSunabarStatus` で扱う sunabar 側 status 文字列 ( "AcceptedToBank" / "AwaitingApproval" / "Approved" / "Settled" / "Failed" ) は仮値です。 実機接続検証時に正確な文字列を確認し、 必要なら追記してください。

## 4. 実機接続前のチェックリスト

- [ ] `.env` に正しいエンドポイントとトークンが入っている
- [ ] `make migrate-up` で全テーブルが作成されている
- [ ] `make test-integration` がモック相手にすべて緑 ( 30 件以上 )
- [ ] 振込先口座番号は仮想口座 ( 自分名義の予備口座 ) を使う
- [ ] 振込金額は最小単位 ( 1 円 ) から始める
- [ ] `make run-relay` のログを別タームでテーリングし、 status 遷移を目で追う
- [ ] 失敗時のロールバック手順 ( DB の transfers テーブルを SELECT して状態を確認 ) を把握している

## 5. ロールバック / 緊急停止

```bash
# 1. API と Relay を停止 ( SIGTERM )
pkill -TERM -f "bin/api"
pkill -TERM -f "bin/relay"

# 2. PENDING の Outbox イベントを FAILED に変更して送出を止める
docker exec gsp-mysql mysql -uapp -papp payments -e \
  "UPDATE outbox_events SET status='FAILED' WHERE status='PENDING'"

# 3. transfers の状態を確認
docker exec gsp-mysql mysql -uapp -papp payments -e \
  "SELECT id, status, apply_no, last_error FROM transfers"
```

緊急停止時に二重実行を防ぐため、 status='PENDING' を一斉に FAILED に倒す手段を用意しています。 復旧時は手動判断で個別に PENDING に戻してください ( 自動再生成はしません。 ADR-003 ) 。
