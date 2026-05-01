-- ローカル開発用の seed データ。 make seed で投入する。
-- 既存データは TRUNCATE せず INSERT IGNORE で衝突回避。 何度実行しても結果が同じになるよう冪等に書く。

INSERT IGNORE INTO accounts
  (id, account_id, branch_code, account_number, account_type_code, account_name, primary_flag, label, created_at, updated_at)
VALUES
  ('00000000-0000-7000-8000-000000000a01', 'ACC0001', '001', '1234567', '1', 'スナバ タロウ', 1, 'メイン口座', UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)),
  ('00000000-0000-7000-8000-000000000a02', 'ACC0002', '001', '7654321', '1', 'スナバ ハナコ', 0, 'サブ口座', UTC_TIMESTAMP(6), UTC_TIMESTAMP(6));

INSERT IGNORE INTO virtual_accounts
  (id, virtual_account_id, branch_code, account_number, memo, expires_on, invoice_id, created_at, updated_at)
VALUES
  ('00000000-0000-7000-8000-000000000b01', 'VA-seed-1', '001', '9999001', 'invoice-001 用', '2027-12-31', NULL, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)),
  ('00000000-0000-7000-8000-000000000b02', 'VA-seed-2', '001', '9999002', 'invoice-002 用', '2027-12-31', NULL, UTC_TIMESTAMP(6), UTC_TIMESTAMP(6));

INSERT IGNORE INTO invoices
  (id, amount, virtual_account_id, status, paid_amount, memo, created_at, updated_at)
VALUES
  ('00000000-0000-7000-8000-000000000c01', 10000, 'VA-seed-1', 'OPEN', 0, '請求 1', UTC_TIMESTAMP(6), UTC_TIMESTAMP(6)),
  ('00000000-0000-7000-8000-000000000c02',  5000, 'VA-seed-2', 'OPEN', 0, '請求 2', UTC_TIMESTAMP(6), UTC_TIMESTAMP(6));

-- 請求とバーチャル口座の双方向リンクを揃える。
UPDATE virtual_accounts SET invoice_id='00000000-0000-7000-8000-000000000c01' WHERE virtual_account_id='VA-seed-1';
UPDATE virtual_accounts SET invoice_id='00000000-0000-7000-8000-000000000c02' WHERE virtual_account_id='VA-seed-2';
