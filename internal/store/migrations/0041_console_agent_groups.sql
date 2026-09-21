ALTER TABLE agent_group ADD COLUMN short_name TEXT;
ALTER TABLE agent_group ADD COLUMN creator_principal TEXT;
ALTER TABLE agent_group ADD COLUMN additions_closed INTEGER NOT NULL DEFAULT 0 CHECK(additions_closed IN (0,1));
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_group_short_name ON agent_group(short_name) WHERE short_name IS NOT NULL;
