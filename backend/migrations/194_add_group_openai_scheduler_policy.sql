ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS openai_scheduler_profile VARCHAR(20) NOT NULL DEFAULT 'inherit',
    ADD COLUMN IF NOT EXISTS openai_scheduler_config JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN groups.openai_scheduler_profile IS
    'OpenAI group scheduler profile: inherit, sla, balanced, cost, or custom';

COMMENT ON COLUMN groups.openai_scheduler_config IS
    'Custom OpenAI scheduler weights retained independently of the selected profile';
