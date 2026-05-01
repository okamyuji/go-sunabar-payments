# sunabar 実機接続テスト チェックリスト

実機接続は実取引が走る可能性があるため、 上から順に 1 ステップずつ確認しながら進める。 各 step の右側にチェック欄があるので、 完了したら `[x]` に変える。

## A. 事前準備

- [ ] A-1. sunabar 口座開設 ( https://gmo-aozora.com/sunabar/ )
- [ ] A-2. サービスサイトログイン -> お知らせ「アクセストークン」発行
- [ ] A-3. 取得したトークンを `.env` の `SUNABAR_ACCESS_TOKEN=` に設定
- [ ] A-4. `.env` を `.gitignore` 経由で track 外であることを `git status` で確認 ( 必須 )
- [ ] A-5. 振込先口座は自分名義の予備口座 ( テスト送金専用 ) を用意
- [ ] A-6. ローカル PC からインターネット経由で `curl -I https://api.sunabar.gmo-aozora.com` が応答すること
- [ ] A-7. ユニット / 統合テストがすべて緑 ( `make lint && make test && make test-integration` )

## B. ローカル環境起動

- [ ] B-1. `make compose-up` で MySQL Healthy
- [ ] B-2. `make migrate-up` で全マイグレーション適用
- [ ] B-3. 別タームで `SUNABAR_BASE_URL=https://api.sunabar.gmo-aozora.com SUNABAR_ACCESS_TOKEN=$SUNABAR_ACCESS_TOKEN make run-api`
- [ ] B-4. 別タームで `SUNABAR_BASE_URL=https://api.sunabar.gmo-aozora.com SUNABAR_ACCESS_TOKEN=$SUNABAR_ACCESS_TOKEN make run-relay`
- [ ] B-5. 別タームで `bash scripts/transfer-status.sh` を継続的に観察できる状態にしておく

## C. 読み取り系の疎通確認 ( 副作用なし )

- [ ] C-1. `curl http://localhost:8080/healthz` -> 200 ok
- [ ] C-2. `curl http://localhost:8080/metrics | jq .` -> JSON
- [ ] C-3. `curl -X POST http://localhost:8080/accounts/sync | jq .` -> 自分の口座が並ぶ
- [ ] C-4. `curl http://localhost:8080/accounts/<accountId>/balance | jq .` -> 残高表示

ここまで通らないなら以降に進まない。 トークン無効 / エンドポイント違い / NW の問題を切り分ける。

## D. バーチャル口座発行 ( 軽い書き込み、 振込は走らない )

- [ ] D-1. `curl -X POST http://localhost:8080/virtual-accounts -H 'Content-Type: application/json' -d '{"memo":"live-test","expiresOn":"2027-12-31"}' | jq .`
- [ ] D-2. レスポンスの virtualAccountId をメモ
- [ ] D-3. `curl http://localhost:8080/virtual-accounts | jq .` で発行済み一覧に出る

## E. 振込テスト ( 実取引 1 円 )

- [ ] E-1. `appRequestId="live-$(date +%s)"` を環境変数で固定
- [ ] E-2. POST /transfers ( amount=1, sourceAccountId=自分の口座, dest=自分の予備口座 )
- [ ] E-3. レスポンス 202 + status=PENDING + id を取得
- [ ] E-4. `bash scripts/transfer-status.sh` で 1 件 PENDING -> REQUESTED に進むのを確認
- [ ] E-5. sunabar サービスサイトのお知らせを開く
- [ ] E-6. 「取引内容承認」ページで取引パスワードを入力 ( このプロジェクトでは任意値で通る )
- [ ] E-7. しばらく待つ ( 数秒〜数十秒 ) と Relay が結果照会で SETTLED まで進める
- [ ] E-8. `curl http://localhost:8080/transfers/<id>` -> status=SETTLED + applyNo

## F. 二重実行防止の確認

- [ ] F-1. 同じ appRequestId で再度 POST /transfers
- [ ] F-2. レスポンス 202 + 同じ id が返る ( 二重 Transfer は作られない )

## G. クリーンアップ

- [ ] G-1. API / Relay を Ctrl+C で停止
- [ ] G-2. `make compose-down` ( DB ボリュームは保持 )
- [ ] G-3. `git status` で `.env` などの機微情報がコミット対象になっていないことを再確認

## 異常時の緊急停止

```bash
bash scripts/emergency-stop.sh
```

これで PENDING の Outbox イベントを一斉 FAILED に倒し、 Relay の追加送信を止める。 復旧時はトークン更新後に手動で個別 PENDING に戻す ( ADR-003 ) 。

## 観察ポイント

- E-4 の段階で REQUESTED に到達しない場合は API が 4xx を返している。 `last_error` を `transfers` テーブルから確認
- E-7 で SETTLED に進まない場合はメールトークン承認待ち or sunabar 側の処理時間。 `bash scripts/transfer-status.sh` で `awaiting approval rows` を見る
- 同時に `tail -f /tmp/relay.log` 相当のログをテーリングしておくと挙動が見える
