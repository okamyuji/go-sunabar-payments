-- 口座メタ情報テーブル
-- sunabar 側の口座情報をキャッシュし、 ラベル付けやユーザ紐付けなどアプリ側の付加情報を持つ。
CREATE TABLE accounts (
  id                 CHAR(36)     NOT NULL,
  account_id         VARCHAR(64)  NOT NULL,
  branch_code        VARCHAR(8)   NOT NULL,
  account_number     VARCHAR(16)  NOT NULL,
  account_type_code  VARCHAR(8)   NOT NULL,
  account_name       VARCHAR(128) NOT NULL,
  primary_flag       TINYINT(1)   NOT NULL DEFAULT 0,
  label              VARCHAR(128) NULL,
  created_at         DATETIME(6)  NOT NULL,
  updated_at         DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_account_id (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- バーチャル口座メタ情報テーブル
-- sunabar API で発行したバーチャル口座の追跡用。 invoice_id は M8 の請求と紐付ける。
CREATE TABLE virtual_accounts (
  id                  CHAR(36)     NOT NULL,
  virtual_account_id  VARCHAR(64)  NOT NULL,
  branch_code         VARCHAR(8)   NOT NULL,
  account_number      VARCHAR(16)  NOT NULL,
  memo                VARCHAR(255) NULL,
  expires_on          DATE         NULL,
  invoice_id          CHAR(36)     NULL,
  created_at          DATETIME(6)  NOT NULL,
  updated_at          DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_virtual_account_id (virtual_account_id),
  KEY idx_invoice_id (invoice_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
