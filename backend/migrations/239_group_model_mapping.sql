ALTER TABLE groups ADD COLUMN IF NOT EXISTS model_mapping JSONB;
COMMENT ON COLUMN groups.model_mapping IS 'Group request model mapping for all platforms: requested model to forwarding and billing model';
