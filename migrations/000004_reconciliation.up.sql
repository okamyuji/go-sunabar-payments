-- 請求テーブル
-- 1 件の請求は 1 つのバーチャル口座に紐付く ( virtual_accounts.invoice_id と双方向 ) 。
-- status は OPEN / PARTIAL / CLEARED / EXCESS の 4 値で、 入金状況に応じて遷移する。
CREATE TABLE invoices (
  id                  CHAR(36)     NOT NULL,
  amount              BIGINT       NOT NULL,
  virtual_account_id  VARCHAR(64)  NOT NULL,
  status              VARCHAR(16)  NOT NULL,
  paid_amount         BIGINT       NOT NULL DEFAULT 0,
  memo                VARCHAR(255) NULL,
  created_at          DATETIME(6)  NOT NULL,
  updated_at          DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_virtual_account_id (virtual_account_id),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 入金明細テーブル
-- sunabar の入出金明細 API から取得した行。 同一 item_key の重複登録を一意制約で防ぐ。
CREATE TABLE incoming_transactions (
  id                  CHAR(36)     NOT NULL,
  virtual_account_id  VARCHAR(64)  NOT NULL,
  item_key            VARCHAR(128) NOT NULL,
  amount              BIGINT       NOT NULL,
  remarks             TEXT         NULL,
  occurred_at         DATETIME(6)  NOT NULL,
  matched_invoice_id  CHAR(36)     NULL,
  created_at          DATETIME(6)  NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_item_key (virtual_account_id, item_key),
  KEY idx_virtual_account (virtual_account_id),
  KEY idx_matched_invoice (matched_invoice_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
