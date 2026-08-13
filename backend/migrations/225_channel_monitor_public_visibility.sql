ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS public_visible BOOLEAN NOT NULL DEFAULT FALSE;

COMMENT ON COLUMN channel_monitors.public_visible IS
    'Whether this monitor and its status are exposed through user-facing V1 APIs';
