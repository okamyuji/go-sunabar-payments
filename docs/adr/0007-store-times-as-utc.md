# ADR-007 時刻は UTC で統一する

## ステータス
採択

## コンテキスト
MySQL のセッションタイムゾーンと Go ドライバの `loc` パラメータの組み合わせで、 DATETIME 列の値の解釈が崩れる事故が多発します。 Aurora MySQL や docker mysql:8.0 は本プロジェクトでは `--default-time-zone=+09:00` で動かしますが、 アプリ側は UTC で書きたいため、 解釈が一致しないと「INSERT 直後に SELECT で取れない」「9 時間ずれて未来扱いになる」などの障害になります。

## 検討した選択肢
- 案 A セッションタイムゾーンをアプリ起動時に UTC に SET する
  - 利点 全 SQL が UTC で揃う
  - 欠点 DBA 運用や他クライアントと食い違う
- 案 B `loc=Asia/Tokyo` でアプリも JST 統一にする
  - 利点 NOW() などの DB 関数と整合する
  - 欠点 ログやトレースで JST と UTC が混在する
- 案 C ドライバを `loc=UTC` にし、 DB 関数は `UTC_TIMESTAMP(6)` を使う ( セッションタイムゾーンは触らない )
  - 利点 アプリ全体が UTC で完結し、 DB のデフォルト挙動を変えない
  - 欠点 `NOW()` を直接使う運用ツールやテストヘルパが落とし穴になりうる

## 決定
案 C を採択する。
- ドライバ DSN は `loc=UTC` で固定する
- アプリは `time.Now().UTC()` で書き、 ドライバが UTC 値を文字列化して MySQL に送る
- DB 関数で「現在時刻」が必要なときは `UTC_TIMESTAMP(6)` を使う ( `NOW()` は禁止 )
- ログ表示時に必要なら JST に変換する

## 影響
- `internal/platform/database/db.go` の DSN 既定値に `loc=UTC` を含める
- 統合テストヘルパで `next_attempt_at` を「過去」にリセットするとき `UTC_TIMESTAMP(6)` を使う
- 運用 SQL や CLI で `NOW()` を使わず `UTC_TIMESTAMP(6)` を使うルール
- ログに出す時刻は時刻型のまま JSON 化する ( slog のデフォルト RFC3339 ナノ秒で UTC 表記される )

## 関連リンク
- 記事第 3 章 失敗事例と再現条件
- `internal/modules/transfer/e2e_integration_test.go` での UTC_TIMESTAMP(6) 適用
