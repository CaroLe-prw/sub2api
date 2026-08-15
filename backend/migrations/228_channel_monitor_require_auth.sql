INSERT INTO settings (key, value)
VALUES ('channel_monitor_require_auth', 'true')
ON CONFLICT (key) DO NOTHING;
