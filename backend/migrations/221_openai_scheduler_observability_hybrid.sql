SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS openai_scheduler_observability_minute_metrics (
    bucket_start TIMESTAMPTZ NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    group_name VARCHAR(255) NOT NULL DEFAULT '',
    request_count BIGINT NOT NULL DEFAULT 0,
    sticky_request_count BIGINT NOT NULL DEFAULT 0,
    switched_request_count BIGINT NOT NULL DEFAULT 0,
    switch_count BIGINT NOT NULL DEFAULT 0,
    failed_request_count BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    cache_eligible_tokens BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_start, group_id)
);

CREATE INDEX IF NOT EXISTS idx_openai_scheduler_observability_metrics_group_bucket
    ON openai_scheduler_observability_minute_metrics (group_id, bucket_start DESC);

CREATE TABLE IF NOT EXISTS openai_scheduler_observability_abnormal_traces (
    request_id VARCHAR(255) PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    payload JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_openai_scheduler_observability_abnormal_occurred
    ON openai_scheduler_observability_abnormal_traces (occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_openai_scheduler_observability_abnormal_group_occurred
    ON openai_scheduler_observability_abnormal_traces (group_id, occurred_at DESC);
