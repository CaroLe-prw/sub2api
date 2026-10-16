CREATE TABLE IF NOT EXISTS user_group_preferred_accounts (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id BIGINT NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_preferred_accounts_group
    ON user_group_preferred_accounts(group_id);
CREATE INDEX IF NOT EXISTS idx_user_group_preferred_accounts_account
    ON user_group_preferred_accounts(account_id);

COMMENT ON TABLE user_group_preferred_accounts IS '用户在分组中的优先上游账号，不绕过调度准入和故障回退';
