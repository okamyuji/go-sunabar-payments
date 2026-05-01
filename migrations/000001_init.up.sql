-- 共通 Outbox イベントテーブル
-- ビジネスデータの更新と同一トランザクション内で INSERT し、外部 API 呼び出しの原子性を担保する。
-- リレーワーカーが status='PENDING' をポーリングで取り出し、配信して SENT に遷移させる。
CREATE TABLE outbox_events (
  id              CHAR(36)     NOT NULL,
  aggregate_type  VARCHAR(64)  NOT NULL,
  aggregate_id    CHAR(36)     NOT NULL,
  event_type      VARCHAR(128) NOT NULL,
  payload         JSON         NOT NULL,
  status          VARCHAR(16)  NOT NULL DEFAULT 'PENDING',
  attempt_count   INT          NOT NULL DEFAULT 0,
  next_attempt_at DATETIME(6)  NOT NULL,
  last_error      TEXT         NULL,
  created_at      DATETIME(6)  NOT NULL,
  sent_at         DATETIME(6)  NULL,
  PRIMARY KEY (id),
  KEY idx_pending (status, next_attempt_at),
  KEY idx_aggregate (aggregate_type, aggregate_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- イベント受信側の冪等性管理テーブル
-- 受信ハンドラは ( event_id, consumer ) の INSERT を試み、重複キーエラーで処理済みと判定する。
CREATE TABLE event_processed (
  event_id     CHAR(36)    NOT NULL,
  consumer     VARCHAR(64) NOT NULL,
  processed_at DATETIME(6) NOT NULL,
  PRIMARY KEY (event_id, consumer)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
