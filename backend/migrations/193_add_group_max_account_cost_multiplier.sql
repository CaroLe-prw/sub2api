ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS max_account_cost_multiplier DECIMAL(10,4);

COMMENT ON COLUMN groups.max_account_cost_multiplier IS
    '账号调度成本倍率硬上限；NULL 表示沿用分组有效计费倍率';
