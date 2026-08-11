ALTER TABLE openai_scheduler_observability_minute_metrics
    ADD COLUMN IF NOT EXISTS sticky_detected_request_count BIGINT NOT NULL DEFAULT 0;

-- Existing rows only stored actual sticky hits. Use that conservative value as
-- the historical denominator so old data never reports more than 100%.
UPDATE openai_scheduler_observability_minute_metrics
SET sticky_detected_request_count = sticky_request_count
WHERE sticky_detected_request_count = 0 AND sticky_request_count > 0;
