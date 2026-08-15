-- Global chronological indexes for the administrator-facing engagement
-- record tables. User-facing lookups keep using their existing user-first
-- indexes while these avoid full scans as history grows.
CREATE INDEX IF NOT EXISTS idx_user_check_ins_admin_created
    ON user_check_ins (created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_lottery_entries_admin_results
    ON lottery_entries (settled_at DESC, round_id DESC, prize_tier ASC, id ASC)
    WHERE prize_tier IS NOT NULL;
