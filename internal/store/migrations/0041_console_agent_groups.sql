ALTER TABLE agent_group ADD COLUMN short_name TEXT;
ALTER TABLE agent_group ADD COLUMN creator_principal TEXT;
ALTER TABLE agent_group ADD COLUMN additions_closed INTEGER NOT NULL DEFAULT 0 CHECK(additions_closed IN (0,1));
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_group_short_name ON agent_group(short_name) WHERE short_name IS NOT NULL;
CREATE TABLE IF NOT EXISTS agent_group_state (
  singleton INTEGER PRIMARY KEY CHECK(singleton=1),
  ever_activated INTEGER NOT NULL CHECK(ever_activated IN (0,1))
);
INSERT OR IGNORE INTO agent_group_state(singleton,ever_activated)
VALUES(1,CASE WHEN EXISTS(SELECT 1 FROM agent_group) THEN 1 ELSE 0 END);
