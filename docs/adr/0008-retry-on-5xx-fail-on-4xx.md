# ADR-008 sunabar の 5xx と接続エラーはリトライ、 4xx は即 MarkFailed

## ステータス
採択

## コンテキスト
SendToSunabarHandler は sunabar の振込依頼 API を呼び出し、 失敗時の挙動を決める必要があります。 これまでは「失敗 = 即 MarkFailed」という単純な扱いでしたが、 障害ドリル ( M11 ) で次の問題が表面化しました。

- sunabar の一時的な 5xx ( メンテナンス窓、 アップストリーム障害 ) で、 リトライすれば成功するケースまで FAILED に倒される。 結果として手動復旧の負担が増える。
- 401 ( トークン失効 ) のような 4xx で、 リトライしても結果は変わらないのに Outbox の attempt_count を MaxAttempt まで消費し続ける。

## 検討した選択肢
- 案 A 失敗は全てリトライ可能とみなす ( ハンドラがエラーを返す )
  - 利点 一時障害から自動復旧する
  - 欠点 4xx でも MaxAttempt まで attempt が積み上がり、 復旧見込みのないログを出し続ける
- 案 B 失敗は全て即 MarkFailed
  - 利点 シンプル
  - 欠点 5xx でも復旧不可になり、 ハンドラが即終端化させる
- 案 C HTTP ステータスでリトライ可否を分岐
  - 利点 5xx は自動復旧、 4xx は即運用判断に倒せる
  - 欠点 ステータスごとの分類ルールを書く必要がある

## 決定
案 C を採択する。 `internal/modules/transfer/application/handler_send_to_sunabar.go` の Handle 関数で、 sunabar 呼び出しエラーを `isRetryable` で判定する。

- `*sunabar.APIError` かつ `StatusCode >= 500` -> リトライ ( ハンドラがエラーを返し、 Relay が next_attempt_at を未来に更新 )
- `*sunabar.APIError` かつ `StatusCode < 500` -> 即 MarkFailed ( transfer.status='FAILED' )
- それ以外のラップ済みエラー ( 接続失敗 / タイムアウト / DNS 失敗など ) -> リトライ ( 一時障害として扱う )

## 影響
- 5xx 障害下でも自動復旧する設計になる
- 4xx 即失敗により、 トークン失効や validation エラーは attempt_count=1 で確定する ( 過剰ログを抑制 )
- 障害ドリル ( internal/scenario ) でこのポリシーを継続的に検証する
- CheckStatusHandler は既に「sunabar API 失敗 = 通常エラー = リトライ」になっており本 ADR の対象外。 結果照会は何度叩いても副作用が小さいためこのままでよい。

## 関連リンク
- ADR-002 共通 Outbox パターン
- ADR-003 冪等キーの二重持ち
- internal/scenario/drill_integration_test.go ( Drill 2 / Drill 4 )
