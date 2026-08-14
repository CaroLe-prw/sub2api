ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS streaming BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN channel_monitors.streaming IS
    'Request and validate provider SSE output; supported by OpenAI-compatible and Anthropic probes';
