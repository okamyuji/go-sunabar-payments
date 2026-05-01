# ADR-006 Relay の分離レベルを READ COMMITTED に固定する

## ステータス
採択

## コンテキスト
Outbox リレーは `SELECT ... FOR UPDATE SKIP LOCKED` で PENDING 行を取り出してロックし、 同じトランザクション内でハンドラを呼び、 終了時に status を更新します。 ハンドラは状態遷移に応じて新たな Outbox イベントを INSERT することがあります。

InnoDB のデフォルト分離レベルは REPEATABLE READ で、 範囲指定の `SELECT FOR UPDATE` は gap lock ( 範囲ロック ) を取ります。 ハンドラ内で `status='PENDING'` の範囲に新規行を INSERT すると、 自分自身が取った範囲ロックに阻まれて lock wait timeout になります。 アプリログには「ハンドラが遅い」しか出ず、 障害特定が難しい問題でした。

## 検討した選択肢
- 案 A REPEATABLE READ のまま、 ハンドラの中で Outbox INSERT を行わない設計に倒す
  - 利点 分離レベルを変えない
  - 欠点 状態変化を別経路で書き戻す必要があり、 設計が複雑になる
- 案 B Relay の SELECT を 2 段階に分ける ( 取得 -> commit -> ハンドラ呼び出し -> 別 tx で status 更新 )
  - 利点 ロック保持時間が短い
  - 欠点 取得と更新の間に他ワーカーが横取り防止する仕組みが必要 ( 状態列を追加する )
- 案 C Relay のトランザクションだけ READ COMMITTED に切り替える
  - 利点 1 行の変更で gap lock を回避できる
  - 欠点 Phantom Read の可能性が生じるが、 Outbox 用途では問題にならない ( SKIP LOCKED で重複処理は防げる )

## 決定
案 C を採択する。 `r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})` で Relay のトランザクションだけ READ COMMITTED にする。 Phantom Read は SKIP LOCKED と組み合わせれば実害がない。

## 影響
- `internal/platform/outbox/relay.go` の `ProcessBatch` で IsolationLevel を指定
- ハンドラの内部で同じテーブルへの新規 Outbox INSERT を許容する設計が成立する
- 他 SELECT 系 ( メトリクス収集など ) はデフォルト分離レベルのままで影響なし

## 関連リンク
- ADR-002 共通 Outbox パターン
- 記事第 3 章 リレーの停滞でハンドラが数分単位で詰まった話
