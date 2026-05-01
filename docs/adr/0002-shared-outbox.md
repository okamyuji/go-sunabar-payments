# ADR-002 共通 Outbox パターンを採用する

## ステータス
採択

## コンテキスト
ビジネスデータの更新と外部 API 呼び出し ( sunabar API への振込依頼など ) を別々のトランザクションで行うと、片方が成功し片方が失敗した場合にデータ不整合が生じる ( Dual Write 問題 ) 。
DB へ書いたが API 呼び出しに失敗した場合のリトライ、 API は成功したが DB 書き込みに失敗した場合の復旧をいずれもアプリ側のロジックで担保するのは現実的でない。

## 検討した選択肢
- 案 A 2 フェーズコミット
  - 利点 強整合性
  - 欠点 銀行 API 側に 2PC 互換のリソースマネージャがない
- 案 B モジュールごとに独自 Outbox
  - 利点 モジュール完結
  - 欠点 横断集計や複数モジュール跨ぎイベントが複雑化、 リレーワーカーがテーブルごとに必要
- 案 C 共通 Outbox ( aggregate_type + aggregate_id で発生元識別 )
  - 利点 横断集計可能、 リレーワーカーが 1 つで済む
  - 欠点 ロック競合に注意 ( SELECT ... FOR UPDATE SKIP LOCKED で対処可 )

## 決定
案 C を採択。 internal/platform/outbox に集約し、 各モジュールは Publisher interface を介して書き込む。
配信先は in-process Handler ディスパッチで開始し、 将来的に Kafka / SQS など差し替え可能な構造にする。
受信側冪等性は event_processed テーブルで管理し、 ( event_id, consumer ) を主キーとする。
バックオフは指数バックオフ ( 上限 10 分 ) 、 MaxAttempt 超過で status='FAILED' に遷移させる。

## 影響
- outbox_events テーブルが共通テーブルになる
- Publisher は transaction.Tx を介して同一トランザクションで INSERT する
- 受信側冪等性は event_processed テーブルで管理する
- リレーワーカーは cmd/relay として独立プロセス
- 並行ワーカーは SELECT ... FOR UPDATE SKIP LOCKED で重複処理を回避する

## 関連リンク
- plan.md ( プロジェクト全体計画 1.6 共通 Outbox 設計 )
- ADR-003 冪等キーをアプリ層と API 層で二重持ちする
