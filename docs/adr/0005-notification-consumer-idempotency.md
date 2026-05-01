# ADR-005 Notification モジュールは event_processed で受信側冪等性を担保する

## ステータス
採択

## コンテキスト
Outbox パターンの送信側 ( Relay ) は at-least-once を保証する。 受信側ハンドラが同じイベントを複数回処理する可能性があるため、 受信側で冪等性を担保しないと「同じ振込完了通知を 2 回送る」などの副作用が発生する。

## 検討した選択肢
- 案 A ハンドラ自身が業務的な冪等性を持つ ( idempotent な副作用 )
  - 利点 追加テーブル不要
  - 欠点 通知のような外部副作用ありの処理では完全な冪等化が困難 ( メールは 2 回送れば 2 通届く )
- 案 B event_processed テーブルで ( event_id, consumer ) 単位の処理済み記録
  - 利点 通知などの非冪等な副作用も 1 回限りに保証
  - 欠点 テーブル維持コスト

## 決定
案 B を採択する。 Notification を含む受信側ハンドラはハンドラ先頭で `outbox.MarkProcessed(ctx, db, eventID, consumer)` を呼び、 ErrAlreadyProcessed なら即 return する。

## 影響
- 全受信ハンドラはこのパターンに従う ( 将来的な Reconciliation などにも適用 )
- consumer 名はモジュール名を使う ( 例 "notification", "reconciliation" )
- ( event_id, consumer ) は event_processed の主キー
- Notification モジュールは TransferAcceptedToBank / TransferAwaitingApproval / TransferSettled / TransferFailed の 4 イベントを購読する

## 関連リンク
- plan.md ( 1.10 受信側冪等性 )
- ADR-002 共通 Outbox パターン
- ADR-004 状態機械に AWAITING_APPROVAL を含める
