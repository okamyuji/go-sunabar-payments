# ADR-003 冪等キーをアプリ層と API 層で二重持ちする

## ステータス
採択

## コンテキスト
sunabar / 本番 BaaS の振込依頼 API は冪等キー ( x-idempotency-key ) を必須とする。
一方でクライアント ( 自前 HTTP API の利用者 ) から見ても、 ネットワーク再送やリトライで同じ振込が二重に依頼されないことが望ましい。
冪等キーには TTL があり、 期限切れ時の自動再生成は重複振込のリスクを招く。

## 検討した選択肢
- 案 A クライアント生成の冪等キーをそのまま sunabar に渡す
  - 利点 シンプル
  - 欠点 クライアントの実装ミスで毎回違うキーになると重複振込のリスク
- 案 B サーバ側で生成した冪等キーのみ使う
  - 利点 サーバが冪等性を完全制御
  - 欠点 クライアント側で「同じ振込依頼」を識別する手段がない ( ネットワーク失敗時の再送で別 Transfer が作られる )
- 案 C 二重持ち ( app_request_id + api_idempotency_key )
  - 利点 クライアント側の重複検知 ( app_request_id ) と API 側の重複抑止 ( api_idempotency_key ) を両立
  - 欠点 実装の複雑度が上がる

## 決定
案 C を採択する。
- app_request_id クライアント生成。 transfers テーブルでユニーク制約。 HTTP API 受付時に重複検査。
- api_idempotency_key サーバが Transfer 作成時に 1 回だけ生成して永続化。 sunabar への送信時はこの値を再利用する。

## 影響
- transfers テーブルに 2 列のユニーク制約を持たせる ( uq_app_request_id, uq_api_idempotency_key )
- HTTP API 受付ハンドラは app_request_id の重複を 409 等で拒否する
- リレーワーカーが sunabar へ送信する際は api_idempotency_key を再利用する
- TTL 切れの api_idempotency_key は自動再生成しない。 検知時はエラーで止め、 運用判断で個別対応する
- sunabar Client interface の RequestTransfer は IdempotencyKey フィールドを必須としてシグネチャに含める

## 関連リンク
- plan.md ( 1.8 冪等性キーの二重持ち )
- ADR-002 共通 Outbox パターンを採用する
