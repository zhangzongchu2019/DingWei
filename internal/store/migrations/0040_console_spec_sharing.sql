ALTER TABLE console_spec ADD COLUMN edit_capability_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE console_spec ADD COLUMN editing_closed INTEGER NOT NULL DEFAULT 0 CHECK(editing_closed IN (0,1));
ALTER TABLE console_spec_revision ADD COLUMN created_via TEXT NOT NULL DEFAULT 'principal' CHECK(created_via IN ('principal','shared_link'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_console_spec_edit_capability
  ON console_spec(edit_capability_hash) WHERE edit_capability_hash != '';
