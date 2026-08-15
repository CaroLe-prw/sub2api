-- Daily check-in rewards. The business date is evaluated in the server's
-- configured timezone and is unique per user, which is the final protection
-- against duplicate rewards from retries or concurrent requests.
CREATE TABLE IF NOT EXISTS user_check_ins (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    business_date DATE NOT NULL,
    reward_amount DECIMAL(20,8) NOT NULL CHECK (reward_amount > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT user_check_ins_user_date_key UNIQUE (user_id, business_date)
);

CREATE INDEX IF NOT EXISTS idx_user_check_ins_user_created
    ON user_check_ins(user_id, created_at DESC);

COMMENT ON TABLE user_check_ins IS 'One daily balance reward per user and server-local business date';
COMMENT ON COLUMN user_check_ins.business_date IS 'Calendar date in the configured server timezone';
COMMENT ON COLUMN user_check_ins.reward_amount IS 'Balance credited atomically with this record';
