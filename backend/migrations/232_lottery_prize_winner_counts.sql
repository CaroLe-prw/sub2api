-- Each prize has an administrator-configurable winner quota. Existing and
-- already configured rounds default to one winner per prize.
ALTER TABLE lottery_rounds
    ADD COLUMN IF NOT EXISTS first_prize_winner_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS second_prize_winner_count INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS third_prize_winner_count INTEGER NOT NULL DEFAULT 1;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'lottery_rounds_winner_counts_check'
    ) THEN
        ALTER TABLE lottery_rounds
            ADD CONSTRAINT lottery_rounds_winner_counts_check
            CHECK (
                first_prize_winner_count > 0
                AND second_prize_winner_count > 0
                AND third_prize_winner_count > 0
            );
    END IF;
END $$;

-- A settled entry with no prize is a valid non-winning result. Pending entries
-- still have no settled_at value, while winning entries keep their full prize
-- and balance snapshot.
ALTER TABLE lottery_entries
    DROP CONSTRAINT IF EXISTS lottery_entries_result_check;

ALTER TABLE lottery_entries
    ADD CONSTRAINT lottery_entries_result_check CHECK (
        (
            prize_tier IS NULL
            AND prize_name IS NULL
            AND reward_amount IS NULL
            AND balance_after IS NULL
        )
        OR
        (
            prize_tier IS NOT NULL
            AND prize_name IS NOT NULL
            AND reward_amount IS NOT NULL
            AND balance_after IS NOT NULL
            AND settled_at IS NOT NULL
        )
    );

COMMENT ON COLUMN lottery_rounds.first_prize_winner_count IS 'Maximum first-prize winners for the round';
COMMENT ON COLUMN lottery_rounds.second_prize_winner_count IS 'Maximum second-prize winners for the round';
COMMENT ON COLUMN lottery_rounds.third_prize_winner_count IS 'Maximum third-prize winners for the round';
