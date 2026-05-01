# ADR-004 状態機械に AWAITING_APPROVAL を含める

## ステータス
採択

## コンテキスト
sunabar サンドボックスでは振込依頼 API 実行後、 メールトークン承認 URL が発行され、 ブラウザで取引パスワードを入力する必要がある ( 任意値 ) 。
本番 BaaS では事業者契約形態によって承認方式が異なり、 API 認可で代替できる場合とそうでない場合がある。
状態機械でこの 2 段階性をどう表現するかを決める必要がある。

## 検討した選択肢
- 案 A AWAITING_APPROVAL を持たず、 REQUESTED から SETTLED への直接遷移のみ
  - 利点 状態数が少ない
  - 欠点 sunabar / 本番でのメールトークン承認待ち期間を表現できない
- 案 B AWAITING_APPROVAL を持つ
  - 利点 通知タイミングや運用監視のフックポイントを取れる、 滞留時間メトリクスを取れる
  - 欠点 状態数が増える

## 決定
案 B を採択。 AWAITING_APPROVAL 状態を明示することで、 Notification モジュールから「ユーザに承認 URL を案内する」フックが自然に取れる。
本番 BaaS でメール承認が不要な契約形態でも、 validTransitions に複数経路 ( REQUESTED -> APPROVED 直行 / REQUESTED -> SETTLED 直行 ) を含めることで両立する。

## 影響
- domain/status.go の validTransitions に複数の遷移パスを含める
- Notification モジュールが TransferAwaitingApproval イベントを購読する
- 観測性として AWAITING_APPROVAL 滞留時間のメトリクスを将来追加可能
- sunabar の status 文字列 ( "AcceptedToBank", "AwaitingApproval", "Approved", "Settled", "Failed" ) は M9 の実機検証で精緻化する ( 現状は仮値 )

## 関連リンク
- plan.md ( 1.7 状態機械 )
- ADR-002 共通 Outbox パターン
- ADR-003 冪等キーをアプリ層と API 層で二重持ちする
