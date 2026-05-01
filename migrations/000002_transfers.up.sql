-- transfers テーブル
-- 振込ドメインの集約。 冪等キーをアプリ層キー ( app_request_id ) と API 層キー ( api_idempotency_key ) で二重持ちする。
-- 状態は domain/status.go の validTransitions に従って遷移する。 楽観ロックは version 列で実現する。
CREATE TABLE transfers (
  id                    CHAR(36)     NOT NULL,
  app_request_id        VARCHAR(64)  NOT NULL,
  api_idempotency_key   CHAR(36)     NOT NULL,
  status                VARCHAR(32)  NOT NULL,
  amount                BIGINT       NOT NULL,
  source_account_id     VARCHAR(64)  NOT NULL,
  dest_bank_code        VARCHAR(8)   NOT NULL,
  dest_branch_code      VARCHAR(8)   NOT NULL,
  dest_account_type     VARCHAR(8)   NOT NULL,
  dest_account_num      VARCHAR(16)  NOT NULL,
  dest_account_name     VARCHAR(128) NOT NULL,
  apply_no              VARCHAR(64)  NULL,
  last_error            TEXT         NULL,
  version               INT          NOT NULL DEFAULT 1,
  created_at            DATETIME(6)  NOT NULL,
  updated_at            DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_app_request_id (app_request_id),
  UNIQUE KEY uq_api_idempotency_key (api_idempotency_key),
  KEY idx_status (status),
  KEY idx_apply_no (apply_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
