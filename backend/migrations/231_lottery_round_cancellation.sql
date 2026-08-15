-- Closing an active lottery before its end time voids that round. Entries are
-- retained for the users' history but never receive a prize or balance credit.
ALTER TABLE lottery_rounds
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'lottery_rounds_terminal_state_check'
    ) THEN
        ALTER TABLE lottery_rounds
            ADD CONSTRAINT lottery_rounds_terminal_state_check
            CHECK (settled_at IS NULL OR cancelled_at IS NULL);
    END IF;
END $$;

DROP INDEX IF EXISTS idx_lottery_rounds_one_unsettled;
CREATE UNIQUE INDEX IF NOT EXISTS idx_lottery_rounds_one_active
    ON lottery_rounds ((1))
    WHERE settled_at IS NULL AND cancelled_at IS NULL;

DROP INDEX IF EXISTS idx_lottery_rounds_end_unsettled;
CREATE INDEX IF NOT EXISTS idx_lottery_rounds_end_active
    ON lottery_rounds (ends_at)
    WHERE settled_at IS NULL AND cancelled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_lottery_rounds_cancelled
    ON lottery_rounds (cancelled_at DESC)
    WHERE cancelled_at IS NOT NULL;

COMMENT ON COLUMN lottery_rounds.cancelled_at IS 'Set when an administrator closes the activity before its end time; cancelled rounds are never settled';
