-- Timed lottery rounds and one participation record per user. Prize
-- configuration is snapshotted on each round so historical results remain
-- stable when an administrator configures a later activity.
CREATE TABLE IF NOT EXISTS lottery_rounds (
    id BIGSERIAL PRIMARY KEY,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    first_prize_name VARCHAR(64) NOT NULL,
    first_prize_reward DECIMAL(20,8) NOT NULL CHECK (first_prize_reward > 0),
    first_prize_weight INTEGER NOT NULL CHECK (first_prize_weight > 0),
    second_prize_name VARCHAR(64) NOT NULL,
    second_prize_reward DECIMAL(20,8) NOT NULL CHECK (second_prize_reward > 0),
    second_prize_weight INTEGER NOT NULL CHECK (second_prize_weight > 0),
    third_prize_name VARCHAR(64) NOT NULL,
    third_prize_reward DECIMAL(20,8) NOT NULL CHECK (third_prize_reward > 0),
    third_prize_weight INTEGER NOT NULL CHECK (third_prize_weight > 0),
    settled_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT lottery_rounds_time_range_check CHECK (ends_at > starts_at)
);

-- At most one round may be awaiting settlement. Once it settles, the next
-- round can be configured without changing the immutable historical round.
CREATE UNIQUE INDEX IF NOT EXISTS idx_lottery_rounds_one_unsettled
    ON lottery_rounds ((1))
    WHERE settled_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_lottery_rounds_end_unsettled
    ON lottery_rounds (ends_at)
    WHERE settled_at IS NULL;

CREATE TABLE IF NOT EXISTS lottery_entries (
    id BIGSERIAL PRIMARY KEY,
    round_id BIGINT NOT NULL REFERENCES lottery_rounds(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    prize_tier SMALLINT CHECK (prize_tier BETWEEN 1 AND 3),
    prize_name VARCHAR(64),
    reward_amount DECIMAL(20,8) CHECK (reward_amount > 0),
    balance_after DECIMAL(20,8),
    settled_at TIMESTAMPTZ,
    CONSTRAINT lottery_entries_round_user_key UNIQUE (round_id, user_id),
    CONSTRAINT lottery_entries_result_check CHECK (
        (prize_tier IS NULL AND prize_name IS NULL AND reward_amount IS NULL AND settled_at IS NULL)
        OR
        (prize_tier IS NOT NULL AND prize_name IS NOT NULL AND reward_amount IS NOT NULL AND settled_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_lottery_entries_user_entered
    ON lottery_entries (user_id, entered_at DESC);

CREATE INDEX IF NOT EXISTS idx_lottery_entries_round
    ON lottery_entries (round_id);

COMMENT ON TABLE lottery_rounds IS 'Timed lottery rounds with immutable prize configuration snapshots';
COMMENT ON TABLE lottery_entries IS 'One entry per user and round, including the atomically credited result';
