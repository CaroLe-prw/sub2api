-- Preserve the recharge amount that produced each affiliate accrual.
-- Payment-order rows can still read the amount from payment_orders; this
-- column covers redeem-code and admin top-up accruals that have no order.

ALTER TABLE user_affiliate_ledger
    ADD COLUMN IF NOT EXISTS source_amount DECIMAL(20,8) NULL;

COMMENT ON COLUMN user_affiliate_ledger.source_amount IS '产生返利的充值基准金额；历史数据无法可靠回填时为 NULL';

-- Payment-order accruals have an authoritative amount and are safe to fill.
UPDATE user_affiliate_ledger ual
SET source_amount = po.amount,
    updated_at = NOW()
FROM payment_orders po
WHERE ual.action = 'accrue'
  AND ual.source_order_id = po.id
  AND ual.source_amount IS NULL;

-- Best-effort recovery for historical redeem-code/admin top-up accruals.
-- Only one-to-one time-window matches are filled; ambiguous rows remain NULL.
WITH candidates AS (
    SELECT ual.id AS ledger_id,
           rc.id AS redeem_code_id,
           rc.value AS source_amount,
           COUNT(*) OVER (PARTITION BY ual.id) AS ledger_match_count,
           COUNT(*) OVER (PARTITION BY rc.id) AS redeem_match_count,
           ROW_NUMBER() OVER (
               PARTITION BY ual.id
               ORDER BY ABS(EXTRACT(EPOCH FROM (ual.created_at - rc.used_at))), rc.id
           ) AS ledger_rank
    FROM user_affiliate_ledger ual
    JOIN redeem_codes rc
      ON rc.used_by = ual.source_user_id
     AND rc.status = 'used'
     AND rc.type IN ('balance', 'admin_balance')
     AND rc.value > 0
     AND rc.used_at IS NOT NULL
     AND ual.created_at BETWEEN rc.used_at - INTERVAL '2 minutes'
                            AND rc.used_at + INTERVAL '2 minutes'
    WHERE ual.action = 'accrue'
      AND ual.source_order_id IS NULL
      AND ual.source_amount IS NULL
)
UPDATE user_affiliate_ledger ual
SET source_amount = candidates.source_amount,
    updated_at = NOW()
FROM candidates
WHERE ual.id = candidates.ledger_id
  AND candidates.ledger_match_count = 1
  AND candidates.redeem_match_count = 1
  AND candidates.ledger_rank = 1;
